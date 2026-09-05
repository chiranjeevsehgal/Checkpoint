#pragma once
#include <Arduino.h>
void ui_init();
void ui_task(void *arg);
void ui_signal_recording(bool on);
void ui_signal_vad_listening(); // VAD idle: mic ON, listening, no speech yet
void ui_signal_bookmark();
void ui_signal_error();
void ui_signal_fatal();
