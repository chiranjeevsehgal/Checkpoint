#pragma once
#include <Arduino.h>
void ui_init();
void ui_task(void *arg);
void ui_signal_recording(bool on);
void ui_signal_vad_listening(); // VAD idle: mic ON, listening, no speech yet
void ui_signal_bookmark();
void ui_signal_error();
void ui_signal_fatal();
// Remote LED control (BLE stealth): muted suppresses OFF/ON/VAD_IDLE/BOOKMARK,
// ERROR/FATAL still show for diagnostics. Brightness 0-255, default HW_RGB_BRIGHTNESS.
void ui_set_muted(bool muted);
bool ui_is_muted();
void ui_set_brightness(uint8_t brightness);
uint8_t ui_get_brightness();
// Called after a remote (BLE) start/stop so the physical button does not
// immediately reverse it inside the 800ms toggle lockout window.
void ui_note_remote_action();
void ui_signal_enroll(bool on);
void ui_signal_auth_ok();
