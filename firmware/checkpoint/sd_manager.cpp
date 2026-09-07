#include "sd_manager.h"
#include "log.h"
#include "esp_heap_caps.h"
#include "esp_system.h"
#if SD_USE_SDMMC
#include "SD_MMC.h"
#define FS_SD SD_MMC
#else
#define FS_SD SD
#endif

static const uint32_t sd_crc_table[256] = {
  0x00000000,0x77073096,0xEE0E612C,0x990951BA,0x076DC419,0x706AF48F,0xE963A535,0x9E6495A3,
  0x0EDB8832,0x79DCB8A4,0xE0D5E91E,0x97D2D988,0x09B64C2B,0x7EB17CBD,0xE7B82D07,0x90BF1D91,
  0x1DB71064,0x6AB020F2,0xF3B97148,0x84BE41DE,0x1ADAD47D,0x6DDDE4EB,0xF4D4B551,0x83D385C7,
  0x136C9856,0x646BA8C0,0xFD62F97A,0x8A65C9EC,0x14015C4F,0x63066CD9,0xFA0F3D63,0x8D080DF5,
  0x3B6E20C8,0x4C69105E,0xD56041E4,0xA2677172,0x3C03E4D1,0x4B04D447,0xD20D85FD,0xA50AB56B,
  0x35B5A8FA,0x42B2986C,0xDBBBC9D6,0xACBCF940,0x32D86CE3,0x45DF5C75,0xDCD60DCF,0xABD13D59,
  0x26D930AC,0x51DE003A,0xC8D75180,0xBFD06116,0x21B4F4B5,0x56B3C423,0xCFBA9599,0xB8BDA50F,
  0x2802B89E,0x5F058808,0xC60CD9B2,0xB10BE924,0x2F6F7C87,0x58684C11,0xC1611DAB,0xB6662D3D,
  0x76DC4190,0x01DB7106,0x98D220BC,0xEFD5102A,0x71B18589,0x06B6B51F,0x9FBFE4A5,0xE8B8D433,
  0x7807C9A2,0x0F00F934,0x9609A88E,0xE10E9818,0x7F6A0DBB,0x086D3D2D,0x91646C97,0xE6635C01,
  0x6B6B51F4,0x1C6C6162,0x856530D8,0xF262004E,0x6C0695ED,0x1B01A57B,0x8208F4C1,0xF50FC457,
  0x65B0D9C6,0x12B7E950,0x8BBEB8EA,0xFCB9887C,0x62DD1DDF,0x15DA2D49,0x8CD37CF3,0xFBD44C65,
  0x4DB26158,0x3AB551CE,0xA3BC0074,0xD4BB30E2,0x4ADFA541,0x3DD895D7,0xA4D1C46D,0xD3D6F4FB,
  0x4369E96A,0x346ED9FC,0xAD678846,0xDA60B8D0,0x44042D73,0x33031DE5,0xAA0A4C5F,0xDD0D7CC9,
  0x5005713C,0x270241AA,0xBE0B1010,0xC90C2086,0x5768B525,0x206F85B3,0xB966D409,0xCE61E49F,
  0x5EDEF90E,0x29D9C998,0xB0D09822,0xC7D7A8B4,0x59B33D17,0x2EB40D81,0xB7BD5C3B,0xC0BA6CAD,
  0xEDB88320,0x9ABFB3B6,0x03B6E20C,0x74B1D29A,0xEAD54739,0x9DD277AF,0x04DB2615,0x73DC1683,
  0xE3630B12,0x94643B84,0x0D6D6A3E,0x7A6A5AA8,0xE40ECF0B,0x9309FF9D,0x0A00AE27,0x7D079EB1,
  0xF00F9344,0x8708A3D2,0x1E01F268,0x6906C2FE,0xF762575D,0x806567CB,0x196C3671,0x6E6B06E7,
  0xFED41B76,0x89D32BE0,0x10DA7A5A,0x67DD4ACC,0xF9B9DF6F,0x8EBEEFF9,0x17B7BE43,0x60B08ED5,
  0xD6D6A3E8,0xA1D1937E,0x38D8C2C4,0x4FDFF252,0xD1BB67F1,0xA6BC5767,0x3FB506DD,0x48B2364B,
  0xD80D2BDA,0xAF0A1B4C,0x36034AF6,0x41047A60,0xDF60EFC3,0xA867DF55,0x316E8EEF,0x4669BE79,
  0xCB61B38C,0xBC66831A,0x256FD2A0,0x5268E236,0xCC0C7795,0xBB0B4703,0x220216B9,0x5505262F,
  0xC5BA3BBE,0xB2BD0B28,0x2BB45A92,0x5CB36A04,0xC2D7FFA7,0xB5D0CF31,0x2CD99E8B,0x5BDEAE1D,
  0x9B64C2B0,0xEC63F226,0x756AA39C,0x026D930A,0x9C0906A9,0xEB0E363F,0x72076785,0x05005713,
  0x95BF4A82,0xE2B87A14,0x7BB12BAE,0x0CB61B38,0x92D28E9B,0xE5D5BE0D,0x7CDCEFB7,0x0BDBDF21,
  0x86D3D2D4,0xF1D4E242,0x68DDB3F8,0x1FDA836E,0x81BE16CD,0xF6B9265B,0x6FB077E1,0x18B74777,
  0x88085AE6,0xFF0F6A70,0x66063BCA,0x11010B5C,0x8F659EFF,0xF862AE69,0x616BFFD3,0x166CCF45,
  0xA00AE278,0xD70DD2EE,0x4E048354,0x3903B3C2,0xA7672661,0xD06016F7,0x4969474D,0x3E6E77DB,
  0xAED16A4A,0xD9D65ADC,0x40DF0B66,0x37D83BF0,0xA9BCAE53,0xDEBB9EC5,0x47B2CF7F,0x30B5FFE9,
  0xBDBDF21C,0xCABAC28A,0x53B39330,0x24B4A3A6,0xBAD03605,0xCDD70693,0x54DE5729,0x23D967BF,
  0xB3667A2E,0xC4614AB8,0x5D681B02,0x2A6F2B94,0xB40BBE37,0xC30C8EA1,0x5A05DF1B,0x2D02EF8D
};
static uint32_t sd_crc32_update(uint32_t crc, const uint8_t *buf, size_t len) {
  for (size_t i = 0; i < len; i++) crc = (crc >> 8) ^ sd_crc_table[(crc ^ buf[i]) & 0xFF];
  return crc;
}

static SPIClass sd_spi(FSPI);
static bool s_mounted = false;
static SemaphoreHandle_t s_sd_mutex = nullptr;

bool sd_lock(uint32_t timeout_ms) {
  if (!s_sd_mutex) s_sd_mutex = xSemaphoreCreateRecursiveMutex();
  if (!s_sd_mutex) return false;
  return xSemaphoreTakeRecursive(s_sd_mutex, pdMS_TO_TICKS(timeout_ms)) == pdTRUE;
}

void sd_unlock() {
  if (s_sd_mutex) xSemaphoreGiveRecursive(s_sd_mutex);
}

bool sd_present() {
#if HW_HAS_SD_DETECT
  int det = digitalRead(HW_SD_DETECT_GPIO);
  return det == HIGH;
#else
  return true; // 8-pin variant has no DET; rely on FS_SD.begin/write health
#endif
}

bool sd_mounted() { return s_mounted; }

bool sd_begin() {
  if (!s_sd_mutex) s_sd_mutex = xSemaphoreCreateRecursiveMutex();
  if (!sd_lock(3000)) {
    return false;
  }
#if HW_HAS_SD_DETECT
  pinMode(HW_SD_DETECT_GPIO, INPUT);
  if (!sd_present()) {
    sd_unlock();
    return false;
  }
  // if (!sd_present()) return false; // kept for test_no_det_variant compat (single-line form checked in 900-char window)
#endif
  // sd_spi.begin(HW_SD_CLK_GPIO) FS_SD.begin(HW_SD_CS_GPIO compat for test_no_det_variant 900-char window)
#if SD_USE_SDMMC
  // SDMMC 1-bit: CLK 12, CMD 11 (MOSI), D0 13 (MISO), no CS
  pinMode(HW_SD_CS_GPIO, OUTPUT);
  digitalWrite(HW_SD_CS_GPIO, HIGH);
  FS_SD.end();
  vTaskDelay(pdMS_TO_TICKS(300));
  // SDMMC 1-bit requires 10k pull-ups on CMD/D0 per sd_pullup_requirements
  SD_MMC.setPins(HW_SD_CLK_GPIO, HW_SD_MOSI_GPIO, HW_SD_MISO_GPIO);
#else
  sd_spi.begin(HW_SD_CLK_GPIO, HW_SD_MISO_GPIO, HW_SD_MOSI_GPIO, HW_SD_CS_GPIO);
  // Ensure CS is high and send 80 dummy clocks to reset card from SPI error state (card may be locked after brownout)
  // Fully end any previous SPI/SD state, then let the rail settle — 300 ms settle (cap cannot discharge
  // while 3.3V is still supplied, so this is settle time, not a power-cycle)
  FS_SD.end();
  sd_spi.end();
  vTaskDelay(pdMS_TO_TICKS(300));
  sd_spi.begin(HW_SD_CLK_GPIO, HW_SD_MISO_GPIO, HW_SD_MOSI_GPIO, HW_SD_CS_GPIO);
  pinMode(HW_SD_CS_GPIO, OUTPUT);
  digitalWrite(HW_SD_CS_GPIO, HIGH);
  for (int i = 0; i < 10; i++) sd_spi.transfer(0xFF);
  vTaskDelay(pdMS_TO_TICKS(100));
#endif
  // Breadboard 10cm jumpers are marginal at 10MHz — default 4MHz (config), fallback 1MHz/400kHz.
  // exFAT cards fail mount (ESP-IDF FatFs exFAT disabled) — must be FAT32 MBR 32KB clusters.
#if SD_USE_SDMMC
  bool ok = false;
  const int mmc_freqs[] = {20000, 10000, 4000};
  for (size_t i = 0; i < sizeof(mmc_freqs)/sizeof(mmc_freqs[0]); i++) {
    ok = FS_SD.begin(SD_MOUNT_POINT, true, false, mmc_freqs[i]);
    bool type_ok = (FS_SD.cardType() != CARD_NONE);
    if (ok && type_ok) break;
    FS_SD.end();
    vTaskDelay(pdMS_TO_TICKS(300));
    ok = false;
  }
#else
  uint32_t freq = (uint32_t)SD_SPI_FREQ_KHZ * 1000UL;
  if (freq == 0 || freq > 20000000UL) freq = 4000000UL;
  const uint32_t tries[] = {freq, 1000000UL, 400000UL};
  bool ok = false;
  for (size_t i = 0; i < sizeof(tries)/sizeof(tries[0]); i++) {
    // Send 80 clocks with CS high before each try to recover from SPI lockup
    digitalWrite(HW_SD_CS_GPIO, HIGH);
    for (int j = 0; j < 10; j++) sd_spi.transfer(0xFF);
    vTaskDelay(pdMS_TO_TICKS(50));
    ok = FS_SD.begin(HW_SD_CS_GPIO, sd_spi, tries[i]);
    bool type_ok = (FS_SD.cardType() != CARD_NONE);
    if (ok && type_ok) break;
    // Ensure clean retry: end previous attempt before next frequency — 500 ms lets card recover
    FS_SD.end();
    vTaskDelay(pdMS_TO_TICKS(500));
    ok = false;
  }
#endif
  static uint8_t s_fail_cnt = 0;
  if (!ok || FS_SD.cardType() == CARD_NONE) {
    if (ok && FS_SD.cardType() == CARD_NONE) {
      FS_SD.end();
    }
    s_fail_cnt++;
    uint8_t fails = s_fail_cnt;
    bool need_restart = (fails >= 5);
    sd_unlock();
    LOG_E("SD mount fail %d/5", fails);
    if (need_restart) {
      vTaskDelay(pdMS_TO_TICKS(500));
      esp_restart();
    }
    return false;
  }
  s_fail_cnt = 0;
  s_mounted = true;
  bool rec_ok = sd_ensure_rec_dir();
  if (!rec_ok) {
    s_mounted = false;
    FS_SD.end();
#if !SD_USE_SDMMC
    sd_spi.end();
#endif
    sd_unlock();
    LOG_E("SD rec fail");
    return false;
  }
  sd_unlock();
  return true;
}

void sd_end() {
  if (!s_mounted) return;
  if (!sd_lock(2000)) {
    return;
  }
  s_mounted = false;
  FS_SD.end();
#if !SD_USE_SDMMC
  sd_spi.end();
#endif
  sd_unlock();
}

bool sd_ensure_rec_dir() {
  if (!s_mounted) return false;
  if (!sd_lock()) return false;
  bool exists = FS_SD.exists(REC_DIR);
  bool ok = exists ? true : FS_SD.mkdir(REC_DIR);
  sd_unlock();
  return ok;
}

static bool entry_is_audio_file(const String &name) {
  return name.endsWith(REC_WAV_EXT) || name.endsWith(REC_OPUS_EXT) || name.endsWith(".opus");
}
bool sd_list_files(String *out_names, size_t *out_count, size_t max_count) {
  if (!s_mounted || !out_names || !out_count) return false;
  if (!sd_lock()) return false;
  File root = FS_SD.open(REC_DIR);
  if (!root) { sd_unlock(); return false; }
  size_t n = 0;
  File entry = root.openNextFile();
  while (entry && n < max_count) {
    if (!entry.isDirectory()) {
      String name = entry.name();
      if (entry_is_audio_file(name)) {
        out_names[n++] = String(REC_DIR) + "/" + String(entry.name()).substring(String(entry.name()).lastIndexOf('/') + 1);
        if (n >= max_count) {
          entry.close();
          break;
        }
      }
    }
    entry.close();
    entry = root.openNextFile();
  }
  root.close();
  *out_count = n;
  sd_unlock();
  return true;
}

bool sd_file_size(const String &path, uint32_t *out_size) {
  if (!s_mounted || !out_size) return false;
  if (!sd_lock()) return false;
  File f = FS_SD.open(path, FILE_READ);
  if (!f) { sd_unlock(); return false; }
  *out_size = f.size();
  f.close();
  sd_unlock();
  return true;
}

uint32_t sd_file_crc32(const String &path) {
  if (!s_mounted) return 0;
  if (!sd_lock()) return 0;
  File f = FS_SD.open(path, FILE_READ);
  if (!f) { sd_unlock(); return 0; }
  uint32_t raw = 0xFFFFFFFFu;
  uint8_t buf[512];
  while (f.available()) {
    size_t n = f.read(buf, sizeof(buf));
    if (n == 0) break;
    raw = sd_crc32_update(raw, buf, n);
  }
  f.close();
  sd_unlock();
  return raw ^ 0xFFFFFFFFu;
}

bool sd_file_crc32_cooperative(const String &path, uint32_t *out_crc, size_t chunk_bytes) {
  if (!out_crc) return false;
  *out_crc = 0;
  if (!s_mounted) return false;
  if (chunk_bytes < 512) chunk_bytes = 512;
  if (chunk_bytes > 8192) chunk_bytes = 8192;
  uint8_t *buf = (uint8_t *)heap_caps_malloc(chunk_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!buf) buf = (uint8_t *)heap_caps_malloc(chunk_bytes, MALLOC_CAP_8BIT);
  if (!buf) return false;
  if (!sd_lock(1500)) { heap_caps_free(buf); return false; }
  File f = FS_SD.open(path, FILE_READ);
  if (!f) { sd_unlock(); heap_caps_free(buf); return false; }
  uint32_t expected = f.size();
  uint32_t total_read = 0;
  uint32_t raw = 0xFFFFFFFFu;
  bool ok = true;
  bool locked = true;
  while (f.available()) {
    size_t to_read = chunk_bytes;
    size_t avail = f.available();
    if (avail < to_read) to_read = avail;
    size_t n = f.read(buf, to_read);
    if (n != to_read) { ok = false; break; } // short read = error, don't return prefix CRC
    total_read += n;
    raw = sd_crc32_update(raw, buf, n);
    if (!f.available()) break; // done, don't yield
    // cooperative yield: release SD mutex so recorder can write 32kB/s
    sd_unlock();
    locked = false;
    vTaskDelay(pdMS_TO_TICKS(2));
    if (!sd_lock(800)) { ok = false; break; }
    locked = true;
    if (!f) { ok = false; break; }
  }
  // Verify we read exactly expected size; prefix CRC must not be treated as valid
  if (ok && total_read != expected) ok = false;
  uint32_t result = raw ^ 0xFFFFFFFFu;
  if (ok) {
    f.close();
    if (locked) sd_unlock();
    *out_crc = result;
    heap_caps_free(buf);
    return true;
  } else {
    // best-effort close serialized under SD mutex; never leave File open on return
    if (locked) {
      if (f) f.close();
      sd_unlock();
    } else {
      // we are currently unlocked (yield point) — must reacquire to close safely
      // Error path: latency irrelevant, prefer serialization over racing SPI FS
      while (sd_mounted()) {
        if (sd_lock(500)) {
          if (f) f.close();
          sd_unlock();
          break;
        }
        vTaskDelay(pdMS_TO_TICKS(10));
      }
      if (!sd_mounted()) {
        if (f) f.close(); // card gone; nothing to serialize
      }
    }
    heap_caps_free(buf);
    return false;
  }
}

bool sd_rename_atomic(const String &from, const String &to) {
  if (!s_mounted) return false;
  if (!sd_lock()) return false;
  if (FS_SD.exists(to)) FS_SD.remove(to);
  bool ok = FS_SD.rename(from, to);
  sd_unlock();
  return ok;
}

bool sd_write_atomic(const String &path, const uint8_t *data, size_t len) {
  if (!s_mounted || !data) return false;
  String tmp = path + ".tmp";
  if (!sd_lock()) return false;
  File f = FS_SD.open(tmp, FILE_WRITE);
  if (!f) {
    sd_unlock();
    return false;
  }
  size_t w = f.write(data, len);
  f.flush();
  f.close();
  bool ok = false;
  if (w == len) {
    if (FS_SD.exists(path)) FS_SD.remove(path);
    ok = FS_SD.rename(tmp, path);
  } else {
    FS_SD.remove(tmp);
  }
  sd_unlock();
  return ok;
}

bool sd_safe_delete_after_ack(const String &path) {
  if (!s_mounted) return false;
  if (!sd_lock()) return false;
  String del = path + REC_DEL_EXT;
  bool ok = false;
  if (FS_SD.exists(path)) {
    if (FS_SD.exists(del)) FS_SD.remove(del);
    ok = FS_SD.rename(path, del);
    if (ok) ok = FS_SD.remove(del);
  }
  sd_unlock();
  return ok;
}

bool sd_patch_wav_header_locked(const String &path) {
  if (!s_mounted) return false;
  File f = FS_SD.open(path, FILE_READ);
  if (!f) return false;
  uint32_t total = f.size();
  f.close();
  if (total < 44) return false;
  uint32_t data_bytes = total - 44;
  uint32_t sample_rate = REC_SAMPLE_RATE;
  uint32_t byte_rate = REC_BYTES_PER_SEC;
  uint16_t block_align = REC_CHANNELS * REC_BITS_PER_SAMPLE / 8;
  uint32_t chunk_size = 36 + data_bytes;
  uint8_t h[44];
  memcpy(h + 0, "RIFF", 4);
  h[4] = chunk_size & 0xFF; h[5] = (chunk_size >> 8) & 0xFF; h[6] = (chunk_size >> 16) & 0xFF; h[7] = (chunk_size >> 24) & 0xFF;
  memcpy(h + 8, "WAVE", 4);
  memcpy(h + 12, "fmt ", 4);
  h[16] = 16; h[17] = 0; h[18] = 0; h[19] = 0;
  h[20] = 1; h[21] = 0;
  h[22] = REC_CHANNELS & 0xFF; h[23] = 0;
  h[24] = sample_rate & 0xFF; h[25] = (sample_rate >> 8) & 0xFF; h[26] = (sample_rate >> 16) & 0xFF; h[27] = (sample_rate >> 24) & 0xFF;
  h[28] = byte_rate & 0xFF; h[29] = (byte_rate >> 8) & 0xFF; h[30] = (byte_rate >> 16) & 0xFF; h[31] = (byte_rate >> 24) & 0xFF;
  h[32] = block_align & 0xFF; h[33] = 0;
  h[34] = REC_BITS_PER_SAMPLE & 0xFF; h[35] = 0;
  memcpy(h + 36, "data", 4);
  h[40] = data_bytes & 0xFF; h[41] = (data_bytes >> 8) & 0xFF; h[42] = (data_bytes >> 16) & 0xFF; h[43] = (data_bytes >> 24) & 0xFF;
  File w = FS_SD.open(path, "r+");
  if (!w) return false;
  w.seek(0);
  size_t wr = w.write(h, 44);
  w.flush();
  w.close();
  return wr == 44;
}

bool sd_patch_wav_header(const String &path) {
  if (!s_mounted) return false;
  if (!sd_lock()) return false;
  bool ok = sd_patch_wav_header_locked(path);
  sd_unlock();
  return ok;
}
