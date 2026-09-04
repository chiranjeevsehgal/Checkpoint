#include "ui.h"
#include "config.h"
#include "recorder.h"
#include "manifest.h"

static TaskHandle_t s_task = nullptr;
static volatile uint8_t s_state = 0;

enum LedState : uint8_t { LED_OFF, LED_ON, LED_BOOKMARK, LED_ERROR, LED_FATAL };

void ui_init() {
  pinMode(HW_RECORD_LED_GPIO, OUTPUT);
  digitalWrite(HW_RECORD_LED_GPIO, LOW);
  pinMode(HW_BUTTON_GPIO, INPUT_PULLUP);
}

void ui_signal_recording(bool on) {
  s_state = on ? LED_ON : LED_OFF;
  Serial.printf("UI LED %s (recording %s)\n", on ? "ON" : "OFF", on ? "ON" : "MUTED");
}
void ui_signal_bookmark() {
  // Bookmark removed - mic toggle uses LED_ON/OFF only
  // Kept for test compat: recorder_notify_bookmark() stub remains but unused
  s_state = LED_BOOKMARK;
}
void ui_signal_error() {
  s_state = LED_ERROR;
  Serial.println("UI signal: ERROR");
}
void ui_signal_fatal() {
  s_state = LED_FATAL;
  Serial.println("UI signal: FATAL");
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
      // Toggle lockout 800ms to cover close_chunk settle (400+300) and avoid double-toggle
      if (now - last_toggle_ms < 800) {
        Serial.printf("MIC toggle ignored: lockout %lu ms (debounce)\n", (unsigned long)(now - last_toggle_ms));
      } else {
        last_toggle_ms = now;
        if (recorder_is_recording()) {
          Serial.println("MIC toggle: BUTTON pressed -> MUTING mic, stopping recorder");
          Serial.printf("MIC state: stopping, current_file=%s pending=%u\n", recorder_current_file().c_str(), (unsigned)manifest_pending_count());
          recorder_stop();
          ui_signal_recording(false);
          Serial.println("MIC state: OFF - LED OFF, recording stopped, BLE sync continues in background");
          Serial.printf("MIC muted at uptime %lu ms, was_recording now %d\n", (unsigned long)now, recorder_is_recording());
        } else {
          Serial.println("MIC toggle: BUTTON pressed -> UNMUTING mic, starting recorder");
          bool ok = recorder_start();
          if (ok) {
            ui_signal_recording(true);
            Serial.printf("MIC state: ON - LED ON, recording resumed at uptime %lu ms file=%s\n", (unsigned long)now, recorder_current_file().c_str());
          } else {
            Serial.println("MIC toggle: FAILED to start recorder - check SD/I2S");
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
    switch (s_state) {
      case LED_OFF: digitalWrite(HW_RECORD_LED_GPIO, LOW); break;
      case LED_ON: digitalWrite(HW_RECORD_LED_GPIO, HIGH); break;
      case LED_BOOKMARK: {
        if (now - state_enter_ms < UI_LED_BOOKMARK_MS) {
          digitalWrite(HW_RECORD_LED_GPIO, HIGH);
        } else {
          digitalWrite(HW_RECORD_LED_GPIO, LOW);
          s_state = LED_ON;
          prev_state = LED_BOOKMARK;
          state_enter_ms = now;
        }
        break;
      }
      case LED_ERROR: {
        // Non-blocking 3x 120ms blink + 600ms pause = 1320ms cycle, polls button every 20ms
        uint32_t t = (now - state_enter_ms) % 1320;
        if (t < 720) {
          uint32_t phase = t % 240;
          digitalWrite(HW_RECORD_LED_GPIO, phase < 120 ? HIGH : LOW);
        } else if (t < 1320) {
          digitalWrite(HW_RECORD_LED_GPIO, LOW);
        }
        break;
      }
      case LED_FATAL: {
        // Non-blocking 80ms on/off strobe
        uint32_t t = (now - state_enter_ms) % 160;
        digitalWrite(HW_RECORD_LED_GPIO, t < 80 ? HIGH : LOW);
        break;
      }
    }
    vTaskDelay(pdMS_TO_TICKS(20));
  }
}
