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

void crypto_build_nonce(uint32_t session_id, uint64_t file_uid, uint16_t seq, uint8_t out[CRYPTO_NONCE_BYTES]) {
  // NONCE INVARIANT: (session_id, file_uid, seq) must be unique per key.
  // Deterministic 96-bit digest of the tuple (v2: uid is 64-bit, so the raw
  // tuple no longer fits 12 bytes). Retries reuse the same nonce only
  // because the plaintext is identical.
  static const char kDom[] = "checkpoint-nonce-v1";
  uint8_t msg[19 + 4 + 8 + 2];
  memcpy(msg, kDom, 19);
  msg[19] = session_id & 0xFF;
  msg[20] = (session_id >> 8) & 0xFF;
  msg[21] = (session_id >> 16) & 0xFF;
  msg[22] = (session_id >> 24) & 0xFF;
  for (int i = 0; i < 8; i++) msg[23 + i] = (uint8_t)((file_uid >> (i * 8)) & 0xFF);
  msg[31] = seq & 0xFF;
  msg[32] = (seq >> 8) & 0xFF;
  uint8_t hash[32];
  mbedtls_sha256(msg, sizeof(msg), hash, 0);
  memcpy(out, hash, CRYPTO_NONCE_BYTES);
  memset(msg, 0, sizeof(msg));
  memset(hash, 0, sizeof(hash));
}

static void hmac_sha256(const uint8_t *key, size_t key_len,
                        const uint8_t *msg, size_t msg_len, uint8_t out[32]) {
  // Fixed small inputs only (HKDF below passes msg_len <= 65).
  if (!msg || msg_len > 65) { memset(out, 0, 32); return; }
  uint8_t key_block[64] = {0};
  if (key_len > sizeof(key_block)) {
    mbedtls_sha256(key, key_len, key_block, 0);
  } else if (key && key_len > 0) {
    memcpy(key_block, key, key_len);
  }
  uint8_t inner[64 + 65];
  uint8_t outer[64 + 32];
  for (int i = 0; i < 64; i++) {
    inner[i] = key_block[i] ^ 0x36;
    outer[i] = key_block[i] ^ 0x5c;
  }
  memcpy(inner + 64, msg, msg_len);
  mbedtls_sha256(inner, 64 + msg_len, outer + 64, 0);
  mbedtls_sha256(outer, 64 + 32, out, 0);
  memset(key_block, 0, sizeof(key_block));
  memset(inner, 0, sizeof(inner));
  memset(outer, 0, sizeof(outer));
}

bool crypto_hkdf_sha256(const uint8_t *salt, size_t salt_len,
                        const uint8_t *ikm, size_t ikm_len,
                        const uint8_t *info, size_t info_len,
                        uint8_t *okm, size_t okm_len) {
  if (!ikm || ikm_len == 0 || !info || !okm) return false;
  if (okm_len == 0 || okm_len > 32 || info_len > 64) return false;
  uint8_t prk[32];
  hmac_sha256(salt, salt_len, ikm, ikm_len, prk);
  uint8_t msg[64 + 1];
  memcpy(msg, info, info_len);
  msg[info_len] = 0x01;
  uint8_t t[32];
  hmac_sha256(prk, sizeof(prk), msg, info_len + 1, t);
  memcpy(okm, t, okm_len);
  memset(prk, 0, sizeof(prk));
  memset(msg, 0, sizeof(msg));
  memset(t, 0, sizeof(t));
  return true;
}

bool crypto_derive_file_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint64_t file_uid, uint8_t out[CRYPTO_KEY_BYTES]) {
  // RFC 5869 HKDF-SHA256, domain-separated from the session key below.
  // NOTE: callers pass the per-session key received in HELLO_ACK, whose own
  // derivation uses a different info string, so domains never collide.
  static const char kInfo[] = "checkpoint-file-v1";
  uint8_t info[sizeof(kInfo) - 1 + 4 + 8];
  memcpy(info, kInfo, sizeof(kInfo) - 1);
  info[18] = session_id & 0xFF;
  info[19] = (session_id >> 8) & 0xFF;
  info[20] = (session_id >> 16) & 0xFF;
  info[21] = (session_id >> 24) & 0xFF;
  for (int i = 0; i < 8; i++) info[22 + i] = (uint8_t)((file_uid >> (i * 8)) & 0xFF);
  uint8_t okm[CRYPTO_KEY_BYTES];
  bool ok = crypto_hkdf_sha256(nullptr, 0, master_key, CRYPTO_KEY_BYTES,
                               info, sizeof(info), okm, sizeof(okm));
  if (ok) memcpy(out, okm, CRYPTO_KEY_BYTES);
  memset(info, 0, sizeof(info));
  memset(okm, 0, sizeof(okm));
  return ok;
}

bool crypto_derive_session_key(const uint8_t master_key[CRYPTO_KEY_BYTES], uint32_t session_id, uint8_t out[CRYPTO_KEY_BYTES]) {
  // Sent in HELLO_ACK instead of the permanent master key: a captured
  // session key only compromises that session's files.
  static const char kInfo[] = "checkpoint-session-v1";
  uint8_t info[sizeof(kInfo) - 1 + 4];
  memcpy(info, kInfo, sizeof(kInfo) - 1);
  info[21] = session_id & 0xFF;
  info[22] = (session_id >> 8) & 0xFF;
  info[23] = (session_id >> 16) & 0xFF;
  info[24] = (session_id >> 24) & 0xFF;
  uint8_t okm[CRYPTO_KEY_BYTES];
  bool ok = crypto_hkdf_sha256(nullptr, 0, master_key, CRYPTO_KEY_BYTES,
                               info, sizeof(info), okm, sizeof(okm));
  if (ok) memcpy(out, okm, CRYPTO_KEY_BYTES);
  memset(info, 0, sizeof(info));
  memset(okm, 0, sizeof(okm));
  return ok;
}
