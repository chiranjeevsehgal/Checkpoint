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
bool crypto_derive_file_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint64_t file_uid, uint8_t out[CRYPTO_KEY_BYTES]);
// Per-session key sent in HELLO_ACK so the permanent master never leaves the device.
bool crypto_derive_session_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint8_t out[CRYPTO_KEY_BYTES]);
bool crypto_load_or_gen_key();
bool crypto_get_key(uint8_t out[CRYPTO_KEY_BYTES]);
bool crypto_has_key();
