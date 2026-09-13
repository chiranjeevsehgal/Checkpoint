#include "clock.h"
#include "recorder.h"
#include "esp_timer.h"

namespace {
bool s_anchor_valid = false;
uint64_t s_anchor_unix_s = 0;
uint64_t s_anchor_ticks_us = 0;
} // namespace

uint64_t clock_ticks_us() {
  return (uint64_t)esp_timer_get_time();
}

void clock_set_anchor(uint64_t unix_s, uint64_t ticks_us) {
  if (unix_s == 0) {
    return;
  }
  s_anchor_unix_s = unix_s;
  s_anchor_ticks_us = ticks_us;
  s_anchor_valid = true;
}

bool clock_has_anchor() {
  return s_anchor_valid;
}

bool clock_resolve(uint32_t file_boot_id, uint64_t start_ticks_us, uint64_t *out_unix_s) {
  if (out_unix_s == nullptr || !s_anchor_valid || file_boot_id == 0) {
    return false;
  }
  if (file_boot_id != recorder_boot_id()) {
    return false;
  }
  int64_t delta_us = (int64_t)(start_ticks_us - s_anchor_ticks_us);
  int64_t unix_s = (int64_t)s_anchor_unix_s + delta_us / 1000000;
  if (unix_s <= 0) {
    return false;
  }
  *out_unix_s = (uint64_t)unix_s;
  return true;
}
