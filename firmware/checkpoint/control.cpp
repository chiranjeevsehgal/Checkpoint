#include "control.h"
#include "config.h"
#include "protocol.h"
#include "ble_service.h"
#include "recorder.h"
#include "manifest.h"
#include "ui.h"
#include <Preferences.h>

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

// Guards the req-flag handoff: NimBLE callback task writes, loop task
// (control_poll) reads+clears. Critical section, never held across
// recorder/NVS/BLE work — only flag copies.
static portMUX_TYPE s_ctrl_mux = portMUX_INITIALIZER_UNLOCKED;

constexpr uint16_t kNoSeq = 0xFFFF;
constexpr uint8_t kErrNotEncrypted = 0x01;

bool control_gate_ok() {
  return ble_is_connected() && ble_is_handshaked() && ble_is_encrypted();
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

} // namespace

void control_init() {
  s_rec_req = 0;
  s_rec_seq = 0;
  s_led_req = false;
  s_led_get_seq_valid = 0;
  s_status_req = false;
  s_status_seq = 0;
  s_denied_pending = false;
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
  } else {
    // Unknown cmd IDs are acked as BAD_ARG in poll.
    s_rec_seq = pkt->seq;
    s_rec_req = 0xFF; // sentinel: unknown cmd
  }
  portEXIT_CRITICAL(&s_ctrl_mux);
  return true;
}

void control_poll() {
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
  portEXIT_CRITICAL(&s_ctrl_mux);

  if (denied) {
    if (control_gate_ok() || ble_is_connected()) {
      control_send_denied();
    }
  }

  if (status_req) {
    if (ble_is_connected() && ble_is_handshaked()) {
      uint8_t payload[CTRL_STATUS_LEN];
      control_build_status(payload);
      ble_send_packet(PKT_STATUS_RESP, status_seq, payload, CTRL_STATUS_LEN);
    }
  }

  if (rec_cmd != 0) {
    if (ble_is_connected() && ble_is_handshaked()) {
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
    if (ble_is_connected() && ble_is_handshaked()) {
      uint8_t status = control_do_led_set(led_muted_arg, led_bright_arg);
      control_send_cmd_resp(led_seq, CTRL_CMD_LED_SET, status, nullptr, 0);
    }
  }

  if (led_get) {
    if (ble_is_connected() && ble_is_handshaked()) {
      uint8_t extra[2] = {ui_is_muted() ? (uint8_t)1 : (uint8_t)0, ui_get_brightness()};
      control_send_cmd_resp(led_get_seq, CTRL_CMD_LED_GET, CTRL_OK, extra, 2);
    }
  }
}
