#include "manifest.h"
#include "sd_manager.h"
#include <ArduinoJson.h>
#include "esp_heap_caps.h"
#include "esp_system.h"

static ManifestEntry *s_entries = nullptr;
static size_t s_capacity = 0;
static size_t s_count = 0;
static SemaphoreHandle_t s_manifest_mutex = nullptr;

static bool entry_is_wav(const String &name) { return name.endsWith(REC_WAV_EXT) || name.endsWith(REC_OPUS_EXT) || name.endsWith(".opus"); }
static bool entry_is_audio(const String &name) { return entry_is_wav(name); }

static void ensure_capacity() {
  if (s_entries) return;
  size_t want = SD_MAX_FILES;
  s_entries = (ManifestEntry*) heap_caps_malloc(sizeof(ManifestEntry) * want, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (s_entries) {
    s_capacity = want;
  } else {
    want = 256;
    s_entries = (ManifestEntry*) heap_caps_malloc(sizeof(ManifestEntry) * want, MALLOC_CAP_8BIT);
    if (s_entries) s_capacity = want;
    else { s_capacity = 0; return; }
  }
  for (size_t i = 0; i < s_capacity; i++) new (&s_entries[i]) ManifestEntry();
}

bool manifest_init() {
  if (!s_manifest_mutex) s_manifest_mutex = xSemaphoreCreateMutex();
  ensure_capacity();
  s_count = 0;
  return s_entries != nullptr;
}

bool manifest_load() {
  if (!s_entries) ensure_capacity();
  if (!sd_mounted()) return false;
  if (!sd_lock(1500)) return false;
  File f = SD.open(REC_MANIFEST, FILE_READ);
  if (!f) { sd_unlock(); return false; }
  size_t sz = f.size();
  if (sz == 0 || sz > 8192) { f.close(); sd_unlock(); return false; }
  String txt;
  txt.reserve(sz + 1);
  while (f.available()) txt += (char)f.read();
  f.close();
  sd_unlock();
  DynamicJsonDocument doc(8192);
  DeserializationError err = deserializeJson(doc, txt);
  if (err) return false;
  JsonArray arr = doc["files"].as<JsonArray>();
  if (arr.isNull()) return false;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  for (JsonObject o : arr) {
    String p = o["path"] | "";
    if (p.length() == 0) continue;
    for (size_t i = 0; i < s_count; i++) {
      if (s_entries[i].path == p) {
        s_entries[i].crc = o["crc"] | s_entries[i].crc;
        s_entries[i].next_seq = o["seq"] | s_entries[i].next_seq;
        s_entries[i].size = o["size"] | s_entries[i].size;
        s_entries[i].uid = o["uid"] | s_entries[i].uid;
        if (s_entries[i].uid == 0) {
          s_entries[i].uid = ((uint64_t)esp_random() << 32) | esp_random();
        }
        break;
      }
    }
  }
  xSemaphoreGive(s_manifest_mutex);
  return true;
}

bool manifest_scan_and_recover() {
  if (!s_entries) ensure_capacity();
  if (!s_entries) return false;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(2000)) != pdTRUE) return false;
  s_count = 0;
  if (!sd_mounted()) {
    xSemaphoreGive(s_manifest_mutex);
    return false;
  }
  xSemaphoreGive(s_manifest_mutex);

  // ── Pass 1: enumerate filenames into RAM — directory CLOSED before any mutation ──
  // Allocate name list on SPIRAM/heap. 256 slots × ~64 chars each is fine.
  const size_t MAX_SCAN = 256;
  String *names = (String *)heap_caps_malloc(sizeof(String) * MAX_SCAN, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!names) names = (String *)heap_caps_malloc(sizeof(String) * MAX_SCAN, MALLOC_CAP_8BIT);
  if (!names) return false;
  for (size_t i = 0; i < MAX_SCAN; i++) new (&names[i]) String();
  size_t name_count = 0;

  if (!sd_lock(2000)) {
    for (size_t i = 0; i < MAX_SCAN; i++) names[i].~String();
    heap_caps_free(names);
    return false;
  }
  {
    File root = SD.open(REC_DIR);
    if (!root) {
      sd_unlock();
      for (size_t i = 0; i < MAX_SCAN; i++) names[i].~String();
      heap_caps_free(names);
      return false;
    }
    File e = root.openNextFile();
    while (e && name_count < MAX_SCAN) {
      if (!e.isDirectory()) {
        String fname = e.name();
        String full = String(REC_DIR) + "/" + fname.substring(fname.lastIndexOf('/') + 1);
        // Skip manifest itself and its atomic-write tmp
        if (full != String(REC_MANIFEST) && full != String(REC_MANIFEST) + ".tmp") {
          names[name_count++] = full;
        }
      }
      e.close();
      e = root.openNextFile();
    }
    if (e) e.close();
    root.close();
  }
  // directory handle fully released — safe to mutate now
  sd_unlock();

  // ── Pass 2: recover/clean — no directory handle is open ──
  ManifestEntry *local = (ManifestEntry *)heap_caps_malloc(sizeof(ManifestEntry) * MAX_SCAN, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!local) local = (ManifestEntry *)heap_caps_malloc(sizeof(ManifestEntry) * MAX_SCAN, MALLOC_CAP_8BIT);
  if (!local) {
    for (size_t i = 0; i < MAX_SCAN; i++) names[i].~String();
    heap_caps_free(names);
    return false;
  }
  for (size_t i = 0; i < MAX_SCAN; i++) new (&local[i]) ManifestEntry();
  size_t local_count = 0;

  for (size_t i = 0; i < name_count; i++) {
    const String &full = names[i];
    if (full.endsWith(REC_TMP_EXT)) {
      // Abandoned .tmp — recover if large enough, else discard
      uint32_t sz = 0;
      if (!sd_lock(500)) continue;
      {
        File f = SD.open(full, FILE_READ);
        if (f) { sz = f.size(); f.close(); }
      }
      sd_unlock();
      if (sz < 1024) {
        if (sd_lock(500)) { SD.remove(full); sd_unlock(); }
      } else {
        String wav = full.substring(0, full.length() - String(REC_TMP_EXT).length());
        if (sd_lock(500)) {
          if (SD.exists(wav)) SD.remove(wav);
          bool renamed = SD.rename(full, wav);
          if (renamed && wav.endsWith(REC_WAV_EXT)) sd_patch_wav_header_locked(wav);
          sd_unlock();
        }
        // overwrite the name slot so Pass 3 sees it as audio
        names[i] = wav;
      }
      continue;
    }
    if (full.endsWith(REC_DEL_EXT)) {
      if (sd_lock(500)) { SD.remove(full); sd_unlock(); }
      continue;
    }
    if (entry_is_audio(full) && local_count < MAX_SCAN && local_count < s_capacity) {
      uint32_t sz = 0;
      if (sd_lock(500)) {
        File f = SD.open(full, FILE_READ);
        if (f) { sz = f.size(); f.close(); }
        sd_unlock();
      }
      local[local_count].path = full;
      local[local_count].size = sz;
      local[local_count].crc = 0;
      local[local_count].created_ms = millis();
      local[local_count].pending = true;
      local[local_count].next_seq = 0;
      local_count++;
    }
  }

  for (size_t i = 0; i < MAX_SCAN; i++) names[i].~String();
  heap_caps_free(names);

  // ── Pass 3: sort, commit to RAM manifest, merge saved JSON, re-save ──
  // Bubble sort by path (lexicographic = time order for REC_XXXXXX_XXXX.wav names)
  for (size_t i = 0; i < local_count; i++)
    for (size_t j = i + 1; j < local_count; j++)
      if (local[j].path < local[i].path) {
        ManifestEntry tmp = local[i]; local[i] = local[j]; local[j] = tmp;
      }

  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) {
    for (size_t k = 0; k < MAX_SCAN; k++) local[k].~ManifestEntry();
    heap_caps_free(local);
    return false;
  }
  s_count = local_count < s_capacity ? local_count : s_capacity;
  for (size_t i = 0; i < s_count; i++) s_entries[i] = local[i];
  xSemaphoreGive(s_manifest_mutex);
  for (size_t k = 0; k < MAX_SCAN; k++) local[k].~ManifestEntry();
  heap_caps_free(local);

  // Merge seq/crc from on-disk manifest.json if present, then persist
  manifest_load();
  manifest_save();
  return true;
}

bool manifest_get_pending(ManifestEntry *out, size_t max_count, size_t *found) {
  if (!out || !found) return false;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  size_t n = 0;
  for (size_t i = 0; i < s_count && n < max_count; i++) {
    if (s_entries[i].pending) out[n++] = s_entries[i];
  }
  *found = n;
  xSemaphoreGive(s_manifest_mutex);
  return true;
}

bool manifest_get_all(ManifestEntry *out, size_t max_count, size_t *found) {
  if (!out || !found) return false;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  size_t n = 0;
  for (size_t i = 0; i < s_count && n < max_count; i++) {
    out[n++] = s_entries[i];
  }
  *found = n;
  xSemaphoreGive(s_manifest_mutex);
  return true;
}

size_t manifest_entry_count() {
  size_t c = 0;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(200)) != pdTRUE) return 0;
  c = s_count;
  xSemaphoreGive(s_manifest_mutex);
  return c;
}

bool manifest_update_seq(const String &path, uint16_t next_seq) {
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  for (size_t i = 0; i < s_count; i++)
    if (s_entries[i].path == path) s_entries[i].next_seq = next_seq;
  xSemaphoreGive(s_manifest_mutex);
  return true;
}

bool manifest_mark_uploading(const String &path, uint16_t next_seq) {
  return manifest_update_seq(path, next_seq);
}

bool manifest_add_file(const String &path, uint32_t size) {
  return manifest_add_file(path, size, 0);
}

bool manifest_add_file(const String &path, uint32_t size, uint32_t crc) {
  if (!s_entries) ensure_capacity();
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  // dedup — update size/crc if already present
  for (size_t i = 0; i < s_count; i++) if (s_entries[i].path == path) {
    s_entries[i].size = size;
    if (crc != 0) s_entries[i].crc = crc;
    xSemaphoreGive(s_manifest_mutex);
    manifest_save();
    return true;
  }
  if (s_count >= s_capacity) { xSemaphoreGive(s_manifest_mutex); return false; }
  s_entries[s_count].path = path;
  s_entries[s_count].size = size;
  s_entries[s_count].crc = crc;
  s_entries[s_count].uid = ((uint64_t)esp_random() << 32) | esp_random();
  if (s_entries[s_count].uid == 0) s_entries[s_count].uid = 1;
  s_entries[s_count].created_ms = millis();
  s_entries[s_count].pending = true;
  s_entries[s_count].next_seq = 0;
  s_count++;
  // keep sorted
  for (size_t i = 0; i < s_count; i++)
    for (size_t j = i + 1; j < s_count; j++)
      if (s_entries[j].path < s_entries[i].path) {
        ManifestEntry tmp = s_entries[i];
        s_entries[i] = s_entries[j];
        s_entries[j] = tmp;
      }
  xSemaphoreGive(s_manifest_mutex);
  manifest_save();
  return true;
}

bool manifest_set_crc(const String &path, uint32_t crc) {
  if (!s_entries) ensure_capacity();
  if (crc == 0) return false; // 0 is sentinel for "unknown"; don't persist zero per protocol (file_id = crc ^ total)
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  bool found = false;
  for (size_t i = 0; i < s_count; i++) if (s_entries[i].path == path) {
    s_entries[i].crc = crc;
    found = true;
    break;
  }
  xSemaphoreGive(s_manifest_mutex);
  if (found) manifest_save();
  return found;
}

bool manifest_set_time(const String &path, uint32_t boot_id, uint64_t start_ticks_us) {
  if (!s_entries) ensure_capacity();
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  bool found = false;
  for (size_t i = 0; i < s_count; i++) if (s_entries[i].path == path) {
    s_entries[i].time_boot_id = boot_id;
    s_entries[i].start_ticks_us = start_ticks_us;
    found = true;
    break;
  }
  xSemaphoreGive(s_manifest_mutex);
  return found;
}

bool manifest_mark_done(const String &path) {
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  size_t w = 0;
  for (size_t i = 0; i < s_count; i++) {
    if (s_entries[i].path == path) continue;
    if (w != i) s_entries[w] = s_entries[i];
    w++;
  }
  s_count = w;
  xSemaphoreGive(s_manifest_mutex);
  manifest_save();
  return true;
}

size_t manifest_pending_count() {
  size_t c = 0;
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(200)) != pdTRUE) return 0;
  for (size_t i = 0; i < s_count; i++) if (s_entries[i].pending) c++;
  xSemaphoreGive(s_manifest_mutex);
  return c;
}

bool manifest_save() {
  if (!sd_mounted()) return false;
  DynamicJsonDocument doc(8192);
  JsonArray arr = doc.createNestedArray("files");
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
  for (size_t i = 0; i < s_count; i++) {
    JsonObject o = arr.createNestedObject();
    o["path"] = s_entries[i].path;
    o["size"] = s_entries[i].size;
    o["crc"] = s_entries[i].crc;
    o["uid"] = s_entries[i].uid;
    o["seq"] = s_entries[i].next_seq;
  }
  xSemaphoreGive(s_manifest_mutex);
  String out;
  serializeJson(doc, out);
  return sd_write_atomic(String(REC_MANIFEST), (const uint8_t *)out.c_str(), out.length());
}

uint64_t manifest_uid_or_generate(const String &path, uint64_t fallback) {
  if (xSemaphoreTake(s_manifest_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return fallback;
  uint64_t uid = fallback;
  for (size_t i = 0; i < s_count; i++) {
    if (s_entries[i].path == path) {
      if (s_entries[i].uid == 0) {
        s_entries[i].uid = ((uint64_t)esp_random() << 32) | esp_random();
      }
      if (s_entries[i].uid == 0) s_entries[i].uid = 1;
      uid = s_entries[i].uid;
      break;
    }
  }
  xSemaphoreGive(s_manifest_mutex);
  if (uid != fallback) manifest_save();
  return uid;
}
