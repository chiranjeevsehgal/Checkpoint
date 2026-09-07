#pragma once
#include <Arduino.h>

// Minimal error-only logging: short tags, integer args only.
// No String, no float, no %llu — keeps UART blocking <2ms per line.
// Snapshot values under mutex, unlock, then log. Never log while holding sd_lock.
#define LOG_E(fmt, ...) Serial.printf("E " fmt "\n", ##__VA_ARGS__)
#define LOG_W(fmt, ...) Serial.printf("W " fmt "\n", ##__VA_ARGS__)

static const uint32_t LOG_THROTTLE_MS = 5000;
static const uint32_t LOG_SUMMARY_MS = 60000;

inline bool log_throttle(uint32_t &last_ms, uint32_t interval_ms) {
  uint32_t now = millis();
  if ((now - last_ms) < interval_ms) {
    return false;
  }
  last_ms = now;
  return true;
}
