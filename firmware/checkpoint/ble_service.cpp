#include "ble_service.h"
#include "protocol.h"
#include "crypto.h"
#include <NimBLEDevice.h>
#ifndef BLE_GAP_LE_PHY_2M_MASK
#define BLE_GAP_LE_PHY_2M_MASK 0x02
#endif

static BleState s_state = BLE_DISCONNECTED;
static uint32_t s_session = 0;
static bool s_connected = false;
static bool s_handshaked = false;
static bool s_encrypted = false;
static uint32_t s_handshake_start_ms = 0;
static uint32_t s_last_handshake_ms = 0; // refreshed at auth-complete + HELLO_ACK
static bool s_hello_sent = false; // HELLO_ACK transmitted, READY pending
static uint32_t s_final_diag_due_ms = 0;
static uint16_t s_conn_handle = 0xFFFF;
static void (*s_packet_cb)(const uint8_t *, size_t) = nullptr;

static NimBLEServer *s_server = nullptr;
static NimBLEService *s_service = nullptr;
static NimBLECharacteristic *s_ctrl = nullptr;
static NimBLECharacteristic *s_data = nullptr;
static NimBLECharacteristic *s_ack = nullptr;

static uint32_t rand32() { return esp_random(); }

// Serializes all GATT writes: the loop task (control_poll), the transfer
// task (announce/done/DATA), and the NimBLE callback task (HELLO_ACK/ERROR)
// share s_ctrl/s_data, which are not thread-safe. Short hold, never across
// SD/crypto/filesystem work.
static SemaphoreHandle_t s_ble_tx_mutex = nullptr;

static bool ble_tx_lock(uint32_t timeout_ms = 500) {
  if (!s_ble_tx_mutex) s_ble_tx_mutex = xSemaphoreCreateMutex();
  if (!s_ble_tx_mutex) return false;
  return xSemaphoreTake(s_ble_tx_mutex, pdMS_TO_TICKS(timeout_ms)) == pdTRUE;
}

static void ble_tx_unlock() {
  if (s_ble_tx_mutex) xSemaphoreGive(s_ble_tx_mutex);
}

// Direct CTRL indicate for the NimBLE callback task (same mutex as above).
static bool ctrl_indicate_locked(const uint8_t *buf, size_t bl) {
  if (!s_ctrl || !ble_tx_lock()) return false;
  s_ctrl->setValue(buf, bl);
  bool ok = s_ctrl->indicate();
  ble_tx_unlock();
  return ok;
}

class ServerCallbacks : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer *pServer, NimBLEConnInfo &connInfo) override {
    (void)pServer;
    s_connected = true;
    s_state = BLE_HANDSHAKING;
    s_handshaked = false;
    s_encrypted = false;
    s_session = rand32();
    s_conn_handle = connInfo.getConnHandle();
    s_handshake_start_ms = millis();
    s_last_handshake_ms = s_handshake_start_ms;
    s_hello_sent = false;
    // Existing bond: actively restore encryption. New peer: leave pairing
    // initiation to the host (Windows/Bleak) so two sides don't race.
    bool bonded = connInfo.isBonded();
    Serial.printf("BLE connect handle=%u encrypted=%d bonded=%d\n",
                  s_conn_handle, (int)connInfo.isEncrypted(),
                  (int)bonded);
    if (bonded) {
      bool sec_started = NimBLEDevice::startSecurity(s_conn_handle);
      Serial.printf("BLE restore security started=%d\n", (int)sec_started);
    }
    // Always reload master key to avoid leaking a derived per-file key
    // if a previous transfer was interrupted mid-file (R3).
    crypto_load_or_gen_key();
  }
  void onDisconnect(NimBLEServer *pServer, NimBLEConnInfo &connInfo, int reason) override {
    // Do NOT delete the bond here. Reason 534 accompanies routine
    // host-initiated disconnects — it is not proof of a stale/corrupt key.
    // Deleting our copy orphans the Windows-side bond and forces a full
    // re-pair on every reconnect. Genuine staleness is recovered host-side
    // by CheckpointClient._rebond(), so the bond must be left intact.
    (void)connInfo;
    Serial.printf("BLE disconnect reason=%d\n", reason);
    s_connected = false;
    s_handshaked = false;
    s_encrypted = false;
    s_state = BLE_DISCONNECTED;
    s_handshake_start_ms = 0;
    s_last_handshake_ms = 0;
    s_hello_sent = false;
    s_final_diag_due_ms = 0;
    s_conn_handle = 0xFFFF;
    // Restore master key if a derived file key was active when link dropped.
    crypto_load_or_gen_key();
    pServer->startAdvertising();
  }
  void onAuthenticationComplete(NimBLEConnInfo &connInfo) override {
    s_encrypted = connInfo.isEncrypted();
    if (s_encrypted) s_last_handshake_ms = millis();
    Serial.printf("BLE auth complete encrypted=%d bonded=%d\n",
                  (int)connInfo.isEncrypted(), (int)connInfo.isBonded());
  }
};

class CtrlCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *pCharacteristic, NimBLEConnInfo &connInfo) override {
    (void)connInfo;
    std::string v = pCharacteristic->getValue();
    if (v.empty() || !s_packet_cb) return;
    s_packet_cb((const uint8_t *)v.data(), v.size());
    if (v.size() >= 2 && (uint8_t)v[1] == PKT_READY) {
      // Host confirms HELLO_ACK processed (session/mtu/crypto installed).
      // Only the handshake completes here — transfers stay gated until now.
      Packet ready;
      if (s_hello_sent && !s_handshaked &&
          proto_parse((const uint8_t *)v.data(), v.size(), &ready) &&
          ready.len >= 4) {
        uint32_t echo = 0;
        memcpy(&echo, ready.payload, 4);
        if (echo == s_session) {
          s_handshaked = true;
          s_handshake_start_ms = 0;
          s_state = BLE_READY;
        }
      }
      return;
    }
    if (v.size() >= 2 && (uint8_t)v[1] == PKT_HELLO) {
      if ((uint8_t)v[0] != PROTO_VER) {
        uint8_t err = 0x02;
        uint8_t buf[PROTO_MAX_PACKET];
        size_t bl = sizeof(buf);
        if (proto_build(PKT_ERROR, 0, &err, 1, buf, &bl) && s_ctrl) {
          ctrl_indicate_locked(buf, bl);
        }
        return;
      }
      if (!s_encrypted || !s_connected) {
        uint8_t err = 0x01;
        uint8_t buf[PROTO_MAX_PACKET];
        size_t bl = sizeof(buf);
        if (proto_build(PKT_ERROR, 0, &err, 1, buf, &bl) && s_ctrl) {
          ctrl_indicate_locked(buf, bl);
        }
        return;
      }
      uint8_t resp[64];
      size_t rl = sizeof(resp);
      uint8_t payload[32];
      payload[0] = PROTO_VER;
      memcpy(payload + 1, &s_session, 4);
      uint16_t mtu = ble_mtu_negotiated();
      memcpy(payload + 5, &mtu, 2);
      uint32_t chunk = REC_CHUNK_SEC;
      memcpy(payload + 7, &chunk, 4);
      size_t plen = 11;
      // Per-session key: the permanent master never leaves the device.
      uint8_t master[CRYPTO_KEY_BYTES];
      uint8_t sess_key[CRYPTO_KEY_BYTES];
      if (crypto_get_key(master) &&
          crypto_derive_session_key(master, s_session, sess_key)) {
        memcpy(payload + 11, sess_key, CRYPTO_KEY_BYTES);
        plen = 27;
        memset(sess_key, 0, sizeof(sess_key));
      }
      memset(master, 0, sizeof(master));
      if (proto_build(PKT_HELLO_ACK, 0, payload, plen, resp, &rl)) {
        // Handshake completes only when the host answers PKT_READY
        // (proves session/mtu/crypto installed). Transfers stay gated.
        if (ctrl_indicate_locked(resp, rl)) {
          s_hello_sent = true;
          s_last_handshake_ms = millis(); // fresh 5s budget for the READY reply
        }
        // hybrid: 15ms iOS-safe, DLE 251, try 2M
        if (s_server && s_conn_handle != 0xFFFF) {
          s_server->updateConnParams(s_conn_handle, 12, 12, 0, 400);
          s_server->setDataLen(s_conn_handle, 251);
          // Request 2M PHY — phone may reject
          // Use named mask instead of literal 0x02 per review
          s_server->updatePhy(s_conn_handle, BLE_GAP_LE_PHY_2M_MASK, BLE_GAP_LE_PHY_2M_MASK, 0);
          s_final_diag_due_ms = millis() + 600;
        }
      }
    }
  }
};

class AckCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *pCharacteristic, NimBLEConnInfo &connInfo) override {
    (void)connInfo;
    std::string v = pCharacteristic->getValue();
    if (v.empty() || !s_packet_cb) return;
    s_packet_cb((const uint8_t *)v.data(), v.size());
  }
};

bool ble_init() {
  NimBLEDevice::init(BLE_DEVICE_NAME);
  NimBLEDevice::setMTU(BLE_MTU);
  NimBLEDevice::setPower(ESP_PWR_LVL_P9);
  NimBLEDevice::setSecurityAuth(true, false, true); // bonding + SC, no MITM: no display/input
  NimBLEDevice::setSecurityIOCap(BLE_HS_IO_NO_INPUT_OUTPUT);
  s_server = NimBLEDevice::createServer();
  s_server->setCallbacks(new ServerCallbacks());
  s_server->advertiseOnDisconnect(true);
  NimBLEService *pGattSvc = s_server->createService("1801");
  pGattSvc->createCharacteristic("2A05", NIMBLE_PROPERTY::INDICATE);
  pGattSvc->start();
  s_service = s_server->createService(BLE_SERVICE_UUID);
  s_ctrl = s_service->createCharacteristic(BLE_CTRL_UUID, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::INDICATE | NIMBLE_PROPERTY::READ);
  s_data = s_service->createCharacteristic(BLE_DATA_UUID, NIMBLE_PROPERTY::NOTIFY | NIMBLE_PROPERTY::READ);
  s_ack = s_service->createCharacteristic(BLE_ACK_UUID, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR);
  s_ctrl->setCallbacks(new CtrlCallbacks());
  s_ack->setCallbacks(new AckCallbacks());
  s_service->start();
  NimBLEAdvertising *adv = NimBLEDevice::getAdvertising();
  // Fix N/A on phone: put name in ADV (passive scan) and also in scan response
  {
    NimBLEAdvertisementData advData;
    advData.setFlags(0x06);
    advData.setName(BLE_DEVICE_NAME);
    advData.addServiceUUID(BLE_SERVICE_UUID);
    adv->setAdvertisementData(advData);
    NimBLEAdvertisementData scanResp;
    scanResp.setName(BLE_DEVICE_NAME);
    scanResp.addServiceUUID(BLE_SERVICE_UUID);
    adv->setScanResponseData(scanResp);
  }
  adv->start();
  crypto_init();
  crypto_load_or_gen_key();
  return true;
}

bool ble_is_connected() { return s_connected; }
bool ble_is_handshaked() { return s_handshaked; }
bool ble_is_encrypted() { return s_encrypted && s_connected; }
BleState ble_state() { return s_state; }
uint32_t ble_session_id() { return s_session; }

bool ble_handshake_timed_out() {
  if (s_state != BLE_HANDSHAKING) return false;
  if (s_handshake_start_ms == 0) return false;
  // Pairing/encryption may take a while (first-time Windows bond); the
  // application HELLO/READY exchange afterwards gets a short budget.
  if (!s_encrypted) return (millis() - s_handshake_start_ms) > BLE_AUTH_TIMEOUT_MS;
  uint32_t base = s_last_handshake_ms ? s_last_handshake_ms : s_handshake_start_ms;
  return (millis() - base) > BLE_HANDSHAKE_TIMEOUT_MS;
}

void ble_check_handshake_timeout() {
  if (ble_handshake_timed_out()) {
    if (s_server && s_conn_handle != 0xFFFF) {
      s_server->disconnect(s_conn_handle);
    } else if (s_server) {
      s_state = BLE_DISCONNECTED;
      s_handshake_start_ms = 0;
      s_server->startAdvertising();
    }
  }
}

void ble_check_final_diag() {
  // PHY/MTU tuning settles without Serial spam; phone-side logs cover triage.
  s_final_diag_due_ms = 0;
}

void ble_disconnect() {
  if (s_server && s_conn_handle != 0xFFFF) {
    s_server->disconnect(s_conn_handle);
  }
}

bool ble_send_raw(const uint8_t *data, size_t len) {
  if (!s_connected || !s_handshaked) return false;
  if (!s_data) return false;
  if (!ble_tx_lock()) return false;
  s_data->setValue(data, len);
  bool ok = s_data->notify();
  ble_tx_unlock();
  return ok;
}

bool ble_send_packet(uint8_t type, uint16_t seq, const uint8_t *payload, uint16_t len) {
  uint8_t buf[PROTO_MAX_PACKET];
  size_t bl = sizeof(buf);
  if (!proto_build(type, seq, payload, len, buf, &bl)) return false;
  if (type == PKT_DATA) return ble_send_raw(buf, bl);
  if (!s_ctrl) return false;
  if (!ble_tx_lock()) return false;
  s_ctrl->setValue(buf, bl);
  bool ok = s_ctrl->indicate();
  ble_tx_unlock();
  return ok;
}

void ble_on_packet(void (*cb)(const uint8_t *data, size_t len)) { s_packet_cb = cb; }

uint16_t ble_conn_interval_ms() {
  if (!s_connected || s_conn_handle == 0xFFFF || !s_server) return 0;
  NimBLEConnInfo info = s_server->getPeerInfoByHandle(s_conn_handle);
  uint16_t itvl = info.getConnInterval();
  if (itvl == 0) return 30;
  // NimBLE reports interval in 1.25ms units
  return (uint16_t)(itvl * 1.25f + 0.5f);
}

uint16_t ble_mtu_negotiated() {
  if (!s_connected || s_conn_handle == 0xFFFF || !s_server) return 0;
  NimBLEConnInfo info = s_server->getPeerInfoByHandle(s_conn_handle);
  return info.getMTU(); // 0 = unknown: host must refuse transfer, not assume
}

uint8_t ble_phy() {
  if (!s_connected || s_conn_handle == 0xFFFF || !s_server) return 1;
  uint8_t tx = 1, rx = 1;
  if (s_server->getPhy(s_conn_handle, &tx, &rx)) {
    // tx: 1=1M 2=2M 4=coded — map to 1/2 for simplicity
    if (tx == BLE_GAP_LE_PHY_2M || rx == BLE_GAP_LE_PHY_2M) return 2;
    if (tx == BLE_GAP_LE_PHY_CODED || rx == BLE_GAP_LE_PHY_CODED) return 3;
    return 1;
  }
  return 1;
}
