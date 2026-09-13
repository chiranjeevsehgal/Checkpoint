#pragma once
#include <Arduino.h>
#include "config.h"

enum BleState { BLE_DISCONNECTED, BLE_HANDSHAKING, BLE_READY, BLE_BUSY };

bool ble_init();
bool ble_is_connected();
bool ble_is_handshaked();
bool ble_is_encrypted();
bool ble_peer_authenticated();
bool ble_get_session_key(uint8_t out[CRYPTO_KEY_BYTES]);
bool ble_enrollment_active();
BleState ble_state();
uint32_t ble_session_id();
bool ble_handshake_timed_out();
void ble_check_handshake_timeout();
// Force-drop the link so both sides restart from a clean session.
void ble_disconnect();
void ble_check_final_diag();
void ble_check_advertising();
bool ble_send_packet(uint8_t type, uint16_t seq, const uint8_t *payload, uint16_t len);
bool ble_send_raw(const uint8_t *data, size_t len);
void ble_on_packet(void (*cb)(const uint8_t *data, size_t len));
uint16_t ble_conn_interval_ms();
uint16_t ble_mtu_negotiated();
uint8_t ble_phy();
