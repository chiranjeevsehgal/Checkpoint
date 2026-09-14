#include "auth.h"
#include "crypto.h"
#include "power.h"
#include <Preferences.h>

namespace {

uint8_t s_device_id[AUTH_DEVICE_ID_BYTES];
uint8_t s_claim[AUTH_CLAIM_KEY_BYTES];
uint8_t s_cloud_secret[AUTH_CLOUD_SECRET_BYTES];
uint8_t s_client_id[AUTH_MAX_CLIENTS][AUTH_CLIENT_ID_BYTES];
uint8_t s_client_key[AUTH_MAX_CLIENTS][AUTH_CLIENT_KEY_BYTES];
bool s_slot_valid[AUTH_MAX_CLIENTS];
bool s_ready = false;

uint32_t s_enroll_until_ms = 0;

bool s_handshake_active = false;
uint32_t s_session = 0;
uint8_t s_pending_client[AUTH_CLIENT_ID_BYTES];
uint8_t s_device_nonce[AUTH_NONCE_BYTES];
bool s_enroll_attempt = false;

uint8_t s_transcript[128];
size_t s_transcript_len = 0;
uint8_t s_session_key[CRYPTO_KEY_BYTES];
bool s_has_session_key = false;
bool s_authenticated = false;

bool s_pending_enroll = false;
uint8_t s_pending_enroll_id[AUTH_CLIENT_ID_BYTES];
uint8_t s_pending_enroll_key[AUTH_CLIENT_KEY_BYTES];

const char kAuthDom[] = "checkpoint-auth-v3";
const char kServerDom[] = "checkpoint-server-finish-v3";
const char kClientDom[] = "checkpoint-client-finish-v3";

void build_transcript(const uint8_t device_id[16], const uint8_t client_id[16], uint32_t session_id,
                      const uint8_t device_nonce[16], const uint8_t client_nonce[16], uint8_t mode,
                      uint8_t *out, size_t *out_len) {
  uint8_t *p = out;
  memcpy(p, kAuthDom, sizeof(kAuthDom) - 1);
  p += sizeof(kAuthDom) - 1;
  memcpy(p, device_id, 16);
  p += 16;
  memcpy(p, client_id, 16);
  p += 16;
  p[0] = session_id & 0xFF;
  p[1] = (session_id >> 8) & 0xFF;
  p[2] = (session_id >> 16) & 0xFF;
  p[3] = (session_id >> 24) & 0xFF;
  p += 4;
  memcpy(p, device_nonce, 16);
  p += 16;
  memcpy(p, client_nonce, 16);
  p += 16;
  *p++ = mode;
  *out_len = (size_t)(p - out);
}

bool load_slots() {
  Preferences pref;
  if (!pref.begin("ckauth", true)) return false;
  for (int i = 0; i < AUTH_MAX_CLIENTS; i++) {
    char id_key[8];
    char key_key[8];
    snprintf(id_key, sizeof(id_key), "c%d_id", i);
    snprintf(key_key, sizeof(key_key), "c%d_key", i);
    s_slot_valid[i] = false;
    if (pref.getBytesLength(id_key) == AUTH_CLIENT_ID_BYTES &&
        pref.getBytesLength(key_key) == AUTH_CLIENT_KEY_BYTES) {
      pref.getBytes(id_key, s_client_id[i], AUTH_CLIENT_ID_BYTES);
      pref.getBytes(key_key, s_client_key[i], AUTH_CLIENT_KEY_BYTES);
      s_slot_valid[i] = true;
    } else {
      memset(s_client_id[i], 0, AUTH_CLIENT_ID_BYTES);
      memset(s_client_key[i], 0, AUTH_CLIENT_KEY_BYTES);
    }
  }
  pref.end();
  return true;
}

int find_slot(const uint8_t client_id[16]) {
  for (int i = 0; i < AUTH_MAX_CLIENTS; i++) {
    if (!s_slot_valid[i]) continue;
    if (memcmp(s_client_id[i], client_id, 16) == 0) return i;
  }
  return -1;
}

int free_slot() {
  for (int i = 0; i < AUTH_MAX_CLIENTS; i++) {
    if (!s_slot_valid[i]) return i;
  }
  return -1;
}

bool persist_slot(int slot) {
  Preferences pref;
  if (!pref.begin("ckauth", false)) return false;
  char id_key[8];
  char key_key[8];
  snprintf(id_key, sizeof(id_key), "c%d_id", slot);
  snprintf(key_key, sizeof(key_key), "c%d_key", slot);
  pref.putBytes(id_key, s_client_id[slot], AUTH_CLIENT_ID_BYTES);
  pref.putBytes(key_key, s_client_key[slot], AUTH_CLIENT_KEY_BYTES);
  pref.end();
  return true;
}

void clear_slot_ram(int slot) {
  s_slot_valid[slot] = false;
  memset(s_client_id[slot], 0, AUTH_CLIENT_ID_BYTES);
  memset(s_client_key[slot], 0, AUTH_CLIENT_KEY_BYTES);
}

bool remove_slot_nvs(int slot) {
  Preferences pref;
  if (!pref.begin("ckauth", false)) return false;
  char id_key[8];
  char key_key[8];
  snprintf(id_key, sizeof(id_key), "c%d_id", slot);
  snprintf(key_key, sizeof(key_key), "c%d_key", slot);
  pref.remove(id_key);
  pref.remove(key_key);
  pref.end();
  return true;
}

}  // namespace

bool auth_init() {
  memset(s_session_key, 0, sizeof(s_session_key));
  s_has_session_key = false;
  s_authenticated = false;
  s_handshake_active = false;
  s_pending_enroll = false;
  s_enroll_until_ms = 0;
  Preferences pref;
  if (!pref.begin("ckauth", false)) return false;
  if (pref.getBytesLength("dev_id") != AUTH_DEVICE_ID_BYTES) {
    uint8_t gen[AUTH_DEVICE_ID_BYTES];
    crypto_random_bytes(gen, sizeof(gen));
    pref.putBytes("dev_id", gen, sizeof(gen));
    memset(gen, 0, sizeof(gen));
  }
  if (pref.getBytesLength("claim") != AUTH_CLAIM_KEY_BYTES) {
    uint8_t gen[AUTH_CLAIM_KEY_BYTES];
    crypto_random_bytes(gen, sizeof(gen));
    pref.putBytes("claim", gen, sizeof(gen));
    memset(gen, 0, sizeof(gen));
  }
  if (pref.getBytesLength("cloud") != AUTH_CLOUD_SECRET_BYTES) {
    uint8_t gen[AUTH_CLOUD_SECRET_BYTES];
    crypto_random_bytes(gen, sizeof(gen));
    pref.putBytes("cloud", gen, sizeof(gen));
    memset(gen, 0, sizeof(gen));
  }
  pref.getBytes("dev_id", s_device_id, AUTH_DEVICE_ID_BYTES);
  pref.getBytes("claim", s_claim, AUTH_CLAIM_KEY_BYTES);
  pref.getBytes("cloud", s_cloud_secret, AUTH_CLOUD_SECRET_BYTES);
  pref.end();
  load_slots();
  s_ready = true;
  return true;
}

int auth_trusted_count() {
  int n = 0;
  for (int i = 0; i < AUTH_MAX_CLIENTS; i++) {
    if (s_slot_valid[i]) n++;
  }
  return n;
}

bool auth_get_device_id(uint8_t out[AUTH_DEVICE_ID_BYTES]) {
  if (!out || !s_ready) return false;
  memcpy(out, s_device_id, AUTH_DEVICE_ID_BYTES);
  return true;
}

bool auth_export_claim_key(uint8_t out[AUTH_CLAIM_KEY_BYTES]) {
  if (!out || !s_ready) return false;
  memcpy(out, s_claim, AUTH_CLAIM_KEY_BYTES);
  return true;
}

bool auth_get_cloud_secret(uint8_t out[AUTH_CLOUD_SECRET_BYTES]) {
  if (!out || !s_ready) return false;
  memcpy(out, s_cloud_secret, AUTH_CLOUD_SECRET_BYTES);
  return true;
}

bool auth_cloud_secret_hash(uint8_t out[32]) {
  if (!out || !s_ready) return false;
  return crypto_sha256(s_cloud_secret, AUTH_CLOUD_SECRET_BYTES, out);
}

bool auth_enrollment_active() {
  if (s_enroll_until_ms == 0) return false;
  if ((int32_t)(millis() - s_enroll_until_ms) >= 0) {
    s_enroll_until_ms = 0;
    return false;
  }
  return true;
}

bool auth_open_enrollment(uint32_t duration_ms) {
  if (!s_ready || duration_ms == 0) return false;
  s_enroll_until_ms = millis() + duration_ms;
  return true;
}

void auth_close_enrollment() { s_enroll_until_ms = 0; }

bool auth_client_exists(const uint8_t client_id[AUTH_CLIENT_ID_BYTES]) {
  if (!client_id) return false;
  return find_slot(client_id) >= 0;
}

bool auth_begin(uint32_t session_id, bool enroll_requested, const uint8_t device_nonce[AUTH_NONCE_BYTES]) {
  if (!device_nonce) return false;
  s_session = session_id;
  memset(s_pending_client, 0, sizeof(s_pending_client));
  memcpy(s_device_nonce, device_nonce, AUTH_NONCE_BYTES);
  s_enroll_attempt = enroll_requested && auth_enrollment_active();
  s_handshake_active = true;
  s_has_session_key = false;
  s_authenticated = false;
  s_pending_enroll = false;
  memset(s_session_key, 0, sizeof(s_session_key));
  memset(s_transcript, 0, sizeof(s_transcript));
  s_transcript_len = 0;
  return true;
}

bool auth_verify_client_proof(const uint8_t client_id[AUTH_CLIENT_ID_BYTES],
                              const uint8_t client_nonce[AUTH_NONCE_BYTES], const uint8_t proof[AUTH_PROOF_BYTES],
                              bool *out_enroll, uint8_t server_proof_out[AUTH_PROOF_BYTES]) {
  if (!s_handshake_active || !client_id || !client_nonce || !proof || !server_proof_out) return false;
  memcpy(s_pending_client, client_id, AUTH_CLIENT_ID_BYTES);
  uint8_t mode = s_enroll_attempt ? 1 : 0;
  build_transcript(s_device_id, s_pending_client, s_session, s_device_nonce, client_nonce, mode, s_transcript,
                   &s_transcript_len);

  const uint8_t *key = nullptr;
  uint8_t derived_client[AUTH_CLIENT_KEY_BYTES];
  bool derived = false;
  if (s_enroll_attempt) {
    if (free_slot() < 0) return false;
    if (!crypto_derive_client_key_v3(s_claim, s_device_nonce, client_nonce, s_device_id, s_pending_client,
                                     derived_client)) {
      return false;
    }
    key = derived_client;
    derived = true;
  } else {
    int slot = find_slot(s_pending_client);
    if (slot < 0) {
      memset(derived_client, 0, sizeof(derived_client));
      return false;
    }
    key = s_client_key[slot];
  }

  uint8_t expect[AUTH_PROOF_BYTES];
  if (!crypto_hmac_sha256(key, AUTH_CLIENT_KEY_BYTES, s_transcript, s_transcript_len, expect)) {
    memset(derived_client, 0, sizeof(derived_client));
    return false;
  }
  bool ok = crypto_verify_hmac_ct(expect, proof);
  memset(expect, 0, sizeof(expect));
  if (!ok) {
    memset(derived_client, 0, sizeof(derived_client));
    return false;
  }

  const uint8_t *sess_ikm = derived ? derived_client : key;
  uint8_t sess[CRYPTO_KEY_BYTES];
  if (!crypto_derive_session_key_v3(sess_ikm, s_device_nonce, client_nonce, s_device_id, s_pending_client,
                                    s_session, sess)) {
    memset(derived_client, 0, sizeof(derived_client));
    return false;
  }
  memcpy(s_session_key, sess, CRYPTO_KEY_BYTES);
  memset(sess, 0, sizeof(sess));
  s_has_session_key = true;

  if (derived) {
    s_pending_enroll = true;
    memcpy(s_pending_enroll_id, s_pending_client, AUTH_CLIENT_ID_BYTES);
    memcpy(s_pending_enroll_key, derived_client, AUTH_CLIENT_KEY_BYTES);
  }
  memset(derived_client, 0, sizeof(derived_client));

  uint8_t msg[32 + 128];
  memcpy(msg, kServerDom, sizeof(kServerDom) - 1);
  memcpy(msg + sizeof(kServerDom) - 1, s_transcript, s_transcript_len);
  if (!crypto_hmac_sha256(s_session_key, CRYPTO_KEY_BYTES, msg, sizeof(kServerDom) - 1 + s_transcript_len,
                          server_proof_out)) {
    memset(msg, 0, sizeof(msg));
    return false;
  }
  memset(msg, 0, sizeof(msg));
  if (out_enroll) *out_enroll = s_enroll_attempt;
  return true;
}

bool auth_verify_client_finish(const uint8_t finish[AUTH_PROOF_BYTES]) {
  if (!s_handshake_active || !s_has_session_key || !finish) return false;
  uint8_t msg[32 + 128];
  memcpy(msg, kClientDom, sizeof(kClientDom) - 1);
  memcpy(msg + sizeof(kClientDom) - 1, s_transcript, s_transcript_len);
  uint8_t expect[AUTH_PROOF_BYTES];
  if (!crypto_hmac_sha256(s_session_key, CRYPTO_KEY_BYTES, msg, sizeof(kClientDom) - 1 + s_transcript_len,
                          expect)) {
    memset(msg, 0, sizeof(msg));
    return false;
  }
  memset(msg, 0, sizeof(msg));
  if (!crypto_verify_hmac_ct(expect, finish)) {
    memset(expect, 0, sizeof(expect));
    return false;
  }
  memset(expect, 0, sizeof(expect));
  if (s_pending_enroll) {
    int slot = free_slot();
    if (slot < 0) return false;
    memcpy(s_client_id[slot], s_pending_enroll_id, AUTH_CLIENT_ID_BYTES);
    memcpy(s_client_key[slot], s_pending_enroll_key, AUTH_CLIENT_KEY_BYTES);
    s_slot_valid[slot] = true;
    persist_slot(slot);
    memset(s_pending_enroll_id, 0, sizeof(s_pending_enroll_id));
    memset(s_pending_enroll_key, 0, sizeof(s_pending_enroll_key));
    s_pending_enroll = false;
    auth_close_enrollment();
  }
  s_authenticated = true;
  return true;
}

bool auth_is_authenticated() { return s_ready && s_authenticated && s_has_session_key; }

bool auth_get_session_key(uint8_t out[CRYPTO_KEY_BYTES]) {
  if (!out || !auth_is_authenticated()) return false;
  memcpy(out, s_session_key, CRYPTO_KEY_BYTES);
  return true;
}

void auth_clear_session() {
  s_handshake_active = false;
  s_authenticated = false;
  s_has_session_key = false;
  s_pending_enroll = false;
  memset(s_session_key, 0, sizeof(s_session_key));
  memset(s_transcript, 0, sizeof(s_transcript));
  s_transcript_len = 0;
  memset(s_pending_client, 0, sizeof(s_pending_client));
  memset(s_device_nonce, 0, sizeof(s_device_nonce));
  memset(s_pending_enroll_id, 0, sizeof(s_pending_enroll_id));
  memset(s_pending_enroll_key, 0, sizeof(s_pending_enroll_key));
}

bool auth_get_slot(int slot, uint8_t id_out[AUTH_CLIENT_ID_BYTES]) {
  if (slot < 0 || slot >= AUTH_MAX_CLIENTS || !id_out) return false;
  if (!s_slot_valid[slot]) return false;
  memcpy(id_out, s_client_id[slot], AUTH_CLIENT_ID_BYTES);
  return true;
}

bool auth_forget_client(int slot) {
  if (slot < 0 || slot >= AUTH_MAX_CLIENTS) return false;
  if (!remove_slot_nvs(slot)) return false;
  clear_slot_ram(slot);
  return true;
}

bool auth_drop_first_slot() {
  if (!s_slot_valid[0] && !s_slot_valid[1]) return false;
  if (s_slot_valid[1]) {
    memcpy(s_client_id[0], s_client_id[1], AUTH_CLIENT_ID_BYTES);
    memcpy(s_client_key[0], s_client_key[1], AUTH_CLIENT_KEY_BYTES);
    s_slot_valid[0] = true;
    persist_slot(0);
  } else {
    remove_slot_nvs(0);
    clear_slot_ram(0);
  }
  auth_forget_client(1);
  return true;
}

void auth_clear_slots() {
  for (int i = 0; i < AUTH_MAX_CLIENTS; i++) auth_forget_client(i);
  auth_clear_session();
  auth_close_enrollment();
}

void auth_factory_reset() { auth_clear_slots(); }

namespace {

void print_hex(const uint8_t *data, size_t len) {
  for (size_t i = 0; i < len; i++) {
    char buf[3];
    snprintf(buf, sizeof(buf), "%02x", data[i]);
    Serial.print(buf);
  }
}

}  // namespace

void auth_usb_poll() {
  static String line;
  while (Serial.available()) {
    char c = (char)Serial.read();
    if (c == '\r') continue;
    if (c != '\n') {
      if (line.length() < 64) line += c;
      continue;
    }
    line.trim();
    if (line == "auth list") {
      uint8_t dev[AUTH_DEVICE_ID_BYTES];
      auth_get_device_id(dev);
      Serial.print("device ");
      print_hex(dev, sizeof(dev));
      Serial.println();
      Serial.printf("trusted %d/%d enroll %s\n", auth_trusted_count(), AUTH_MAX_CLIENTS,
                    auth_enrollment_active() ? "open" : "closed");
      for (int i = 0; i < AUTH_MAX_CLIENTS; i++) {
        uint8_t id[AUTH_CLIENT_ID_BYTES];
        if (auth_get_slot(i, id)) {
          Serial.printf("slot %d ", i);
          print_hex(id, 4);
          Serial.println("...");
        } else {
          Serial.printf("slot %d empty\n", i);
        }
      }
    } else if (line == "auth reset") {
      auth_factory_reset();
      Serial.println("auth slots cleared");
    } else if (line.startsWith("auth forget ")) {
      int slot = line.substring(12).toInt();
      if (line.substring(12) == "0") slot = 0;
      if (slot < 0 || slot >= AUTH_MAX_CLIENTS) {
        Serial.println("usage: auth forget 0|1");
      } else if (auth_forget_client(slot)) {
        Serial.printf("slot %d forgotten\n", slot);
      } else {
        Serial.println("forget failed");
      }
    } else if (line == "auth export") {
      uint8_t dev[AUTH_DEVICE_ID_BYTES];
      uint8_t claim[AUTH_CLAIM_KEY_BYTES];
      auth_get_device_id(dev);
      auth_export_claim_key(claim);
      Serial.print("device ");
      print_hex(dev, sizeof(dev));
      Serial.println();
      Serial.print("claim ");
      print_hex(claim, sizeof(claim));
      Serial.println();
      Serial.print("uri checkpoint://claim?device=");
      print_hex(dev, sizeof(dev));
      Serial.print("&key=");
      print_hex(claim, sizeof(claim));
      Serial.println();
      Serial.println("keep this secret — it enrolls new devices");
      memset(claim, 0, sizeof(claim));
      memset(dev, 0, sizeof(dev));
    } else if (line == "auth provision") {
      uint8_t dev[AUTH_DEVICE_ID_BYTES];
      uint8_t hash[32];
      auth_get_device_id(dev);
      auth_cloud_secret_hash(hash);
      Serial.print("device ");
      print_hex(dev, sizeof(dev));
      Serial.println();
      Serial.print("cloud-sha256 ");
      print_hex(hash, sizeof(hash));
      Serial.println();
      memset(dev, 0, sizeof(dev));
      memset(hash, 0, sizeof(hash));
    } else if (line == "power sleep") {
      Serial.println("sleeping");
      Serial.flush();
      power_sleep_now();
    } else if (line.length() > 0) {
      Serial.println("commands: auth list | auth forget 0|1 | auth reset | auth export | auth provision | power sleep");
    }
    line = "";
  }
}
