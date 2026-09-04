#pragma once
#include <Arduino.h>
#include "config.h"

bool crypto_init();
bool crypto_set_key(const uint8_t key[CRYPTO_KEY_BYTES]);
bool crypto_encrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *plain, size_t plain_len, const uint8_t *aad, size_t aad_len, uint8_t *cipher, uint8_t tag[CRYPTO_TAG_BYTES]);
bool crypto_decrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *cipher, size_t cipher_len, const uint8_t *aad, size_t aad_len, const uint8_t tag[CRYPTO_TAG_BYTES], uint8_t *plain);
void crypto_build_nonce(uint32_t session_id, uint32_t file_id, uint16_t seq, uint8_t out[CRYPTO_NONCE_BYTES]);
bool crypto_derive_file_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint32_t file_id, uint8_t out[CRYPTO_KEY_BYTES]);
bool crypto_load_or_gen_key();
bool crypto_get_key(uint8_t out[CRYPTO_KEY_BYTES]);
bool crypto_has_key();
