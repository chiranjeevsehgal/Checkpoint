#include "crypto.h"
#include "mbedtls/ccm.h"
#include "mbedtls/sha256.h"
#include <Preferences.h>

static uint8_t s_master_key[CRYPTO_KEY_BYTES];
static bool s_has_key = false;
static mbedtls_ccm_context s_ccm;
static SemaphoreHandle_t s_crypto_mutex = nullptr;

static bool crypto_lock(uint32_t timeout_ms = 500) {
  if (!s_crypto_mutex) s_crypto_mutex = xSemaphoreCreateMutex();
  if (!s_crypto_mutex) return false;
  return xSemaphoreTake(s_crypto_mutex, pdMS_TO_TICKS(timeout_ms)) == pdTRUE;
}

static void crypto_unlock() {
  if (s_crypto_mutex) xSemaphoreGive(s_crypto_mutex);
}

bool crypto_init() {
  if (!s_crypto_mutex) s_crypto_mutex = xSemaphoreCreateMutex();
  mbedtls_ccm_init(&s_ccm);
  s_has_key = false;
  memset(s_master_key, 0, sizeof(s_master_key));
  return true;
}

bool crypto_set_key(const uint8_t key[CRYPTO_KEY_BYTES]) {
  if (!key) return false;
  if (!crypto_lock()) return false;
  memcpy(s_master_key, key, CRYPTO_KEY_BYTES);
  int rc = mbedtls_ccm_setkey(&s_ccm, MBEDTLS_CIPHER_ID_AES, s_master_key, 128);
  if (rc == 0) s_has_key = true;
  crypto_unlock();
  return rc == 0;
}

bool crypto_has_key() {
  if (!crypto_lock(100)) return s_has_key;
  bool v = s_has_key;
  crypto_unlock();
  return v;
}

bool crypto_get_key(uint8_t out[CRYPTO_KEY_BYTES]) {
  if (!out) return false;
  if (!crypto_lock()) return false;
  if (!s_has_key) { crypto_unlock(); return false; }
  memcpy(out, s_master_key, CRYPTO_KEY_BYTES);
  crypto_unlock();
  return true;
}

bool crypto_load_or_gen_key() {
  // Serialize NVS access and key update; crypto_set_key takes its own lock
  // so read NVS first without holding crypto lock, then set.
  Preferences pref;
  if (pref.begin("checkpoint", false)) {
    size_t len = pref.getBytesLength("ccmmaster");
    if (len == CRYPTO_KEY_BYTES) {
      uint8_t tmp[CRYPTO_KEY_BYTES];
      pref.getBytes("ccmmaster", tmp, CRYPTO_KEY_BYTES);
      pref.end();
      bool ok = crypto_set_key(tmp);
      memset(tmp, 0, sizeof(tmp));
      return ok;
    }
    pref.end();
  }
  // No key — generate new under checkpoint (still serialized by brief crypto lock)
  if (!crypto_lock(1000)) return false;
  // Re-check after lock in case another task just created it
  crypto_unlock();
  if (pref.begin("checkpoint", false)) {
    size_t len2 = pref.getBytesLength("ccmmaster");
    if (len2 == CRYPTO_KEY_BYTES) {
      uint8_t tmp2[CRYPTO_KEY_BYTES];
      pref.getBytes("ccmmaster", tmp2, CRYPTO_KEY_BYTES);
      pref.end();
      bool ok2 = crypto_set_key(tmp2);
      memset(tmp2, 0, sizeof(tmp2));
      return ok2;
    }
    pref.end();
  }
  if (!pref.begin("checkpoint", false)) return false;
  uint8_t gen[CRYPTO_KEY_BYTES];
  for (int i = 0; i < CRYPTO_KEY_BYTES; i++) gen[i] = (uint8_t)esp_random();
  pref.putBytes("ccmmaster", gen, CRYPTO_KEY_BYTES);
  pref.end();
  memset(gen, 0, sizeof(gen));
  pref.begin("checkpoint", true);
  uint8_t stored[CRYPTO_KEY_BYTES];
  pref.getBytes("ccmmaster", stored, CRYPTO_KEY_BYTES);
  pref.end();
  bool ok = crypto_set_key(stored);
  memset(stored, 0, sizeof(stored));
  return ok;
}

bool crypto_encrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *plain, size_t plain_len, const uint8_t *aad, size_t aad_len, uint8_t *cipher, uint8_t tag[CRYPTO_TAG_BYTES]) {
  if (!nonce || !tag) return false;
  if (plain_len && (!plain || !cipher)) return false;
  if (!crypto_lock()) return false;
  if (!s_has_key) { crypto_unlock(); return false; }
  int rc = mbedtls_ccm_encrypt_and_tag(&s_ccm, plain_len, nonce, CRYPTO_NONCE_BYTES, aad, aad_len, plain, cipher, tag, CRYPTO_TAG_BYTES);
  crypto_unlock();
  return rc == 0;
}

bool crypto_decrypt(const uint8_t nonce[CRYPTO_NONCE_BYTES], const uint8_t *cipher, size_t cipher_len, const uint8_t *aad, size_t aad_len, const uint8_t tag[CRYPTO_TAG_BYTES], uint8_t *plain) {
  if (!nonce || !tag) return false;
  if (cipher_len && (!cipher || !plain)) return false;
  if (!crypto_lock()) return false;
  if (!s_has_key) { crypto_unlock(); return false; }
  int rc = mbedtls_ccm_auth_decrypt(&s_ccm, cipher_len, nonce, CRYPTO_NONCE_BYTES, aad, aad_len, cipher, plain, tag, CRYPTO_TAG_BYTES);
  crypto_unlock();
  return rc == 0;
}

void crypto_build_nonce(uint32_t session_id, uint32_t file_id, uint16_t seq, uint8_t out[CRYPTO_NONCE_BYTES]) {
  // NONCE INVARIANT: (session_id, file_id, seq) must be unique per key.
  // Retries reuse same nonce only because plaintext is identical; do not
  // re-encrypt different data with the same tuple.
  memset(out, 0, CRYPTO_NONCE_BYTES);
  out[0] = session_id & 0xFF;
  out[1] = (session_id >> 8) & 0xFF;
  out[2] = (session_id >> 16) & 0xFF;
  out[3] = (session_id >> 24) & 0xFF;
  out[4] = file_id & 0xFF;
  out[5] = (file_id >> 8) & 0xFF;
  out[6] = (file_id >> 16) & 0xFF;
  out[7] = (file_id >> 24) & 0xFF;
  out[8] = seq & 0xFF;
  out[9] = (seq >> 8) & 0xFF;
  out[10] = 0xA5;
  out[11] = 0x5A;
}

bool crypto_derive_file_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint32_t file_id, uint8_t out[CRYPTO_KEY_BYTES]) {
  // HKDF-inspired but not RFC5869: PRK=SHA256(IKM), OKM=SHA256(PRK||info||0x01).
  // Replace with mbedtls_hkdf if this key protects higher-stakes assets.
  uint8_t info[8];
  info[0] = session_id & 0xFF;
  info[1] = (session_id >> 8) & 0xFF;
  info[2] = (session_id >> 16) & 0xFF;
  info[3] = (session_id >> 24) & 0xFF;
  info[4] = file_id & 0xFF;
  info[5] = (file_id >> 8) & 0xFF;
  info[6] = (file_id >> 16) & 0xFF;
  info[7] = (file_id >> 24) & 0xFF;
  uint8_t prk[32];
  mbedtls_sha256(master_key, CRYPTO_KEY_BYTES, prk, 0);
  uint8_t tmp[32 + 8 + 1];
  memcpy(tmp, prk, 32);
  memcpy(tmp + 32, info, 8);
  tmp[40] = 0x01;
  uint8_t hash[32];
  mbedtls_sha256(tmp, sizeof(tmp), hash, 0);
  memcpy(out, hash, CRYPTO_KEY_BYTES);
  memset(prk, 0, sizeof(prk));
  memset(tmp, 0, sizeof(tmp));
  memset(hash, 0, sizeof(hash));
  return true;
}
