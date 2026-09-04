#pragma once
#include <Arduino.h>

bool recorder_init();
bool recorder_start();
void recorder_stop();
bool recorder_is_recording();
uint32_t recorder_chunks_written();
String recorder_current_file();
void recorder_task(void *arg);
void recorder_notify_bookmark();
uint32_t recorder_dropped_bytes();
uint32_t recorder_drop_events();
