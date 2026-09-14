#include "control.h"
#include "config.h"
#include "auth.h"
#include "clock.h"
#include "crypto.h"
#include "protocol.h"
#include "ble_service.h"
#include "recorder.h"
#include "manifest.h"
#include "sd_manager.h"
#include "transfer.h"
#include "ui.h"
#include <Preferences.h>
#include "esp_heap_caps.h"

namespace {

// Request flags written by control_on_packet (NimBLE task), consumed by
// control_poll (loop task). Single-writer/single-reader volatiles.
volatile uint8_t s_rec_req = 0; // 0=none, CTRL_CMD_REC_START/STOP
volatile uint16_t s_rec_seq = 0;
volatile bool s_led_req = false;
volatile uint8_t s_led_muted_arg = 0;
volatile uint8_t s_led_bright_arg = HW_RGB_BRIGHTNESS;
volatile uint16_t s_led_seq = 0;
volatile uint8_t s_led_get_seq_valid = 0;
volatile uint16_t s_led_get_seq = 0;
volatile bool s_status_req = false;
volatile uint16_t s_status_seq = 0;
volatile bool s_denied_pending = false;
volatile bool s_storage_req = false;
volatile uint16_t s_storage_seq = 0;
volatile bool s_list_req = false;
volatile uint16_t s_list_start = 0;
volatile uint16_t s_list_seq = 0;
volatile bool s_del_req = false;
volatile uint16_t s_del_len = 0;
volatile uint8_t s_del_path[PROTO_MAX_PAYLOAD];
volatile uint16_t s_del_seq = 0;
volatile bool s_fetch_req = false;
volatile uint16_t s_fetch_len = 0;
volatile uint8_t s_fetch_path[PROTO_MAX_PAYLOAD];
volatile uint16_t s_fetch_seq = 0;
volatile bool s_erase_req = false;
volatile uint8_t s_erase_step = 0;
volatile uint16_t s_erase_seq = 0;
volatile bool s_sync_req = false;
volatile uint8_t s_sync_enabled_arg = 1;
volatile uint16_t s_sync_seq = 0;
volatile uint8_t s_sync_get_seq_valid = 0;
volatile uint16_t s_sync_get_seq = 0;
volatile bool s_time_req = false;
volatile uint64_t s_time_unix_s = 0;
volatile uint16_t s_time_seq = 0;
volatile bool s_cloud_req = false;
volatile uint16_t s_cloud_seq = 0;
volatile bool s_clear_slots_req = false;
volatile uint16_t s_clear_slots_seq = 0;
volatile bool s_forget_self_req = false;
volatile uint16_t s_forget_self_seq = 0;
// Erase arm timestamp, loop-task only (set/consumed in poll).
static uint32_t s_erase_armed_ms = 0;
// Set after the clear-slots ACK is sent; acted on the next poll tick so the
// indicate can flush before the session is invalidated.
static bool s_clear_slots_pending = false;
// Same ACK-then-act ordering for dropping the caller's own trusted slot.
static bool s_forget_self_pending = false;
// BLE auto-upload gate. Written only in control_poll (loop task), read in
// transfer_task; single-byte volatile matches the s_busy cross-task style.
static volatile bool s_sync_enabled = true;
// Last pushed status snapshot (loop task only). Powers the unsolicited
// STATUS_RESP push that keeps the host in sync after hardware-side changes
// (physical button, VAD transitions, new chunks) without host polling.
static uint8_t s_last_status[CTRL_STATUS_LEN] = {0};
static uint32_t s_last_push_ms = 0;
static bool s_has_last_status = false;
static constexpr uint32_t kStatusPushMinMs = 1000;

// Guards the req-flag handoff: NimBLE callback task writes, loop task
// (control_poll) reads+clears. Critical section, never held across
// recorder/NVS/BLE work — only flag copies.
static portMUX_TYPE s_ctrl_mux = portMUX_INITIALIZER_UNLOCKED;

constexpr uint16_t kNoSeq = 0xFFFF;
constexpr uint8_t kErrNotEncrypted = 0x01;

bool control_gate_ok() {
  return ble_is_connected() && ble_is_handshaked() && ble_is_encrypted() && ble_peer_authenticated();
}

void control_load_led() {
  Preferences pref;
  if (!pref.begin("checkpoint", true)) {
    return;
  }
  uint8_t muted = pref.getUChar("led_muted", 0);
  uint8_t bright = pref.getUChar("led_bright", HW_RGB_BRIGHTNESS);
  pref.end();
  if (muted > 1) {
    muted = 0;
  }
  if (!muted && bright < CTRL_BRIGHT_MIN) {
    bright = muted ? bright : CTRL_BRIGHT_MIN;
  }
  ui_set_muted(muted != 0);
  ui_set_brightness(bright);
}

void control_save_led(bool muted, uint8_t bright) {
  Preferences pref;
  if (!pref.begin("checkpoint", false)) {
    return;
  }
  pref.putUChar("led_muted", muted ? 1 : 0);
  pref.putUChar("led_bright", bright);
  pref.end();
}

bool control_load_sync_flag() {
  Preferences pref;
  if (!pref.begin("checkpoint", true)) {
    return true;
  }
  uint8_t v = pref.getUChar("sync_enabled", 1);
  pref.end();
  return v != 0;
}

void control_save_sync(bool enabled) {
  Preferences pref;
  if (!pref.begin("checkpoint", false)) {
    return;
  }
  pref.putUChar("sync_enabled", enabled ? 1 : 0);
  pref.end();
}

void control_send_denied() {
  uint8_t err = kErrNotEncrypted;
  ble_send_packet(PKT_ERROR, 0, &err, 1);
}

void control_send_cmd_resp(uint16_t seq, uint8_t cmd, uint8_t status,
                           const uint8_t *extra, uint8_t extra_len) {
  uint8_t payload[8];
  payload[0] = cmd;
  payload[1] = status;
  uint8_t len = 2;
  if (extra != nullptr && extra_len > 0 && (uint8_t)(len + extra_len) <= sizeof(payload)) {
    for (uint8_t i = 0; i < extra_len; i++) {
      payload[len++] = extra[i];
    }
  }
  ble_send_packet(PKT_CMD_RESP, seq, payload, len);
}

void control_build_status(uint8_t out[CTRL_STATUS_LEN]) {
  const bool rec = recorder_is_recording();
  const bool vad_active = recorder_vad_active();
  const bool vad_speech = recorder_vad_speech();
  const bool muted = ui_is_muted();
  const uint8_t bright = ui_get_brightness();
  float level_f = recorder_vad_level_dbfs();
  int level_i = (int)level_f;
  if (level_i < -90) {
    level_i = -90;
  } else if (level_i > 0) {
    level_i = 0;
  }
  size_t pend = manifest_pending_count();
  if (pend > 0xFFFF) {
    pend = 0xFFFF;
  }
  uint32_t chunks = recorder_chunks_written();
  uint32_t utt = recorder_vad_utterances();
  out[0] = rec ? 1 : 0;
  out[1] = vad_active ? 1 : 0;
  out[2] = vad_speech ? 1 : 0;
  out[3] = muted ? 1 : 0;
  out[4] = bright;
  out[5] = (uint8_t)(int8_t)level_i;
  out[6] = (uint8_t)(pend & 0xFF);
  out[7] = (uint8_t)((pend >> 8) & 0xFF);
  out[8] = (uint8_t)(chunks & 0xFF);
  out[9] = (uint8_t)((chunks >> 8) & 0xFF);
  out[10] = (uint8_t)((chunks >> 16) & 0xFF);
  out[11] = (uint8_t)((chunks >> 24) & 0xFF);
  out[12] = (uint8_t)(utt & 0xFF);
  out[13] = (uint8_t)((utt >> 8) & 0xFF);
  out[14] = (uint8_t)((utt >> 16) & 0xFF);
  out[15] = (uint8_t)((utt >> 24) & 0xFF);
  out[16] = s_sync_enabled ? 1 : 0;
}

static bool status_event_changed(const uint8_t *current, const uint8_t *previous) {
  if (current[0] != previous[0] || current[3] != previous[3] ||
      current[4] != previous[4] || current[16] != previous[16]) {
    return true;
  }
  // Layout: [6..7] pending, [8..15] chunks + utterances.
  // Skips [1..2] VAD and [5] mic level: live meter values visible on
  // explicit polls, never push triggers.
  if (memcmp(current + 6, previous + 6, 2) != 0) {
    return true;
  }
  return memcmp(current + 8, previous + 8, 8) != 0;
}

void control_push_status_if_changed() {
  if (!control_gate_ok()) {
    return;
  }
  uint8_t current[CTRL_STATUS_LEN];
  control_build_status(current);
  if (s_has_last_status && !status_event_changed(current, s_last_status)) {
    return;
  }
  if (s_has_last_status && (millis() - s_last_push_ms) < kStatusPushMinMs) {
    return;
  }
  if (ble_send_packet(PKT_STATUS_RESP, 0, current, CTRL_STATUS_LEN)) {
    memcpy(s_last_status, current, CTRL_STATUS_LEN);
    s_last_push_ms = millis();
    s_has_last_status = true;
  }
}

uint8_t control_do_rec_start() {
  if (recorder_is_recording()) {
    return CTRL_OK;
  }
  bool ok = recorder_start();
  if (!ok) {
    ui_signal_error();
    return CTRL_ERR_NOT_READY;
  }
#if VAD_ENABLE
  ui_signal_vad_listening();
#else
  ui_signal_recording(true);
#endif
  ui_note_remote_action();
  return CTRL_OK;
}

uint8_t control_do_rec_stop() {
  if (!recorder_is_recording()) {
    return CTRL_OK;
  }
  recorder_stop();
  ui_signal_recording(false);
  ui_note_remote_action();
  return CTRL_OK;
}

uint8_t control_do_led_set(uint8_t muted_arg, uint8_t bright_arg) {
  if (muted_arg > 1) {
    return CTRL_ERR_BAD_ARG;
  }
  bool muted = muted_arg != 0;
  uint8_t bright = bright_arg;
  if (!muted && bright < CTRL_BRIGHT_MIN) {
    bright = CTRL_BRIGHT_MIN;
  }
  bool changed = (muted != ui_is_muted()) || (bright != ui_get_brightness());
  ui_set_muted(muted);
  ui_set_brightness(bright);
  if (changed) {
    control_save_led(muted, bright);
  }
  return CTRL_OK;
}

void control_build_storage(uint8_t out[CTRL_STORAGE_LEN]) {
  uint64_t total = 0;
  uint64_t used = 0;
  (void)sd_card_usage(&total, &used); // false when unmounted: zeros tell host
  size_t files = manifest_entry_count();
  size_t pend = manifest_pending_count();
  if (files > 0xFFFF) files = 0xFFFF;
  if (pend > 0xFFFF) pend = 0xFFFF;
  for (int i = 0; i < 8; i++) out[i] = (uint8_t)((total >> (i * 8)) & 0xFF);
  for (int i = 0; i < 8; i++) out[8 + i] = (uint8_t)((used >> (i * 8)) & 0xFF);
  out[16] = (uint8_t)(files & 0xFF);
  out[17] = (uint8_t)((files >> 8) & 0xFF);
  out[18] = (uint8_t)(pend & 0xFF);
  out[19] = (uint8_t)((pend >> 8) & 0xFF);
}

// Packs one LIST page into out (<= out_size). Always writes the 5-byte
// header [start u16, total u16, count u8]; returns bytes used.
size_t control_pack_list(uint16_t start, uint8_t *out, size_t out_size, uint16_t *total_out) {
  if (total_out) *total_out = 0;
  if (!out || out_size < 5) return 0;
  // Snapshot the manifest on the heap (entries own Strings, like scan does).
  const size_t MAX_SNAP = 256;
  ManifestEntry *snap = (ManifestEntry *)heap_caps_malloc(sizeof(ManifestEntry) * MAX_SNAP,
                                                          MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!snap) snap = (ManifestEntry *)heap_caps_malloc(sizeof(ManifestEntry) * MAX_SNAP, MALLOC_CAP_8BIT);
  size_t snap_count = 0;
  if (snap) {
    for (size_t i = 0; i < MAX_SNAP; i++) new (&snap[i]) ManifestEntry();
    if (!manifest_get_all(snap, MAX_SNAP, &snap_count)) snap_count = 0;
  }
  size_t total = manifest_entry_count();
  if (total > 0xFFFF) total = 0xFFFF;
  if (total_out) *total_out = (uint16_t)total;
  out[0] = (uint8_t)(start & 0xFF);
  out[1] = (uint8_t)((start >> 8) & 0xFF);
  out[2] = (uint8_t)(total & 0xFF);
  out[3] = (uint8_t)((total >> 8) & 0xFF);
  size_t used = 5;
  uint8_t count = 0;
  // Packs a single entry if it fits; sets *fitted=false when it does not.
  // Manifest sizes are scan-time; the open recording's size is read live.
  for (size_t i = start; snap && i < snap_count; i++) {
    const String &name = snap[i].path;
    size_t namelen = name.length();
    if (namelen == 0 || namelen > 200) continue;
    size_t need = 1 + namelen + 4 + 1;
    if (used + need > out_size) break;
    uint8_t flags = 0;
    if (snap[i].pending) flags |= CTRL_LIST_FLAG_PENDING;
    if (snap[i].crc != 0) flags |= CTRL_LIST_FLAG_CRC;
    out[used++] = (uint8_t)namelen;
    memcpy(out + used, name.c_str(), namelen);
    used += namelen;
    uint32_t sz = snap[i].size;
    out[used++] = (uint8_t)(sz & 0xFF);
    out[used++] = (uint8_t)((sz >> 8) & 0xFF);
    out[used++] = (uint8_t)((sz >> 16) & 0xFF);
    out[used++] = (uint8_t)((sz >> 24) & 0xFF);
    out[used++] = flags;
    count++;
  }
  // The open recording has no manifest entry yet: append it live so the
  // host sees what is currently being written.
  if (recorder_is_recording()) {
    String cur = recorder_current_file();
    bool dup = false;
    if (snap) {
      for (size_t i = start; i < snap_count; i++) {
        if (snap[i].path == cur) { dup = true; break; }
      }
    }
    if (!dup && cur.length() > 0 && cur.length() <= 200) {
      uint32_t sz = 0;
      if (sd_file_size(cur, &sz)) {
        size_t need = 1 + cur.length() + 4 + 1;
        if (used + need <= out_size) {
          out[used++] = (uint8_t)cur.length();
          memcpy(out + used, cur.c_str(), cur.length());
          used += cur.length();
          out[used++] = (uint8_t)(sz & 0xFF);
          out[used++] = (uint8_t)((sz >> 8) & 0xFF);
          out[used++] = (uint8_t)((sz >> 16) & 0xFF);
          out[used++] = (uint8_t)((sz >> 24) & 0xFF);
          out[used++] = (uint8_t)(CTRL_LIST_FLAG_PENDING | CTRL_LIST_FLAG_ACTIVE);
          count++;
        }
      }
    }
  }
  if (snap) {
    for (size_t i = 0; i < MAX_SNAP; i++) snap[i].~ManifestEntry();
    heap_caps_free(snap);
  }
  out[4] = count;
  return used;
}

static bool storage_path_valid(const String &path) {
  if (!path.startsWith(String(REC_DIR) + "/")) return false; // jail to /rec
  if (path == String(REC_MANIFEST) || path == String(REC_MANIFEST) + ".tmp") return false;
  return path.endsWith(REC_OPUS_EXT) || path.endsWith(REC_WAV_EXT) ||
         path.endsWith(".opus") || path.endsWith(REC_TMP_EXT);
}

uint8_t control_do_file_delete(const uint8_t *name, uint16_t name_len) {
  if (!name || name_len == 0 || name_len > 128) return CTRL_ERR_BAD_ARG;
  for (uint16_t i = 0; i < name_len; i++) {
    if (name[i] == 0) return CTRL_ERR_BAD_ARG;
  }
  String path;
  path.concat((const char *)name, name_len);
  if (!sd_mounted()) return CTRL_ERR_NO_SD;
  if (!storage_path_valid(path)) return CTRL_ERR_BAD_ARG;
  if (transfer_is_transferring(path)) return CTRL_ERR_BUSY;
  if (recorder_is_recording() && path == recorder_current_file()) return CTRL_ERR_BUSY;
  if (!sd_file_exists(path)) return CTRL_ERR_NOT_FOUND;
  if (!sd_delete_file(path)) return CTRL_ERR_NOT_READY;
  (void)manifest_mark_done(path); // best-effort; next rescan reconciles
  return CTRL_OK;
}

uint8_t control_do_file_fetch(const uint8_t *name, uint16_t name_len) {
  if (!name || name_len == 0 || name_len > 128) return CTRL_ERR_BAD_ARG;
  for (uint16_t i = 0; i < name_len; i++) {
    if (name[i] == 0) return CTRL_ERR_BAD_ARG;
  }
  String path;
  path.concat((const char *)name, name_len);
  if (!sd_mounted()) return CTRL_ERR_NO_SD;
  if (!storage_path_valid(path)) return CTRL_ERR_BAD_ARG;
  if (recorder_is_recording() && path == recorder_current_file()) return CTRL_ERR_BUSY;
  if (transfer_is_transferring(path)) return CTRL_ERR_BUSY;
  if (!sd_file_exists(path)) return CTRL_ERR_NOT_FOUND;
  transfer_request_fetch((const char *)name, name_len);
  return CTRL_OK;
}

uint8_t control_do_erase(uint8_t step, uint16_t *removed_out) {
  if (removed_out) *removed_out = 0;
  if (step != CTRL_ERASE_ARM && step != CTRL_ERASE_CONFIRM) return CTRL_ERR_BAD_ARG;
  if (!sd_mounted()) return CTRL_ERR_NO_SD;
  if (step == CTRL_ERASE_ARM) {
    s_erase_armed_ms = millis();
    return CTRL_OK;
  }
  if (s_erase_armed_ms == 0 || (millis() - s_erase_armed_ms) > CTRL_ERASE_ARM_VALID_MS) {
    s_erase_armed_ms = 0;
    return CTRL_ERR_BAD_ARG; // not armed or expired: arm first
  }
  // Transient states keep the arm so the host can retry confirm.
  if (recorder_is_recording() || transfer_is_busy()) return CTRL_ERR_BUSY;
  s_erase_armed_ms = 0;
  size_t removed = 0;
  if (!sd_erase_recordings(&removed)) return CTRL_ERR_NOT_READY;
  manifest_scan_and_recover(); // rebuild RAM list + rewrite manifest.json
  if (removed_out) *removed_out = (removed > 0xFFFF) ? 0xFFFF : (uint16_t)removed;
  return CTRL_OK;
}

uint8_t control_do_sync_set(uint8_t enabled_arg) {
  if (enabled_arg > 1) {
    return CTRL_ERR_BAD_ARG;
  }
  bool enabled = enabled_arg != 0;
  if (enabled != s_sync_enabled) {
    s_sync_enabled = enabled;
    control_save_sync(enabled);
  }
  return CTRL_OK;
}

} // namespace

bool control_sync_enabled() {
  return s_sync_enabled;
}

void control_init() {
  s_rec_req = 0;
  s_rec_seq = 0;
  s_led_req = false;
  s_led_get_seq_valid = 0;
  s_status_req = false;
  s_status_seq = 0;
  s_denied_pending = false;
  s_storage_req = false;
  s_list_req = false;
  s_del_req = false;
  s_del_len = 0;
  s_fetch_req = false;
  s_fetch_len = 0;
  s_erase_req = false;
  s_erase_armed_ms = 0;
  s_sync_req = false;
  s_sync_get_seq_valid = 0;
  s_time_req = false;
  s_time_seq = 0;
  s_sync_enabled = control_load_sync_flag();
  control_load_led();
}

bool control_on_packet(const Packet *pkt) {
  if (pkt == nullptr) {
    return false;
  }
  if (pkt->type == PKT_STATUS_REQ) {
    if (!control_gate_ok()) {
      portENTER_CRITICAL(&s_ctrl_mux);
      s_denied_pending = true;
      portEXIT_CRITICAL(&s_ctrl_mux);
      return true;
    }
    portENTER_CRITICAL(&s_ctrl_mux);
    s_status_seq = pkt->seq;
    s_status_req = true;
    portEXIT_CRITICAL(&s_ctrl_mux);
    return true;
  }
  if (pkt->type == PKT_STORAGE_REQ) {
    if (!control_gate_ok()) {
      portENTER_CRITICAL(&s_ctrl_mux);
      s_denied_pending = true;
      portEXIT_CRITICAL(&s_ctrl_mux);
      return true;
    }
    portENTER_CRITICAL(&s_ctrl_mux);
    s_storage_seq = pkt->seq;
    s_storage_req = true;
    portEXIT_CRITICAL(&s_ctrl_mux);
    return true;
  }
  if (pkt->type == PKT_LIST_REQ) {
    if (!control_gate_ok()) {
      portENTER_CRITICAL(&s_ctrl_mux);
      s_denied_pending = true;
      portEXIT_CRITICAL(&s_ctrl_mux);
      return true;
    }
    if (pkt->len < 2) return true;
    portENTER_CRITICAL(&s_ctrl_mux);
    s_list_start = (uint16_t)(pkt->payload[0] | (pkt->payload[1] << 8));
    s_list_seq = pkt->seq;
    s_list_req = true;
    portEXIT_CRITICAL(&s_ctrl_mux);
    return true;
  }
  if (pkt->type != PKT_CMD) {
    return false;
  }
  if (!control_gate_ok()) {
    portENTER_CRITICAL(&s_ctrl_mux);
    s_denied_pending = true;
    portEXIT_CRITICAL(&s_ctrl_mux);
    return true;
  }
  if (pkt->len < 1) {
    return true;
  }
  uint8_t cmd = pkt->payload[0];
  // NOTE: the req flag is written last inside the critical section — poll()
  // treats a set flag as "args valid".
  portENTER_CRITICAL(&s_ctrl_mux);
  if (cmd == CTRL_CMD_REC_START || cmd == CTRL_CMD_REC_STOP) {
    s_rec_seq = pkt->seq;
    s_rec_req = cmd;
  } else if (cmd == CTRL_CMD_LED_SET) {
    if (pkt->len >= 3) {
      s_led_muted_arg = pkt->payload[1];
      s_led_bright_arg = pkt->payload[2];
      s_led_seq = pkt->seq;
      s_led_req = true;
    }
  } else if (cmd == CTRL_CMD_LED_GET) {
    s_led_get_seq = pkt->seq;
    s_led_get_seq_valid = 1;
  } else if (cmd == CTRL_CMD_SYNC_SET) {
    if (pkt->len >= 2) {
      s_sync_enabled_arg = pkt->payload[1];
      s_sync_seq = pkt->seq;
      s_sync_req = true;
    }
  } else if (cmd == CTRL_CMD_SYNC_GET) {
    s_sync_get_seq = pkt->seq;
    s_sync_get_seq_valid = 1;
  } else if (cmd == CTRL_CMD_TIME_SET) {
    if (pkt->len >= 9) {
      uint64_t unix_s = 0;
      for (int i = 0; i < 8; i++) unix_s |= (uint64_t)pkt->payload[1 + i] << (i * 8);
      s_time_unix_s = unix_s;
      s_time_seq = pkt->seq;
      s_time_req = true;
    }
  } else if (cmd == CTRL_CMD_FILE_DELETE) {
    uint16_t n = (pkt->len > 1) ? (uint16_t)(pkt->len - 1) : 0;
    if (n == 0 || n > sizeof(s_del_path)) {
      s_del_len = 0xFFFF; // sentinel: invalid length -> BAD_ARG in poll
    } else {
      for (uint16_t i = 0; i < n; i++) s_del_path[i] = pkt->payload[1 + i];
      s_del_len = n;
    }
    s_del_seq = pkt->seq;
    s_del_req = true;
  } else if (cmd == CTRL_CMD_STORAGE_ERASE) {
    s_erase_step = (pkt->len >= 2) ? pkt->payload[1] : 0xFF;
    s_erase_seq = pkt->seq;
    s_erase_req = true;
  } else if (cmd == CTRL_CMD_FILE_FETCH) {
    uint16_t n = (pkt->len > 1) ? (uint16_t)(pkt->len - 1) : 0;
    if (n == 0 || n > sizeof(s_fetch_path)) {
      s_fetch_len = 0xFFFF; // sentinel: invalid length -> BAD_ARG in poll
    } else {
      for (uint16_t i = 0; i < n; i++) s_fetch_path[i] = pkt->payload[1 + i];
      s_fetch_len = n;
    }
    s_fetch_seq = pkt->seq;
    s_fetch_req = true;
  } else if (cmd == CTRL_CMD_GET_CLOUD_SECRET) {
    s_cloud_seq = pkt->seq;
    s_cloud_req = true;
  } else if (cmd == CTRL_CMD_CLEAR_TRUSTED_SLOTS) {
    s_clear_slots_seq = pkt->seq;
    s_clear_slots_req = true;
  } else if (cmd == CTRL_CMD_FORGET_SELF) {
    s_forget_self_seq = pkt->seq;
    s_forget_self_req = true;
  } else {
    // Unknown cmd IDs are acked as BAD_ARG in poll.
    s_rec_seq = pkt->seq;
    s_rec_req = 0xFF; // sentinel: unknown cmd
  }
  portEXIT_CRITICAL(&s_ctrl_mux);
  return true;
}

void control_send_cloud_secret(uint16_t seq) {
  uint8_t key[CRYPTO_KEY_BYTES];
  uint8_t secret[AUTH_CLOUD_SECRET_BYTES];
  uint8_t nonce[CRYPTO_NONCE_BYTES];
  uint8_t payload[2 + CRYPTO_NONCE_BYTES + AUTH_CLOUD_SECRET_BYTES + CRYPTO_TAG_BYTES];
  if (!auth_get_session_key(key) || !auth_get_cloud_secret(secret)) {
    control_send_cmd_resp(seq, CTRL_CMD_GET_CLOUD_SECRET, CTRL_ERR_DENIED, nullptr, 0);
    return;
  }
  crypto_build_cloud_nonce(ble_session_id(), seq, nonce);
  uint8_t aad[4] = {(uint8_t)PROTO_VER, (uint8_t)CTRL_CMD_GET_CLOUD_SECRET, (uint8_t)(seq & 0xFF), (uint8_t)((seq >> 8) & 0xFF)};
  payload[0] = CTRL_CMD_GET_CLOUD_SECRET;
  payload[1] = CTRL_OK;
  memcpy(payload + 2, nonce, CRYPTO_NONCE_BYTES);
  bool ok = crypto_aead_encrypt(key, nonce, secret, sizeof(secret), aad, sizeof(aad),
                                payload + 2 + CRYPTO_NONCE_BYTES,
                                payload + 2 + CRYPTO_NONCE_BYTES + AUTH_CLOUD_SECRET_BYTES);
  if (ok) {
    ble_send_packet(PKT_CMD_RESP, seq, payload, (uint16_t)sizeof(payload));
  } else {
    control_send_cmd_resp(seq, CTRL_CMD_GET_CLOUD_SECRET, CTRL_ERR_NOT_READY, nullptr, 0);
  }
  memset(key, 0, sizeof(key));
  memset(secret, 0, sizeof(secret));
  memset(nonce, 0, sizeof(nonce));
  memset(payload, 0, sizeof(payload));
}

void control_poll() {
  if (s_forget_self_pending) {
    s_forget_self_pending = false;
    auth_forget_self();
  }
  if (s_clear_slots_pending) {
    s_clear_slots_pending = false;
    auth_clear_slots();
  }
  bool denied = false;
  bool status_req = false;
  uint16_t status_seq = 0;
  uint8_t rec_cmd = 0;
  uint16_t rec_seq = 0;
  bool led_req = false;
  uint8_t led_muted_arg = 0;
  uint8_t led_bright_arg = HW_RGB_BRIGHTNESS;
  uint16_t led_seq = 0;
  bool led_get = false;
  uint16_t led_get_seq = 0;
  bool storage_req = false;
  uint16_t storage_seq = 0;
  bool list_req = false;
  uint16_t list_start = 0;
  uint16_t list_seq = 0;
  bool del_req = false;
  uint16_t del_len = 0;
  uint8_t del_path[PROTO_MAX_PAYLOAD];
  uint16_t del_seq = 0;
  bool fetch_req = false;
  uint16_t fetch_len = 0;
  uint8_t fetch_path[PROTO_MAX_PAYLOAD];
  uint16_t fetch_seq = 0;
  bool erase_req = false;
  uint8_t erase_step = 0;
  uint16_t erase_seq = 0;
  bool sync_req = false;
  uint8_t sync_enabled_arg = 1;
  uint16_t sync_seq = 0;
  bool sync_get = false;
  uint16_t sync_get_seq = 0;
  bool time_req = false;
  uint64_t time_unix_s = 0;
  uint16_t time_seq = 0;
  bool cloud_req = false;
  uint16_t cloud_seq = 0;
  bool clear_slots_req = false;
  uint16_t clear_slots_seq = 0;
  bool forget_self_req = false;
  uint16_t forget_self_seq = 0;

  portENTER_CRITICAL(&s_ctrl_mux);
  denied = s_denied_pending;
  s_denied_pending = false;
  status_req = s_status_req;
  status_seq = s_status_seq;
  s_status_req = false;
  rec_cmd = s_rec_req;
  rec_seq = s_rec_seq;
  s_rec_req = 0;
  led_req = s_led_req;
  led_muted_arg = s_led_muted_arg;
  led_bright_arg = s_led_bright_arg;
  led_seq = s_led_seq;
  s_led_req = false;
  led_get = s_led_get_seq_valid != 0;
  led_get_seq = s_led_get_seq;
  s_led_get_seq_valid = 0;
  storage_req = s_storage_req;
  storage_seq = s_storage_seq;
  s_storage_req = false;
  list_req = s_list_req;
  list_start = s_list_start;
  list_seq = s_list_seq;
  s_list_req = false;
  del_req = s_del_req;
  del_len = s_del_len;
  if (del_len != 0xFFFF && del_len > 0 && del_len <= sizeof(del_path)) {
    for (uint16_t i = 0; i < del_len; i++) del_path[i] = s_del_path[i];
  }
  del_seq = s_del_seq;
  s_del_req = false;
  fetch_req = s_fetch_req;
  fetch_len = s_fetch_len;
  if (fetch_len != 0xFFFF && fetch_len > 0 && fetch_len <= sizeof(fetch_path)) {
    for (uint16_t i = 0; i < fetch_len; i++) fetch_path[i] = s_fetch_path[i];
  }
  fetch_seq = s_fetch_seq;
  s_fetch_req = false;
  erase_req = s_erase_req;
  erase_step = s_erase_step;
  erase_seq = s_erase_seq;
  s_erase_req = false;
  sync_req = s_sync_req;
  sync_enabled_arg = s_sync_enabled_arg;
  sync_seq = s_sync_seq;
  s_sync_req = false;
  sync_get = s_sync_get_seq_valid != 0;
  sync_get_seq = s_sync_get_seq;
  s_sync_get_seq_valid = 0;
  time_req = s_time_req;
  time_unix_s = s_time_unix_s;
  time_seq = s_time_seq;
  s_time_req = false;
  cloud_req = s_cloud_req;
  cloud_seq = s_cloud_seq;
  s_cloud_req = false;
  clear_slots_req = s_clear_slots_req;
  clear_slots_seq = s_clear_slots_seq;
  s_clear_slots_req = false;
  forget_self_req = s_forget_self_req;
  forget_self_seq = s_forget_self_seq;
  s_forget_self_req = false;
  portEXIT_CRITICAL(&s_ctrl_mux);

  if (denied) {
    if (control_gate_ok() || ble_is_connected()) {
      control_send_denied();
    }
  }

  if (status_req) {
    if (control_gate_ok()) {
      uint8_t payload[CTRL_STATUS_LEN];
      control_build_status(payload);
      ble_send_packet(PKT_STATUS_RESP, status_seq, payload, CTRL_STATUS_LEN);
      memcpy(s_last_status, payload, CTRL_STATUS_LEN);
      s_last_push_ms = millis();
      s_has_last_status = true;
    }
  }

  if (rec_cmd != 0) {
    if (control_gate_ok()) {
      uint8_t status = CTRL_ERR_BAD_ARG;
      if (rec_cmd == CTRL_CMD_REC_START) {
        status = control_do_rec_start();
      } else if (rec_cmd == CTRL_CMD_REC_STOP) {
        status = control_do_rec_stop();
      }
      control_send_cmd_resp(rec_seq, rec_cmd, status, nullptr, 0);
    }
  }

  if (led_req) {
    if (control_gate_ok()) {
      uint8_t status = control_do_led_set(led_muted_arg, led_bright_arg);
      control_send_cmd_resp(led_seq, CTRL_CMD_LED_SET, status, nullptr, 0);
    }
  }

  if (led_get) {
    if (control_gate_ok()) {
      uint8_t extra[2] = {ui_is_muted() ? (uint8_t)1 : (uint8_t)0, ui_get_brightness()};
      control_send_cmd_resp(led_get_seq, CTRL_CMD_LED_GET, CTRL_OK, extra, 2);
    }
  }

  if (storage_req) {
    if (control_gate_ok()) {
      uint8_t payload[CTRL_STORAGE_LEN];
      control_build_storage(payload);
      ble_send_packet(PKT_STORAGE_RESP, storage_seq, payload, CTRL_STORAGE_LEN);
    }
  }

  if (list_req) {
    if (control_gate_ok()) {
      uint8_t payload[PROTO_MAX_PAYLOAD];
      uint16_t total = 0;
      size_t used = control_pack_list(list_start, payload, sizeof(payload), &total);
      (void)total;
      ble_send_packet(PKT_LIST_RESP, list_seq, payload, (uint16_t)used);
    }
  }

  if (del_req) {
    if (control_gate_ok()) {
      uint8_t status = CTRL_ERR_BAD_ARG;
      if (del_len != 0xFFFF && del_len > 0) {
        status = control_do_file_delete(del_path, del_len);
      }
      control_send_cmd_resp(del_seq, CTRL_CMD_FILE_DELETE, status, nullptr, 0);
    }
  }

  if (fetch_req) {
    if (control_gate_ok()) {
      uint8_t status = CTRL_ERR_BAD_ARG;
      if (fetch_len != 0xFFFF && fetch_len > 0 && fetch_len <= 128) {
        status = control_do_file_fetch(fetch_path, fetch_len);
      }
      control_send_cmd_resp(fetch_seq, CTRL_CMD_FILE_FETCH, status, nullptr, 0);
    }
  }

  if (erase_req) {
    if (control_gate_ok()) {
      uint16_t removed = 0;
      uint8_t status = control_do_erase(erase_step, &removed);
      uint8_t extra[2] = {(uint8_t)(removed & 0xFF), (uint8_t)((removed >> 8) & 0xFF)};
      control_send_cmd_resp(erase_seq, CTRL_CMD_STORAGE_ERASE, status, extra, 2);
    }
  }

  if (sync_req) {
    if (control_gate_ok()) {
      uint8_t status = control_do_sync_set(sync_enabled_arg);
      control_send_cmd_resp(sync_seq, CTRL_CMD_SYNC_SET, status, nullptr, 0);
    }
  }

  if (sync_get) {
    if (control_gate_ok()) {
      uint8_t extra[1] = {s_sync_enabled ? (uint8_t)1 : (uint8_t)0};
      control_send_cmd_resp(sync_get_seq, CTRL_CMD_SYNC_GET, CTRL_OK, extra, 1);
    }
  }

  if (time_req) {
    if (control_gate_ok()) {
      clock_set_anchor(time_unix_s, clock_ticks_us());
      uint32_t echo = (uint32_t)time_unix_s;
      uint8_t extra[4] = {(uint8_t)(echo & 0xFF), (uint8_t)((echo >> 8) & 0xFF),
                          (uint8_t)((echo >> 16) & 0xFF), (uint8_t)((echo >> 24) & 0xFF)};
      control_send_cmd_resp(time_seq, CTRL_CMD_TIME_SET, CTRL_OK, extra, sizeof(extra));
    }
  }

  if (cloud_req) {
    if (control_gate_ok()) {
      control_send_cloud_secret(cloud_seq);
    }
  }

  if (clear_slots_req) {
    if (control_gate_ok()) {
      control_send_cmd_resp(clear_slots_seq, CTRL_CMD_CLEAR_TRUSTED_SLOTS, CTRL_OK, nullptr, 0);
      s_clear_slots_pending = true;
    }
  }

  if (forget_self_req) {
    if (control_gate_ok()) {
      control_send_cmd_resp(forget_self_seq, CTRL_CMD_FORGET_SELF, CTRL_OK, nullptr, 0);
      s_forget_self_pending = true;
    }
  }

  if (!status_req) {
    control_push_status_if_changed();
  }
}
