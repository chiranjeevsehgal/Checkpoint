#include "ui.h"
#include "config.h"
#include "recorder.h"
#include "manifest.h"
#include "log.h"
#include <Adafruit_NeoPixel.h>

static TaskHandle_t s_task = nullptr;
static volatile uint8_t s_state = 0;
static volatile bool s_muted = false;
static volatile uint8_t s_brightness = HW_RGB_BRIGHTNESS;
static volatile bool s_brightness_dirty = false;
static volatile uint32_t s_remote_action_ms = 0;

enum LedState : uint8_t { LED_OFF, LED_ON, LED_BOOKMARK, LED_ERROR, LED_FATAL, LED_VAD_IDLE };

static Adafruit_NeoPixel s_rgb(1, HW_RGB_PIN, NEO_GRB + NEO_KHZ800);
static uint32_t s_last_rgb = 0xFFFFFFFFu; // force first show

static inline void ui_set_rgb(uint8_t r, uint8_t g, uint8_t b) {
  uint32_t c = s_rgb.Color(r, g, b);
  if (c != s_last_rgb) {
    s_last_rgb = c;
    s_rgb.setPixelColor(0, c);
    s_rgb.show();
  }
}

void ui_init() {
  s_rgb.begin();
  s_rgb.setBrightness(HW_RGB_BRIGHTNESS);
  s_rgb.clear();
  s_rgb.show();
  s_last_rgb = s_rgb.Color(0, 0, 0);
  pinMode(HW_BUTTON_GPIO, INPUT_PULLUP);
}

void ui_signal_recording(bool on) {
  s_state = on ? LED_ON : LED_OFF;
}
void ui_signal_vad_listening() {
  s_state = LED_VAD_IDLE;
}
void ui_signal_bookmark() {
  // Bookmark removed - no LED output (kept for compat, no state change)
}
void ui_signal_error() {
  s_state = LED_ERROR;
}
void ui_signal_fatal() {
  s_state = LED_FATAL;
}

void ui_set_muted(bool muted) {
  s_muted = muted;
}

bool ui_is_muted() {
  return s_muted;
}

void ui_set_brightness(uint8_t brightness) {
  if (brightness == s_brightness) {
    return;
  }
  s_brightness = brightness;
  s_brightness_dirty = true;
}

uint8_t ui_get_brightness() {
  return s_brightness;
}

void ui_note_remote_action() {
  s_remote_action_ms = millis();
}

static inline bool ui_stealth_active() {
  return s_muted;
}

void ui_task(void *arg) {
  (void)arg;
  uint32_t last_change = 0;
  bool last_level = HIGH;
  bool armed = true;
  uint32_t state_enter_ms = 0;
  static uint32_t last_toggle_ms = 0;
  while (true) {
    bool level = digitalRead(HW_BUTTON_GPIO);
    uint32_t now = millis();
    if (level != last_level) {
      last_level = level;
      last_change = now;
    }
    if (armed && level == LOW && (now - last_change) > UI_DEBOUNCE_MS) {
      armed = false;
      // Toggle lockout 800ms to cover close_chunk settle (400+300) and avoid double-toggle.
      // Also covers a recent remote (BLE) action so the button cannot instantly reverse it.
      if ((now - last_toggle_ms < 800) || (now - s_remote_action_ms < 800)) {
      } else {
        last_toggle_ms = now;
        if (recorder_is_recording()) {
          recorder_stop();
          ui_signal_recording(false);
        } else {
          bool ok = recorder_start();
          if (ok) {
            ui_signal_recording(true);
          } else {
            LOG_E("MIC start fail");
            ui_signal_error();
          }
        }
      }
      // recorder_notify_bookmark() retained for test compat (bookmark removed) - not called
      state_enter_ms = now;
    }
    if (level == HIGH && (now - last_change) > UI_DEBOUNCE_MS) {
      armed = true;
    }
    // Track state entry for non-blocking blinks
    static uint8_t prev_state = 255;
    if (s_state != prev_state) {
      prev_state = s_state;
      state_enter_ms = now;
    }
    if (s_brightness_dirty) {
      s_brightness_dirty = false;
      s_rgb.setBrightness(s_brightness);
      s_last_rgb = 0xFFFFFFFFu; // force re-show at new brightness
      if (s_state == LED_OFF || (s_muted && s_state != LED_ERROR && s_state != LED_FATAL)) {
        s_rgb.clear();
        s_rgb.show();
        s_last_rgb = s_rgb.Color(0, 0, 0);
      }
    }
    const bool stealth = ui_stealth_active();
    switch (s_state) {
      case LED_OFF: ui_set_rgb(0, 0, 0); break;
      case LED_ON:
        if (stealth) ui_set_rgb(0, 0, 0);
        else ui_set_rgb(0, 180, 0);
        break;
      case LED_BOOKMARK: {
        ui_set_rgb(0, 0, 0);
        break;
      }
      case LED_ERROR: {
        // Non-blocking 3x 120ms blink + 600ms pause = 1320ms cycle, polls button every 20ms
        uint32_t t = (now - state_enter_ms) % 1320;
        if (t < 720) {
          uint32_t phase = t % 240;
          bool on = (phase < 120);
          ui_set_rgb(on ? 255 : 0, on ? 140 : 0, 0);
        } else if (t < 1320) {
          ui_set_rgb(0, 0, 0);
        }
        break;
      }
      case LED_FATAL: {
        // Non-blocking 80ms on/off strobe
        uint32_t t = (now - state_enter_ms) % 160;
        bool on = (t < 80);
        ui_set_rgb(on ? 255 : 0, 0, 0);
        break;
      }
      case LED_VAD_IDLE: {
        // Slow 1Hz pulse, 10% duty: proves "mic ON, listening, silence"
        // vs LED_OFF (muted) and LED_ON solid (utterance capturing).
        // Suppressed when stealth-muted so listening stays dark.
        if (stealth) {
          ui_set_rgb(0, 0, 0);
          break;
        }
        uint32_t t = (now - state_enter_ms) % 1000;
        ui_set_rgb(0, 0, (t < 100) ? 90 : 0);
        break;
      }
    }
    vTaskDelay(pdMS_TO_TICKS(20));
  }
}
