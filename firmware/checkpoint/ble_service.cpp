#include "ble_service.h"
#include "protocol.h"
#include "crypto.h"
#include "auth.h"
#include "ui.h"
#include "log.h"
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
static uint8_t s_device_nonce[16];

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
    memset(s_device_nonce, 0, sizeof(s_device_nonce));
    auth_clear_session();
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
    // re-pair on every reconnect. Lost bonds are recovered over USB
    // (`auth forget`) + physical re-enrollment, never automatically.
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
    memset(s_device_nonce, 0, sizeof(s_device_nonce));
    auth_clear_session();
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

static void send_error_locked(uint8_t code) {
  uint8_t buf[PROTO_MAX_PACKET];
  size_t bl = sizeof(buf);
  if (proto_build(PKT_ERROR, 0, &code, 1, buf, &bl) && s_ctrl) {
    ctrl_indicate_locked(buf, bl);
  }
}

static void send_ready_ack() {
  uint8_t ack_payload[4];
  memcpy(ack_payload, &s_session, 4);
  uint8_t resp[PROTO_MAX_PACKET];
  size_t rl = sizeof(resp);
  if (proto_build(PKT_READY_ACK, 0, ack_payload, sizeof(ack_payload), resp, &rl)) {
    ctrl_indicate_locked(resp, rl);
  }
}

static void handle_hello(const uint8_t *raw, size_t raw_len) {
  if (raw_len >= 1 && raw[0] != PROTO_VER) {
    send_error_locked(0x02);
    return;
  }
  if (!s_encrypted || !s_connected) {
    send_error_locked(0x01);
    return;
  }
  Packet hello;
  if (!proto_parse(raw, raw_len, &hello) || hello.len != 1) {
    send_error_locked(0x03);
    return;
  }
  bool enroll_requested = (hello.payload[0] & 0x01) != 0;
  if (!crypto_random_bytes(s_device_nonce, sizeof(s_device_nonce))) {
    send_error_locked(0x03);
    return;
  }
  if (!auth_begin(s_session, enroll_requested, s_device_nonce)) {
    send_error_locked(0x03);
    return;
  }
  uint8_t device_id[16];
  if (!auth_get_device_id(device_id)) {
    send_error_locked(0x03);
    return;
  }
  uint8_t payload[44];
  payload[0] = PROTO_VER;
  memcpy(payload + 1, &s_session, 4);
  uint16_t mtu = ble_mtu_negotiated();
  memcpy(payload + 5, &mtu, 2);
  uint32_t chunk = REC_CHUNK_SEC;
  memcpy(payload + 7, &chunk, 4);
  memcpy(payload + 11, device_id, 16);
  memcpy(payload + 27, s_device_nonce, 16);
  payload[43] = (enroll_requested && auth_enrollment_active()) ? 1 : 0;
  memset(device_id, 0, sizeof(device_id));
  uint8_t resp[PROTO_MAX_PACKET];
  size_t rl = sizeof(resp);
  if (proto_build(PKT_HELLO_ACK, 0, payload, sizeof(payload), resp, &rl)) {
    memset(payload, 0, sizeof(payload));
    if (ctrl_indicate_locked(resp, rl)) {
      s_hello_sent = true;
      s_last_handshake_ms = millis();
    }
    if (s_server && s_conn_handle != 0xFFFF) {
      s_server->updateConnParams(s_conn_handle, 12, 12, 0, 400);
      s_server->setDataLen(s_conn_handle, 251);
      s_server->updatePhy(s_conn_handle, BLE_GAP_LE_PHY_2M_MASK, BLE_GAP_LE_PHY_2M_MASK, 0);
      s_final_diag_due_ms = millis() + 600;
    }
  } else {
    memset(payload, 0, sizeof(payload));
  }
}

static void handle_auth(const uint8_t *raw, size_t raw_len) {
  if (!s_encrypted || !s_connected) {
    send_error_locked(0x01);
    return;
  }
  Packet pkt;
  if (!s_hello_sent || !proto_parse(raw, raw_len, &pkt) || pkt.len != 64) {
    send_error_locked(0x03);
    return;
  }
  uint8_t client_nonce[16];
  memcpy(client_nonce, pkt.payload + 16, 16);
  bool is_enroll = false;
  uint8_t server_proof[32];
  if (!auth_verify_client_proof(pkt.payload, client_nonce, pkt.payload + 32, &is_enroll, server_proof)) {
    memset(client_nonce, 0, sizeof(client_nonce));
    send_error_locked(0x03);
    if (s_server && s_conn_handle != 0xFFFF) s_server->disconnect(s_conn_handle);
    return;
  }
  memset(client_nonce, 0, sizeof(client_nonce));
  uint8_t resp[PROTO_MAX_PACKET];
  size_t rl = sizeof(resp);
  if (proto_build(PKT_AUTH_OK, 0, server_proof, sizeof(server_proof), resp, &rl)) {
    ctrl_indicate_locked(resp, rl);
    s_last_handshake_ms = millis();
  }
  memset(server_proof, 0, sizeof(server_proof));
}

static void handle_ready(const uint8_t *raw, size_t raw_len) {
  if (!s_encrypted || !s_connected) {
    send_error_locked(0x01);
    return;
  }
  Packet ready;
  if (!s_hello_sent || !proto_parse(raw, raw_len, &ready) || ready.len != 36) {
    send_error_locked(0x03);
    return;
  }
  uint32_t echo = 0;
  memcpy(&echo, ready.payload, 4);
  if (echo != s_session) {
    send_error_locked(0x03);
    return;
  }
  if (s_handshaked && auth_is_authenticated()) {
    send_ready_ack();
    return;
  }
  if (s_handshaked) {
    send_error_locked(0x03);
    return;
  }
  if (!auth_verify_client_finish(ready.payload + 4)) {
    send_error_locked(0x03);
    if (s_server && s_conn_handle != 0xFFFF) s_server->disconnect(s_conn_handle);
    return;
  }
  s_handshaked = true;
  s_handshake_start_ms = 0;
  s_state = BLE_READY;
  ui_signal_auth_ok();
  send_ready_ack();
}

class CtrlCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *pCharacteristic, NimBLEConnInfo &connInfo) override {
    (void)connInfo;
    std::string v = pCharacteristic->getValue();
    if (v.empty()) return;
    const uint8_t *raw = (const uint8_t *)v.data();
    size_t raw_len = v.size();
    if (raw_len < 2) return;
    uint8_t ver = raw[0];
    uint8_t type = raw[1];
    if (ver != PROTO_VER) {
      if (type == PKT_HELLO) send_error_locked(0x02);
      return;
    }
    if (type == PKT_HELLO) {
      handle_hello(raw, raw_len);
      return;
    }
    if (type == PKT_AUTH) {
      handle_auth(raw, raw_len);
      return;
    }
    if (type == PKT_READY) {
      handle_ready(raw, raw_len);
      return;
    }
    if (!s_packet_cb) return;
    if (!auth_is_authenticated()) {
      send_error_locked(0x03);
      return;
    }
    s_packet_cb(raw, raw_len);
  }
};

class AckCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *pCharacteristic, NimBLEConnInfo &connInfo) override {
    (void)connInfo;
    std::string v = pCharacteristic->getValue();
    if (v.empty() || !s_packet_cb) return;
    const uint8_t *raw = (const uint8_t *)v.data();
    if (v.size() < 2 || raw[0] != PROTO_VER) return;
    if (!auth_is_authenticated()) return;
    s_packet_cb(raw, v.size());
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
  s_ctrl = s_service->createCharacteristic(BLE_CTRL_UUID, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_ENC | NIMBLE_PROPERTY::INDICATE);
  s_data = s_service->createCharacteristic(BLE_DATA_UUID, NIMBLE_PROPERTY::NOTIFY);
  s_ack = s_service->createCharacteristic(BLE_ACK_UUID, NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_NR | NIMBLE_PROPERTY::WRITE_ENC);
  s_ctrl->setCallbacks(new CtrlCallbacks());
  s_ack->setCallbacks(new AckCallbacks());
  s_service->start();
  NimBLEAdvertising *adv = NimBLEDevice::getAdvertising();
  // The app filters scans by BLE_SERVICE_UUID, so the 128-bit UUID must sit in
  // the advertisement packet itself; a UUID that only lives in the scan
  // response is missed by some stacks. Name + UUID exceed the 31-byte legacy
  // ADV (3 + 12 + 18 = 33), so the name moves to the scan response (the app
  // tolerates a missing name and still matches on the UUID).
  {
    NimBLEAdvertisementData advData;
    advData.setFlags(0x06);
    advData.addServiceUUID(BLE_SERVICE_UUID);
    if (!adv->setAdvertisementData(advData)) {
      LOG_E("BLE adv data fail");
    }
    NimBLEAdvertisementData scanResp;
    scanResp.setName(BLE_DEVICE_NAME);
    scanResp.addServiceUUID(BLE_SERVICE_UUID);
    if (!adv->setScanResponseData(scanResp)) {
      LOG_E("BLE scan rsp fail");
    }
    // Without this, a controller resync drops the scan response (and with it
    // the UUID) while advertising silently continues.
    adv->enableScanResponse(true);
  }
  if (!adv->start()) {
    LOG_E("BLE adv start fail");
  }
  Serial.println("BLE advertising started");
  crypto_init();
  crypto_load_or_gen_key();
  return true;
}

bool ble_is_connected() { return s_connected; }
bool ble_is_handshaked() { return s_handshaked; }
bool ble_is_encrypted() { return s_encrypted && s_connected; }
bool ble_peer_authenticated() { return auth_is_authenticated(); }
bool ble_get_session_key(uint8_t out[CRYPTO_KEY_BYTES]) { return auth_get_session_key(out); }
bool ble_enrollment_active() { return auth_enrollment_active(); }
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

void ble_check_advertising() {
  // Self-heal: if the link is down but advertising is not active (failed
  // restart after disconnect, controller resync), bring it back so the app can
  // always rediscover the pendant. Throttled to avoid log/host spam.
  if (s_connected || !s_server) {
    return;
  }
  static uint32_t last_try_ms = 0;
  uint32_t now = millis();
  if ((now - last_try_ms) < 2000) {
    return;
  }
  last_try_ms = now;
  NimBLEAdvertising *adv = NimBLEDevice::getAdvertising();
  if (adv && !adv->isAdvertising()) {
    LOG_W("BLE adv restart");
    adv->start();
  }
}

void ble_disconnect() {
  if (s_server && s_conn_handle != 0xFFFF) {
    s_server->disconnect(s_conn_handle);
  }
}

bool ble_send_raw(const uint8_t *data, size_t len) {
  if (!s_connected || !s_handshaked || !auth_is_authenticated()) return false;
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
