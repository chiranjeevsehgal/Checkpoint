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
static uint32_t s_final_diag_due_ms = 0;
static uint16_t s_conn_handle = 0xFFFF;
static void (*s_packet_cb)(const uint8_t *, size_t) = nullptr;

static NimBLEServer *s_server = nullptr;
static NimBLEService *s_service = nullptr;
static NimBLECharacteristic *s_ctrl = nullptr;
static NimBLECharacteristic *s_data = nullptr;
static NimBLECharacteristic *s_ack = nullptr;

static uint32_t rand32() { return esp_random(); }

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
    // Log which device connected (address + handle)
    {
      String addr = connInfo.getAddress().toString().c_str();
      Serial.printf("BLE connected: %s handle %u session %08lx\n", addr.c_str(), s_conn_handle, (unsigned long)s_session);
    }
    // Always reload master key to avoid leaking a derived per-file key
    // if a previous transfer was interrupted mid-file (R3).
    crypto_load_or_gen_key();
  }
  void onDisconnect(NimBLEServer *pServer, NimBLEConnInfo &connInfo, int reason) override {
    String addr = connInfo.getAddress().toString().c_str();
    const char *reason_str = "UNKNOWN";
    // Common NimBLE/ESP error 534 (0x216) = MIC failure (bond mismatch) — see NimBLE host error table
    if (reason == 534) reason_str = "MIC_FAILURE (0x216/bond mismatch/timeout)";
    else if (reason == 0x08) reason_str = "TIMEOUT (0x08)";
    else if (reason == 0x13) reason_str = "REMOTE_USER_TERM (0x13)";
    else if (reason == 0x16) reason_str = "CONN_TERM_LOCAL_HOST (0x16)";
    Serial.printf("BLE disconnected: %s reason %d (%s) handle %u uptime %lu ms\n", addr.c_str(), reason, reason_str, connInfo.getConnHandle(), (unsigned long)millis());
    if (reason == 534) {
      // Stale bond on either side causes MIC failure on next connect
      // Delete bond for this peer so Windows will re-pair cleanly
      Serial.printf("BLE MIC_FAILURE: deleting bond for %s to force re-pair\n", addr.c_str());
      NimBLEDevice::deleteBond(connInfo.getAddress());
    }
    s_connected = false;
    s_handshaked = false;
    s_encrypted = false;
    s_state = BLE_DISCONNECTED;
    s_handshake_start_ms = 0;
    s_final_diag_due_ms = 0;
    s_conn_handle = 0xFFFF;
    // Restore master key if a derived file key was active when link dropped.
    crypto_load_or_gen_key();
    pServer->startAdvertising();
    Serial.println("BLE advertising Checkpoint — waiting for handshake");
  }
  void onAuthenticationComplete(NimBLEConnInfo &connInfo) override {
    s_encrypted = connInfo.isEncrypted();
    String addr = connInfo.getAddress().toString().c_str();
    Serial.printf("BLE auth complete: %s encrypted %d handle %u uptime %lu ms delta %ld ms since connect\n", addr.c_str(), s_encrypted, connInfo.getConnHandle(), (unsigned long)millis(), (long)(millis() - s_handshake_start_ms));
  }
};

class CtrlCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *pCharacteristic, NimBLEConnInfo &connInfo) override {
    (void)connInfo;
    std::string v = pCharacteristic->getValue();
    if (v.empty() || !s_packet_cb) return;
    s_packet_cb((const uint8_t *)v.data(), v.size());
    if (v.size() >= 2 && (uint8_t)v[1] == PKT_HELLO) {
      // Detailed HELLO log for debugging pairing race (BLE auth may still be pending)
      Serial.printf("HELLO recv ver=%u type=%u len=%u encrypted=%d connected=%d handshaked=%d handle=%u uptime=%lu deltaConnect=%ld ms\n",
                    (unsigned)v[0], (unsigned)v[1], (unsigned)v.size(), s_encrypted, s_connected, s_handshaked, s_conn_handle, (unsigned long)millis(), (long)(millis() - s_handshake_start_ms));
      if ((uint8_t)v[0] != PROTO_VER) {
        Serial.printf("HELLO reject version mismatch dev=%u expect=%u\n", (unsigned)v[0], PROTO_VER);
        uint8_t err = 0x02;
        uint8_t buf[PROTO_MAX_PACKET];
        size_t bl = sizeof(buf);
        if (proto_build(PKT_ERROR, 0, &err, 1, buf, &bl) && s_ctrl) {
          s_ctrl->setValue(buf, bl);
          s_ctrl->indicate();
        }
        return;
      }
      if (!s_encrypted || !s_connected) {
        uint32_t since_connect = (s_handshake_start_ms == 0) ? 99999 : (millis() - s_handshake_start_ms);
        Serial.printf("HELLO reject not encrypted/connected (enc=%d conn=%d state=%d sinceConnect=%lu) -> PKT_ERROR 0x01\n", s_encrypted, s_connected, s_state, (unsigned long)since_connect);
        uint8_t err = 0x01;
        uint8_t buf[PROTO_MAX_PACKET];
        size_t bl = sizeof(buf);
        if (proto_build(PKT_ERROR, 0, &err, 1, buf, &bl) && s_ctrl) {
          s_ctrl->setValue(buf, bl);
          s_ctrl->indicate();
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
      uint8_t key[CRYPTO_KEY_BYTES];
      if (crypto_get_key(key)) {
        memcpy(payload + 11, key, CRYPTO_KEY_BYTES);
        plen = 27;
        memset(key, 0, sizeof(key));
      }
      if (proto_build(PKT_HELLO_ACK, 0, payload, plen, resp, &rl)) {
        s_ctrl->setValue(resp, rl);
        s_ctrl->indicate();
        s_handshaked = true;
        s_handshake_start_ms = 0;
        s_state = BLE_READY;
        uint16_t ci = ble_conn_interval_ms();
        uint16_t mtu_now = ble_mtu_negotiated();
        Serial.printf("HELLO_ACK session=%08lx mtu=%u chunk=%lu phy=%u interval=%ums\n",
                      (unsigned long)s_session, mtu_now, (unsigned long)chunk, ble_phy(), ci);
        // hybrid: 15ms iOS-safe, DLE 251, try 2M
        if (s_server && s_conn_handle != 0xFFFF) {
          s_server->updateConnParams(s_conn_handle, 12, 12, 0, 400);
          s_server->setDataLen(s_conn_handle, 251);
          // Request 2M PHY — phone may reject, final diag logs result
          // Use named mask instead of literal 0x02 per review
          bool phy_ok = s_server->updatePhy(s_conn_handle, BLE_GAP_LE_PHY_2M_MASK, BLE_GAP_LE_PHY_2M_MASK, 0);
          Serial.printf("BLE updatePhy 2M req phy_ok=%d handle %u\n", phy_ok, s_conn_handle);
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
  NimBLEDevice::setSecurityAuth(true, true, true);
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
  Serial.printf("BLE advertising %s — ready\n", BLE_DEVICE_NAME);
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
  return (millis() - s_handshake_start_ms) > BLE_HANDSHAKE_TIMEOUT_MS;
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
  if (s_final_diag_due_ms == 0) return;
  if (!s_connected || s_conn_handle == 0xFFFF) { s_final_diag_due_ms = 0; return; }
  if ((int32_t)(millis() - s_final_diag_due_ms) < 0) return;
  s_final_diag_due_ms = 0;
  Serial.printf("BLE_FINAL mtu=%u interval=%ums phy=%u (req 15ms DLE251)\n",
                ble_mtu_negotiated(), ble_conn_interval_ms(), ble_phy());
}

bool ble_send_raw(const uint8_t *data, size_t len) {
  if (!s_connected || !s_handshaked) return false;
  if (!s_data) return false;
  s_data->setValue(data, len);
  return s_data->notify();
}

bool ble_send_packet(uint8_t type, uint16_t seq, const uint8_t *payload, uint16_t len) {
  uint8_t buf[PROTO_MAX_PACKET];
  size_t bl = sizeof(buf);
  if (!proto_build(type, seq, payload, len, buf, &bl)) return false;
  if (type == PKT_DATA) return ble_send_raw(buf, bl);
  if (!s_ctrl) return false;
  s_ctrl->setValue(buf, bl);
  return s_ctrl->indicate();
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
  if (!s_connected || s_conn_handle == 0xFFFF || !s_server) return BLE_MTU;
  NimBLEConnInfo info = s_server->getPeerInfoByHandle(s_conn_handle);
  uint16_t mtu = info.getMTU();
  return mtu ? mtu : BLE_MTU;
}

uint8_t ble_phy() {
  if (!s_connected || s_conn_handle == 0xFFFF || !s_server) return 1;
  uint8_t tx = 1, rx = 1;
  if (s_server->getPhy(s_conn_handle, &tx, &rx)) {
    // tx: 1=1M 2=2M 4=coded — map to 1/2 for bench simplicity
    if (tx == BLE_GAP_LE_PHY_2M || rx == BLE_GAP_LE_PHY_2M) return 2;
    if (tx == BLE_GAP_LE_PHY_CODED || rx == BLE_GAP_LE_PHY_CODED) return 3;
    return 1;
  }
  return 1;
}
