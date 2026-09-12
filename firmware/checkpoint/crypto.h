#pragma once
#include <Arduino.h>
#include "config.h"

bool crypto_init();
bool crypto_set_key(const uint8_t key[CRYPTO_KEY_BYTES]);
bool crypto_encrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *plain, size_t plain_len, const uint8_t *aad, size_t aad_len, uint8_t *cipher, uint8_t tag[CRYPTO_TAG_BYTES]);
bool crypto_decrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *cipher, size_t cipher_len, const uint8_t *aad, size_t aad_len, const uint8_t tag[CRYPTO_TAG_BYTES], uint8_t *plain);
void crypto_build_nonce(uint32_t session_id, uint64_t file_uid, uint16_t seq, uint8_t out[CRYPTO_NONCE_BYTES]);
// RFC 5869 HKDF-SHA256, single-block outputs only (okm_len <= 32).
bool crypto_hkdf_sha256(const uint8_t *salt, size_t salt_len, const uint8_t *ikm, size_t ikm_len, const uint8_t *info, size_t info_len, uint8_t *okm, size_t okm_len);
// HMAC-SHA256 over arbitrary-length messages (v3 auth transcripts are ~90B).
bool crypto_hmac_sha256(const uint8_t *key, size_t key_len, const uint8_t *msg, size_t msg_len, uint8_t out[32]);
// Constant-time 32-byte comparison for authentication proofs.
bool crypto_verify_hmac_ct(const uint8_t a[32], const uint8_t b[32]);
// Cryptographically random bytes (wraps esp_random).
bool crypto_random_bytes(uint8_t *out, size_t len);
bool crypto_derive_file_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint64_t file_uid, uint8_t out[CRYPTO_KEY_BYTES]);
// Protocol v3: session key derived locally from the per-client secret.
// Never transmitted. Replaces the v2 HELLO_ACK key transport.
bool crypto_derive_session_key_v3(const uint8_t client_key[32], const uint8_t device_nonce[16], const uint8_t client_nonce[16], const uint8_t device_id[16], const uint8_t client_id[16], uint32_t session_id, uint8_t out[CRYPTO_KEY_BYTES]);
// Protocol v3 enrollment: fresh per-device client key from the claim secret.
bool crypto_derive_client_key_v3(const uint8_t claim_key[32], const uint8_t device_nonce[16], const uint8_t client_nonce[16], const uint8_t device_id[16], const uint8_t client_id[16], uint8_t out[32]);
bool crypto_load_or_gen_key();
bool crypto_get_key(uint8_t out[CRYPTO_KEY_BYTES]);
bool crypto_has_key();
