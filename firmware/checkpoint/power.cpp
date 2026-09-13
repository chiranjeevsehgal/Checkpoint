#include "power.h"
#include "config.h"
#include "ble_service.h"
#include "control.h"
#include "manifest.h"
#include "recorder.h"
#include "sd_manager.h"
#include "transfer.h"
#include "ui.h"
#include "log.h"
#include "esp_sleep.h"
#include "driver/rtc_io.h"

namespace {

bool s_idle_tracking = false;
uint32_t s_idle_since_ms = 0;
uint8_t s_synced_confirm = 0;

// Instantaneous gates. The idle clock keeps running while these are busy, so
// the device sleeps as soon as the mic is off long enough and everything else
// is clear.
bool power_clear_to_sleep() {
  if (transfer_is_busy()) return false;
  if (ble_is_connected()) return false;
  if (ble_enrollment_active()) return false;
  // Pending uploads block sleep only when auto-sync can actually clear them;
  // with sync off, waiting would keep the device awake forever.
  if (control_sync_enabled()) {
    if (manifest_pending_count() != 0) {
      s_synced_confirm = 0;
      return false;
    }
    // manifest_pending_count() returns 0 on a 200ms mutex timeout, so require
    // two consecutive clean reads before treating the manifest as synced.
    if (s_synced_confirm < 2) {
      s_synced_confirm++;
      return false;
    }
  }
  return true;
}

} // namespace

void power_init() {
#if POWER_AUTO_SLEEP_ENABLE
  esp_sleep_wakeup_cause_t cause = esp_sleep_get_wakeup_cause();
  if (cause != ESP_SLEEP_WAKEUP_GPIO && cause != ESP_SLEEP_WAKEUP_EXT1) {
    return; // cold boot / SW reset / panic: no wake-hold gate
  }
  Serial.printf("PWR wake cause=%d\n", (int)cause);
  rtc_gpio_deinit((gpio_num_t)HW_BUTTON_GPIO);
  pinMode(HW_BUTTON_GPIO, INPUT_PULLUP);
  uint32_t t0 = millis();
  while (digitalRead(HW_BUTTON_GPIO) == LOW && (millis() - t0) < POWER_WAKE_HOLD_MS) {
    vTaskDelay(pdMS_TO_TICKS(20));
  }
  if ((millis() - t0) < POWER_WAKE_HOLD_MS) {
    LOG_W("PWR short wake");
    power_sleep_now(); // released before the hold: not a real wake
  }
  ui_flash_blue(POWER_WAKE_LED_FLASHES);
  // Wait for release so ui_task does not misread the lingering press as a
  // mic toggle. Bounded so a stuck button cannot block boot forever.
  uint32_t rel = millis();
  while (digitalRead(HW_BUTTON_GPIO) == LOW && (millis() - rel) < POWER_WAKE_RELEASE_CAP_MS) {
    vTaskDelay(pdMS_TO_TICKS(20));
  }
#endif
}

void power_poll() {
#if POWER_AUTO_SLEEP_ENABLE
  if (recorder_is_recording()) {
    s_idle_tracking = false;
    s_idle_since_ms = 0;
    s_synced_confirm = 0;
    return;
  }
  uint32_t now = millis();
  if (!s_idle_tracking) {
    s_idle_tracking = true;
    s_idle_since_ms = now;
    return;
  }
  if ((now - s_idle_since_ms) < POWER_IDLE_SLEEP_MS) {
    return;
  }
  if (!power_clear_to_sleep()) {
    return;
  }
  power_sleep_now();
#endif
}

void power_sleep_now() {
  LOG_W("PWR sleep");
  ui_signal_sleeping();
  vTaskDelay(pdMS_TO_TICKS((uint32_t)POWER_SLEEP_LED_FLASHES * POWER_LED_FLASH_PERIOD_MS + 80));
  if (recorder_is_recording()) {
    recorder_stop();
  }
  vTaskDelay(pdMS_TO_TICKS(500));
  ble_disconnect();
  vTaskDelay(pdMS_TO_TICKS(200));
  if (sd_mounted()) {
    sd_end();
  }
  Serial.flush();
  // ESP32-S3 has no deep-sleep GPIO wake API, so wake via EXT1 on GPIO1 (an
  // RTC-capable pin). Keep the internal RTC pull-up so the pin cannot float
  // once the digital GPIO domain is powered down.
  rtc_gpio_pullup_en((gpio_num_t)HW_BUTTON_GPIO);
  rtc_gpio_pulldown_dis((gpio_num_t)HW_BUTTON_GPIO);
  esp_err_t err =
      esp_sleep_enable_ext1_wakeup_io(1ULL << HW_BUTTON_GPIO, ESP_EXT1_WAKEUP_ANY_LOW);
  if (err != ESP_OK) {
    LOG_E("PWR wake cfg %d", (int)err);
  }
  esp_deep_sleep_start();
}
