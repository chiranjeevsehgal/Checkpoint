#pragma once
#include <Arduino.h>

// Deep-sleep power management. Single task: called from setup()/loop() only.
//
// Idle policy: once the microphone is off (recorder stopped) for
// POWER_IDLE_SLEEP_MS with no BLE link, no transfer and no unsynced
// recordings, the device enters deep sleep. Wake is a held press on
// HW_BUTTON_GPIO: the first POWER_WAKE_HOLD_MS are validated in firmware
// (hardware wake fires on level, not duration).

// Run early in setup(): on a deep-sleep wake, validates the wake hold and
// flashes blue; re-enters sleep if the button was released too early.
// A no-op on cold boot, software reset and panic.
void power_init();

// Run from loop(). Enters deep sleep once the idle and gating conditions hold.
void power_poll();

// Prepares peripherals and enters deep sleep. Does not return.
void power_sleep_now();
