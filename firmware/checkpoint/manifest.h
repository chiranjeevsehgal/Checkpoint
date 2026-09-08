#pragma once
#include <Arduino.h>

struct ManifestEntry {
  String path;
  uint32_t size;
  uint32_t crc;
  uint32_t uid; // random per-file id for the transfer protocol (0 = legacy)
  uint32_t created_ms;
  bool pending;
  uint16_t next_seq;
};

bool manifest_init();
bool manifest_scan_and_recover();
bool manifest_get_pending(ManifestEntry *out, size_t max_count, size_t *found);
// Copies all entries regardless of pending flag (for remote file listing).
bool manifest_get_all(ManifestEntry *out, size_t max_count, size_t *found);
size_t manifest_entry_count();
bool manifest_mark_uploading(const String &path, uint16_t next_seq);
bool manifest_mark_done(const String &path);
bool manifest_update_seq(const String &path, uint16_t next_seq);
bool manifest_save();
bool manifest_load();
// Stable per-file transfer id, generated once and persisted. Falls back to
// crc ^ size only for entries that predate uid (uid == 0 is never returned).
uint32_t manifest_uid_or_generate(const String &path, uint32_t fallback);
bool manifest_add_file(const String &path, uint32_t size);
bool manifest_add_file(const String &path, uint32_t size, uint32_t crc);
bool manifest_set_crc(const String &path, uint32_t crc);
size_t manifest_pending_count();
