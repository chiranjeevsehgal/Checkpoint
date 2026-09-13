#include <Arduino.h>
#include "esp_system.h"
#include "config.h"
#include "sd_manager.h"
#include "manifest.h"
#include "recorder.h"
#include "ble_service.h"
#include "transfer.h"
#include "ui.h"
#include "crypto.h"
#include "auth.h"
#include "log.h"
#include "control.h"
#include "power.h"

static TaskHandle_t uiTaskHandle = nullptr;
static TaskHandle_t transferTaskHandle = nullptr;

void setup() {
  Serial.begin(115200);
  delay(500);
  esp_reset_reason_t rr = esp_reset_reason();
  // Decode common reasons for quick triage (POWERON vs SW vs WDT vs PANIC)
  const char *rr_str = "UNKNOWN";
  if (rr == ESP_RST_POWERON) rr_str = "POWERON";
  else if (rr == ESP_RST_SW) rr_str = "SW";
  else if (rr == ESP_RST_PANIC) rr_str = "PANIC";
  else if (rr == ESP_RST_INT_WDT) rr_str = "INT_WDT";
  else if (rr == ESP_RST_TASK_WDT) rr_str = "TASK_WDT";
  else if (rr == ESP_RST_WDT) rr_str = "WDT";
  else if (rr == ESP_RST_BROWNOUT) rr_str = "BROWNOUT";
  else if (rr == ESP_RST_SDIO) rr_str = "SDIO";
  else if (rr == ESP_RST_DEEPSLEEP) rr_str = "DEEPSLEEP";

  ui_init();
  power_init();
  control_init();
  xTaskCreatePinnedToCore(ui_task, "ui", TASK_STACK_UI, nullptr, TASK_PRIO_UI, &uiTaskHandle, 0);

  if (!sd_begin()) {
    LOG_E("SD init fail rst:%s", rr_str);
    ui_signal_error();
  }

  manifest_init();
  manifest_scan_and_recover();

  crypto_init();

  auth_init();

  if (!recorder_init()) {
    LOG_E("I2S init fail");
    ui_signal_fatal();
    return;
  }

  bool b = ble_init();
  if (!b) {
    LOG_E("BLE init fail");
  }
  bool t = transfer_init();
  if (!t) {
    LOG_E("XFER init fail");
  }

  xTaskCreatePinnedToCore(transfer_task, "transfer", TASK_STACK_TRANSFER, nullptr, TASK_PRIO_TRANSFER, &transferTaskHandle, 0);

  recorder_start();
#if VAD_ENABLE
  ui_signal_vad_listening();
#else
  ui_signal_recording(true);
#endif
  {
    unsigned pend = manifest_pending_count();
    Serial.printf("CK boot rst:%s pend:%u\n", rr_str, pend);
  }
}

void loop() {
  static uint32_t last_sd_check = 0;

  if (millis() - last_sd_check > 2000) {
    last_sd_check = millis();
#if HW_HAS_SD_DETECT
    if (!sd_present() && sd_mounted()) {
      sd_end();
      ui_signal_error();
    } else if (sd_present() && !sd_mounted()) {
      if (sd_begin()) {
        manifest_scan_and_recover();
#if VAD_ENABLE
        if (recorder_vad_active()) ui_signal_recording(true);
        else if (recorder_is_recording()) ui_signal_vad_listening();
        else ui_signal_recording(false);
#else
        ui_signal_recording(recorder_is_recording());
#endif
      }
    }
#else
    // 8-pin variant: no DET — main loop sole owner of mount
    // Probe FS health when mounted, but debounce to avoid spurious unmount during single glitch
    if (!sd_mounted()) {
      if (sd_begin()) {
        manifest_scan_and_recover();
#if VAD_ENABLE
        if (recorder_vad_active()) ui_signal_recording(true);
        else if (recorder_is_recording()) ui_signal_vad_listening();
        else ui_signal_recording(false);
#else
        ui_signal_recording(recorder_is_recording());
#endif
      } else {
        ui_signal_error();
      }
    } else {
      static uint8_t fs_fail_cnt = 0;
      if (recorder_is_recording()) {
        // Skip periodic SD.exists probe while actively recording to eliminate SPI contention and avoid flash read/write collisions
        fs_fail_cnt = 0;
      } else {
        bool healthy = false;
        if (sd_lock(200)) {
          bool rec_ok = SD.exists(REC_DIR);
          int ct = SD.cardType();
          healthy = rec_ok && ct != CARD_NONE;
          if (!healthy) {
            // Fallback check root
            healthy = SD.exists("/") && ct != CARD_NONE;
          }
          sd_unlock();
        }
        if (!healthy) {
          fs_fail_cnt++;
          if (fs_fail_cnt >= 3) {
            LOG_E("SD FS lost");
            sd_end();
            ui_signal_error();
            fs_fail_cnt = 0;
          }
        } else {
          fs_fail_cnt = 0;
        }
      }
    }
#endif
  }

  // Recovery scan removed from periodic timer — active REC_x.wav.tmp must not be
  // treated as crash artifact while recorder holds s_file open.
  // Recovery now only on boot (setup) and SD remount (sd_begin success paths above).
  ble_check_handshake_timeout();
  ble_check_final_diag();
  control_poll();
  auth_usb_poll();
  power_poll();

  if (ble_is_connected() && ble_is_handshaked()) {
    // keepalive handled in ble_service (BLE_KEEPALIVE_MS reserved)
  }

  vTaskDelay(pdMS_TO_TICKS(200));
}
