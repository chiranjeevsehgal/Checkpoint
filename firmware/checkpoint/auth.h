#pragma once
#include <Arduino.h>
#include "config.h"

#define AUTH_CLIENT_ID_BYTES 16
#define AUTH_CLIENT_KEY_BYTES 32
#define AUTH_DEVICE_ID_BYTES 16
#define AUTH_CLAIM_KEY_BYTES 32
#define AUTH_CLOUD_SECRET_BYTES 32
#define AUTH_NONCE_BYTES 16
#define AUTH_PROOF_BYTES 32
#define AUTH_MAX_CLIENTS 2

bool auth_init();

int auth_trusted_count();
bool auth_get_device_id(uint8_t out[AUTH_DEVICE_ID_BYTES]);
bool auth_export_claim_key(uint8_t out[AUTH_CLAIM_KEY_BYTES]);
// Cloud ownership secret: separate from the BLE claim key, never broadcast.
bool auth_get_cloud_secret(uint8_t out[AUTH_CLOUD_SECRET_BYTES]);
bool auth_cloud_secret_hash(uint8_t out[32]);

bool auth_enrollment_active();
bool auth_open_enrollment(uint32_t duration_ms);
void auth_close_enrollment();

bool auth_client_exists(const uint8_t client_id[AUTH_CLIENT_ID_BYTES]);

bool auth_begin(uint32_t session_id, bool enroll_requested, const uint8_t device_nonce[AUTH_NONCE_BYTES]);
bool auth_verify_client_proof(const uint8_t client_id[AUTH_CLIENT_ID_BYTES], const uint8_t client_nonce[AUTH_NONCE_BYTES], const uint8_t proof[AUTH_PROOF_BYTES], bool *out_enroll, uint8_t server_proof_out[AUTH_PROOF_BYTES]);
bool auth_verify_client_finish(const uint8_t finish[AUTH_PROOF_BYTES]);

bool auth_is_authenticated();
bool auth_get_session_key(uint8_t out[CRYPTO_KEY_BYTES]);
void auth_clear_session();

bool auth_get_slot(int slot, uint8_t id_out[AUTH_CLIENT_ID_BYTES]);
bool auth_forget_client(int slot);
bool auth_drop_first_slot();
// Clears trusted clients + active session, keeping device id, claim and cloud secret.
void auth_clear_slots();
void auth_factory_reset();
void auth_usb_poll();
