#pragma once
#include <Arduino.h>

bool recorder_init();
bool recorder_start();
void recorder_stop();
bool recorder_is_recording();
uint32_t recorder_chunks_written();
String recorder_current_file();
uint32_t recorder_boot_id();
void recorder_task(void *arg);
void recorder_notify_bookmark();
uint32_t recorder_dropped_bytes();
uint32_t recorder_drop_events();
// VAD — voice-triggered recording state (vad.{h,cpp})
bool recorder_vad_active();    // file open, capturing an utterance
bool recorder_vad_speech();    // currently in voiced frames (vs hangover/session pause)
float recorder_vad_level_dbfs(); // last 20ms frame level
uint32_t recorder_vad_utterances(); // completed utterances since boot
