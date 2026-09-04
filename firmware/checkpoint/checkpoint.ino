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
#include "bench.h"

static TaskHandle_t uiTaskHandle = nullptr;
static TaskHandle_t transferTaskHandle = nullptr;

void setup() {
  Serial.begin(115200);
  delay(500);
  esp_reset_reason_t rr = esp_reset_reason();
  Serial.printf("\nCheckpoint Rev B rst:0x%x (%d) uptime %lu\n", rr, rr, (unsigned long)millis());
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
  Serial.printf("Reset reason: %s\n", rr_str);

  ui_init();
  xTaskCreatePinnedToCore(ui_task, "ui", TASK_STACK_UI, nullptr, TASK_PRIO_UI, &uiTaskHandle, 0);

  if (!sd_begin()) {
    Serial.println("SD mount failed");
    ui_signal_error();
  } else {
    Serial.println("SD mounted");
    // Card diagnostics (helps distinguish wiring vs FS vs path bugs)
    if (sd_lock(500)) {
      Serial.printf("SD type %d size %llu total %llu used %llu rec_dir_exists %d\n",
                    (int)SD.cardType(), (unsigned long long)SD.cardSize(),
                    (unsigned long long)SD.totalBytes(), (unsigned long long)SD.usedBytes(),
                    (int)SD.exists(REC_DIR));
      sd_unlock();
    }
  }

  manifest_init();
  manifest_scan_and_recover();

  // Post-manifest SD health (detects FS corruption from previous run)
  if (sd_lock(200)) {
    Serial.printf("SD post-manifest rec_dir %d root %d cardType %d\n", (int)SD.exists(REC_DIR), (int)SD.exists("/"), (int)SD.cardType());
    sd_unlock();
  }

  crypto_init();

  if (!recorder_init()) {
    Serial.println("I2S init failed");
    ui_signal_fatal();
    return;
  }
  Serial.println("I2S OK");
  if (sd_lock(200)) {
    Serial.printf("SD post-I2S rec_dir %d cardType %d\n", (int)SD.exists(REC_DIR), (int)SD.cardType());
    sd_unlock();
  }

  bool b = ble_init();
  Serial.printf("BLE init %s\n", b?"OK":"FAIL");
  if (sd_lock(200)) {
    Serial.printf("SD post-BLE rec_dir %d cardType %d\n", (int)SD.exists(REC_DIR), (int)SD.cardType());
    sd_unlock();
  }
  bool t = transfer_init();
  Serial.printf("Transfer init %s\n", t?"OK":"FAIL");
  if (sd_lock(200)) {
    Serial.printf("SD post-transfer rec_dir %d cardType %d\n", (int)SD.exists(REC_DIR), (int)SD.cardType());
    sd_unlock();
  }

  xTaskCreatePinnedToCore(transfer_task, "transfer", TASK_STACK_TRANSFER, nullptr, TASK_PRIO_TRANSFER, &transferTaskHandle, 0);
  Serial.println("Transfer task started");

  recorder_start();
  ui_signal_recording(true);
  Serial.println("Recorder started, 1-min chunks");
  Serial.printf("Checkpoint ready — BLE %s\n", BLE_DEVICE_NAME);
}

void loop() {
  static uint32_t last_sd_check = 0;
  static uint32_t last_bench = 0;
  static bool bench_header_done = false;

  if (millis() - last_sd_check > 2000) {
    last_sd_check = millis();
#if HW_HAS_SD_DETECT
    if (!sd_present() && sd_mounted()) {
      sd_end();
      ui_signal_error();
    } else if (sd_present() && !sd_mounted()) {
      if (sd_begin()) {
        manifest_scan_and_recover();
        ui_signal_recording(recorder_is_recording());
      }
    }
#else
    // 8-pin variant: no DET — main loop sole owner of mount
    // Probe FS health when mounted, but debounce to avoid spurious unmount during single glitch
    if (!sd_mounted()) {
      if (sd_begin()) {
        manifest_scan_and_recover();
        ui_signal_recording(recorder_is_recording());
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
          // Log only on failure to avoid spam
          if (!healthy) {
            Serial.printf("SD health probe rec=%d ct=%d root=%d fail %d/3\n", (int)rec_ok, ct, (int)SD.exists("/"), fs_fail_cnt+1);
          }
          sd_unlock();
        }
        if (!healthy) {
          fs_fail_cnt++;
          if (fs_fail_cnt >= 3) {
            Serial.println("SD FS lost (3×), unmounting for remount");
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

  if (ble_is_connected() && ble_is_handshaked()) {
    // keepalive handled in ble_service (BLE_KEEPALIVE_MS reserved)
  }

  // BENCH: periodic dump of last file sample every 5s while transfer active or after file
  if (millis() - last_bench > 5000) {
    last_bench = millis();
    if (!bench_header_done) {
      bench_print_header();
      bench_header_done = true;
    }
    BenchSample s = transfer_bench_snapshot();
    if (s.total_frags != 0) {
      bench_print_sample("fw", s);
    }
  }

  vTaskDelay(pdMS_TO_TICKS(200));
}
