#pragma once
#include <Arduino.h>

// Monotonic microsecond tick since boot. Uses esp_timer (ESP32-S3 SYSTIMER
// clocked from the 40 MHz XTAL), not the drifting RTC RC_SLOW domain.
uint64_t clock_ticks_us();

// Anchor the device clock from a phone-supplied unix time observed at a
// device tick. RAM-only: valid for the current boot, reset on reboot.
void clock_set_anchor(uint64_t unix_s, uint64_t ticks_us);
bool clock_has_anchor();

// Resolve a file's start tick to unix seconds. Returns false when the file
// belongs to a different boot, no anchor exists, or the result is not a
// plausible positive time. Files recorded before the anchor are handled by
// the signed tick delta.
bool clock_resolve(uint32_t file_boot_id, uint64_t start_ticks_us, uint64_t *out_unix_s);
