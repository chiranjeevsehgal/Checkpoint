#pragma once
#include <Arduino.h>
bool transfer_init();
void transfer_task(void *arg);
void transfer_on_packet(const uint8_t *data, size_t len);
bool transfer_is_busy();
String transfer_current_file();
// True while the transfer task holds this path open for upload.
bool transfer_is_transferring(const String &path);
// Queue a one-shot preview fetch. The transfer task sends exactly this file
// and, unlike sync, leaves the SD file and the manifest untouched.
void transfer_request_fetch(const char *path, size_t len);
