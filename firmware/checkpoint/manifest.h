#pragma once
#include <Arduino.h>

struct ManifestEntry {
  String path;
  uint32_t size;
  uint32_t crc;
  uint32_t created_ms;
  bool pending;
  uint16_t next_seq;
};

bool manifest_init();
bool manifest_scan_and_recover();
bool manifest_get_pending(ManifestEntry *out, size_t max_count, size_t *found);
bool manifest_mark_uploading(const String &path, uint16_t next_seq);
bool manifest_mark_done(const String &path);
bool manifest_update_seq(const String &path, uint16_t next_seq);
bool manifest_save();
bool manifest_load();
bool manifest_add_file(const String &path, uint32_t size);
bool manifest_add_file(const String &path, uint32_t size, uint32_t crc);
bool manifest_set_crc(const String &path, uint32_t crc);
size_t manifest_pending_count();
