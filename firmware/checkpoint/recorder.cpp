#include "recorder.h"
#include "config.h"
#include "sd_manager.h"
#include "manifest.h"
#include "ui.h"
#include "opus_codec.h"
#include "ogg_mux.h"
#include "vad.h"
#include "driver/i2s.h"
#include "freertos/ringbuf.h"
#include "esp_heap_caps.h"
#include "esp_timer.h"
#include <Preferences.h>

static TaskHandle_t s_task = nullptr;
static TaskHandle_t s_encode_task = nullptr;
static volatile bool s_recording = false;
static volatile bool s_bookmark = false;
static uint32_t s_chunks = 0;
static uint32_t s_file_seq = 0;
static uint32_t s_boot_id = 0;
static String s_current = "";
static SemaphoreHandle_t s_mutex = nullptr;
static RingbufHandle_t s_ring = nullptr; // PCM 16k mono
static RingbufHandle_t s_opus_ring = nullptr; // encoded frames (raw opus 40B each + 2B len prefix) or OGG pages
static volatile uint32_t s_dropped_bytes = 0;
static volatile uint32_t s_drop_events = 0;
// Diagnostics: prove the true capture rate. exp_frames = elapsed_ms/20.
// If opus_frames << exp_frames with drops rising -> pipeline/SD stall.
// If opus_frames ~= exp_frames but file still short/fast -> clock/granule bug.
static volatile uint32_t s_i2s_frames = 0; // WS ticks kept (should be 16000/s)
static volatile uint32_t s_pcm_bytes_total = 0;

// Opus state
static rec_opus_encoder_t *s_encoder = nullptr;
// Libopus encoder state is NOT thread-safe: recorder_task (close_chunk PCM
// drain) and opus_encode_task both called opus_encode(s_encoder,...)
// concurrently with no lock -> heap/state corruption (Double exception / WDT
// after the first canary trip). Serialize all opus_encode calls.
static SemaphoreHandle_t s_enc_mutex = nullptr;
static inline bool enc_lock(uint32_t ms = 1000) {
  if (!s_enc_mutex) return false;
  return xSemaphoreTake(s_enc_mutex, pdMS_TO_TICKS(ms)) == pdTRUE;
}
static inline void enc_unlock() {
  if (s_enc_mutex) xSemaphoreGive(s_enc_mutex);
}
static uint32_t s_opus_serial = 0;
static uint32_t s_opus_seq = 0;
static uint64_t s_opus_granule = 0; // 48k timeline
static uint32_t s_opus_frames = 0;
static uint64_t s_encode_time_us_sum = 0;
static uint32_t s_encode_frames = 0;

// VAD — voice-triggered recording. opus_encode_task is the sole writer of the
// request flags; recorder_task (owner of s_file) is the sole clearer.
static volatile bool s_vad_open_req = false;  // ONSET: recorder_task should open_chunk()
static volatile bool s_vad_close_req = false; // OFFSET: recorder_task should close_chunk()
static volatile bool s_vad_discard_req = false; // DISCARD: hum — purge opus_ring, drop file
static volatile uint32_t s_vad_utt_frames = 0; // voiced+hangover frames in open utterance
static int16_t *s_preroll = nullptr; // circular PCM pre-roll (VAD_PREROLL_MS of 20ms frames)
static uint32_t s_preroll_frames = 0;
static uint32_t s_preroll_head = 0; // next write slot
static uint32_t s_preroll_count = 0;
static inline uint32_t vad_preroll_frames_cfg() {
  uint32_t f = (VAD_PREROLL_MS / REC_OPUS_FRAME_MS);
  if (f < 1) f = 1;
  if (f > 100) f = 100; // cap 2s
  return f;
}
static inline uint32_t vad_min_speech_frames_cfg() {
  uint32_t f = (VAD_MIN_SPEECH_MS / REC_OPUS_FRAME_MS);
  return f; // 0 = disabled
}

static void opus_encode_task(void *arg);

#if VAD_ENABLE
// Encode one 320-sample frame and push length-prefixed to opus_ring.
// Buffers are heap-owned (canary-safe). Updates encode stats + drop counters.
static void vad_encode_push(short *pcm_s16, uint8_t *frame_bytes, uint8_t *packet) {
  int64_t t0 = esp_timer_get_time();
  int n = 0;
  if (enc_lock()) {
    n = opus_encode(s_encoder, pcm_s16, REC_OPUS_SAMPLES_PER_FRAME, frame_bytes, REC_OPUS_MAX_FRAME_BYTES);
    enc_unlock();
  }
  int64_t dt = esp_timer_get_time() - t0;
  s_encode_time_us_sum += dt; s_encode_frames++;
  if (n > 0 && n <= REC_OPUS_MAX_FRAME_BYTES) {
    packet[0] = n & 0xFF; packet[1] = (n >> 8) & 0xFF;
    memcpy(packet + 2, frame_bytes, n);
    BaseType_t sent = xRingbufferSend(s_opus_ring, packet, n + 2, pdMS_TO_TICKS(10));
    if (sent != pdTRUE) {
      s_dropped_bytes += n;
      s_drop_events++;
    } else {
      s_vad_utt_frames++;
    }
    if (dt > 8000) Serial.printf("REC encode slow %lldus n=%d frames %lu\n", dt, n, (unsigned long)s_encode_frames);
  }
}
#endif
static bool write_opus_sliced(const uint8_t *opus, size_t olen, bool *fs_lost_out);

static uint32_t load_boot_id() {
  Preferences pref;
  uint32_t id = 0;
  if (pref.begin("checkpoint", false)) {
    id = pref.getUInt("boot_id", 0) + 1;
    pref.putUInt("boot_id", id);
    pref.end();
  }
  return id;
}

static File s_file;
static String s_tmp_path;
static String s_final_path;
static uint32_t s_bytes_in_chunk = 0;
static uint32_t s_chunk_start_ms = 0;

// Legacy WAV header for migration — kept for old .wav recovery and tests
static void write_wav_header(File &f, uint32_t data_bytes) {
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
  f.seek(0);
  f.write(h, 44);
}

static String chunk_name() {
  (void)s_chunks;
  char buf[64];
#if REC_CODEC_OPUS
  snprintf(buf, sizeof(buf), REC_DIR "/REC_%06lu_%04lu" REC_OPUS_EXT,
           (unsigned long)(s_boot_id % 1000000), (unsigned long)(s_file_seq % 10000));
#else
  snprintf(buf, sizeof(buf), REC_DIR "/REC_%06lu_%04lu" REC_WAV_EXT,
           (unsigned long)(s_boot_id % 1000000), (unsigned long)(s_file_seq % 10000));
#endif
  return String(buf);
}

static bool open_chunk() {
#if HW_HAS_SD_DETECT
  if (!sd_mounted() || !sd_present()) return false;
#else
  if (!sd_mounted()) return false;
#endif
  bool rec_dir_ok = sd_ensure_rec_dir();
  s_final_path = chunk_name();
  s_tmp_path = s_final_path + REC_TMP_EXT;
  if (!sd_lock(2000)) {
    Serial.printf("REC open lock fail %s mounted=%d rec_dir_ok=%d\n", s_tmp_path.c_str(), sd_mounted(), rec_dir_ok);
    return false;
  }
  {
    bool exists_rec = SD.exists(REC_DIR);
    bool exists_tmp = SD.exists(s_tmp_path);
    bool exists_final = SD.exists(s_final_path);
    Serial.printf("REC open try %s rec_dir=%d tmp_exists=%d final_exists=%d mounted=%d\n", s_tmp_path.c_str(), exists_rec, exists_tmp, exists_final, sd_mounted());
  }
  s_file = SD.open(s_tmp_path, FILE_WRITE);
  if (!s_file) {
    Serial.printf("REC open SD.open fail %s mounted=%d\n", s_tmp_path.c_str(), sd_mounted());
    bool exists_rec2 = SD.exists(REC_DIR);
    Serial.printf("REC diag rec_dir exists=%d\n", exists_rec2);
    File root = SD.open(REC_DIR);
    if (!root) {
      Serial.println("REC diag SD.open(REC_DIR) fail - FS not accessible");
    } else {
      Serial.println("REC diag listing /rec:");
      File e = root.openNextFile();
      int cnt = 0;
      while (e && cnt < 10) {
        Serial.printf("  entry %s size %u dir %d\n", e.name(), (unsigned)e.size(), e.isDirectory());
        e.close();
        e = root.openNextFile();
        cnt++;
      }
      if (cnt == 0) Serial.println("  (empty)");
      root.close();
    }
    {
      String test = String(REC_DIR) + "/_test.tmp";
      File tf = SD.open(test, FILE_WRITE);
      if (tf) {
        tf.write((uint8_t*)"test", 4);
        tf.close();
        bool ok = SD.exists(test);
        Serial.printf("REC diag test write %s -> %d exists %d\n", test.c_str(), 1, ok);
        if (ok) SD.remove(test);
      } else {
        Serial.printf("REC diag test write fail %s\n", test.c_str());
      }
    }
    sd_unlock();
    return false;
  }
#if REC_CODEC_OPUS
  // OGG-Opus BOS: OpusHead + OpusTags
  s_opus_serial = (s_boot_id << 16) ^ s_file_seq ^ (uint32_t)millis();
  s_opus_seq = 1; // 0 is BOS
  s_opus_granule = 0;
  s_opus_frames = 0;
  s_i2s_frames = 0;
  s_pcm_bytes_total = 0;
  size_t bos = ogg_write_bos(s_file, s_opus_serial);
  s_file.flush();
  sd_unlock();
  s_file_seq++;
  s_bytes_in_chunk = bos;
  s_chunk_start_ms = millis();
  s_current = s_tmp_path;
  s_encode_time_us_sum = 0;
  s_encode_frames = 0;
  Serial.printf("REC open OGG %s seq %lu boot %lu bos %u bitrate %d\n", s_tmp_path.c_str(), (unsigned long)(s_file_seq-1), (unsigned long)s_boot_id, (unsigned)bos, REC_OPUS_BITRATE);
  return true;
#else
  uint8_t hdr[44] = {0};
  s_file.write(hdr, 44);
  s_file.flush();
  sd_unlock();
  s_file_seq++;
  s_bytes_in_chunk = 0;
  s_chunk_start_ms = millis();
  s_current = s_tmp_path;
  Serial.printf("REC open %s seq %lu boot %lu\n", s_tmp_path.c_str(), (unsigned long)(s_file_seq-1), (unsigned long)s_boot_id);
  return true;
#endif
}

static void close_chunk(bool keep) {
  if (!s_file) {
    Serial.println("REC close: no file");
    return;
  }
  Serial.printf("REC close keep=%d tmp=%s final=%s bytes=%lu frames=%lu\n", keep, s_tmp_path.c_str(), s_final_path.c_str(), (unsigned long)s_bytes_in_chunk, (unsigned long)s_opus_frames);
  if (!sd_lock(3000)) {
    Serial.println("REC close: sd_lock 3000 failed, force close");
    s_file.close();
    s_current = "";
    s_bytes_in_chunk = 0;
    return;
  }
#if REC_CODEC_OPUS
  // Flush any remaining PCM in s_ring through encode before EOS
  // Drain opus_ring first (already encoded)
  // Drain opus_ring first (already encoded). Items are length-prefixed raw
  // Opus frames (2B LE len + opus bytes) from opus_encode_task -> must be
  // muxed to OGG via write_opus_sliced (granule/seq/CRC). Writing them raw
  // corrupts the tail and can truncate duration in strict parsers (VLC).
  if (s_opus_ring) {
    size_t item_sz = 0;
    uint8_t *item = nullptr;
    while ((item = (uint8_t*)xRingbufferReceive(s_opus_ring, &item_sz, 0)) != nullptr) {
      if (item_sz >= 2) {
        uint16_t olen = item[0] | (item[1] << 8);
        if (olen + 2 <= item_sz && olen <= REC_OPUS_MAX_FRAME_BYTES && s_file) {
          bool fs_lost = false;
          if (!write_opus_sliced(item + 2, olen, &fs_lost)) {
            s_dropped_bytes += item_sz;
            s_drop_events++;
          }
          if (fs_lost) {
            vRingbufferReturnItem(s_opus_ring, item);
            Serial.println("REC close: FS lost during opus drain");
            break;
          }
        }
      }
      vRingbufferReturnItem(s_opus_ring, item);
    }
  }
  // Drain remaining PCM ring: encode leftover with padding
  // NOTE: pcm/out live on the heap, not the task stack — they stay live
  // across the ~20KB-deep opus_encode() call, so 640B+80B on-stack here was
  // part of the canary overflow. opus_encode is serialized via s_enc_mutex
  // (see opus_encode_task) because the encoder instance is not thread-safe.
  if (s_ring) {
    size_t item_sz = 0;
    uint8_t *item = nullptr;
    short *drain_pcm = (short*)heap_caps_malloc(REC_OPUS_SAMPLES_PER_FRAME*sizeof(short), MALLOC_CAP_8BIT);
    unsigned char *drain_out = (unsigned char*)heap_caps_malloc(REC_OPUS_MAX_FRAME_BYTES, MALLOC_CAP_8BIT);
    if (!drain_pcm) drain_pcm = (short*)malloc(REC_OPUS_SAMPLES_PER_FRAME*sizeof(short));
    if (!drain_out) drain_out = (unsigned char*)malloc(REC_OPUS_MAX_FRAME_BYTES);
    // Collect leftover PCM bytes
    while ((item = (uint8_t*)xRingbufferReceive(s_ring, &item_sz, 0)) != nullptr) {
      // item_sz is multiple of 2 (PCM). Encode in 640B chunks
      size_t offset = 0;
      while (offset + REC_OPUS_SAMPLES_PER_FRAME*2 <= item_sz) {
        int n = 0;
        if (drain_pcm && drain_out) {
          memcpy(drain_pcm, item+offset, REC_OPUS_SAMPLES_PER_FRAME*2);
          int64_t t0 = esp_timer_get_time();
          if (enc_lock()) {
            n = opus_encode(s_encoder, drain_pcm, REC_OPUS_SAMPLES_PER_FRAME, drain_out, REC_OPUS_MAX_FRAME_BYTES);
            enc_unlock();
          }
          int64_t dt = esp_timer_get_time() - t0;
          s_encode_time_us_sum += dt; s_encode_frames++;
          if (n > 0) {
            s_opus_granule += 960; // 20ms @48k
            size_t wr = ogg_write_audio_frame(s_file, drain_out, n, s_opus_granule, s_opus_seq++, s_opus_serial);
            s_bytes_in_chunk += wr;
            s_opus_frames++;
            if (dt > 8000) Serial.printf("REC encode slow %lldus frame %lu\n", dt, (unsigned long)s_opus_frames);
          }
        }
        offset += REC_OPUS_SAMPLES_PER_FRAME*2;
      }
      // leftover <640B pad with zeros
      size_t remain = item_sz - offset;
      if (remain > 0 && drain_pcm && drain_out) {
        memset(drain_pcm, 0, REC_OPUS_SAMPLES_PER_FRAME*sizeof(short));
        memcpy(drain_pcm, item+offset, remain);
        int n = 0;
        if (enc_lock()) {
          n = opus_encode(s_encoder, drain_pcm, REC_OPUS_SAMPLES_PER_FRAME, drain_out, REC_OPUS_MAX_FRAME_BYTES);
          enc_unlock();
        }
        if (n > 0) {
          s_opus_granule += 960;
          size_t wr = ogg_write_audio_frame(s_file, drain_out, n, s_opus_granule, s_opus_seq++, s_opus_serial);
          s_bytes_in_chunk += wr;
          s_opus_frames++;
        }
      }
      vRingbufferReturnItem(s_ring, item);
    }
    if (drain_pcm) heap_caps_free(drain_pcm);
    if (drain_out) heap_caps_free(drain_out);
  }
  // EOS page
  ogg_write_eos(s_file, s_opus_granule, s_opus_seq++, s_opus_serial);
  s_bytes_in_chunk += 27; // EOS size
  if (s_encode_frames>0) {
    uint32_t avg = s_encode_time_us_sum / s_encode_frames;
    Serial.printf("REC opus avg encode %luus frames %lu bitrate %d granule %llu\n", (unsigned long)avg, (unsigned long)s_encode_frames, REC_OPUS_BITRATE, s_opus_granule);
  }
  s_file.flush();
  s_file.close();
  sd_unlock();
  vTaskDelay(pdMS_TO_TICKS(400));
#else
  write_wav_header(s_file, s_bytes_in_chunk);
  s_file.flush();
  s_file.close();
  sd_unlock();
  vTaskDelay(pdMS_TO_TICKS(400));
#endif
  if (!keep) {
    Serial.printf("REC discard keep=0 remove %s\n", s_tmp_path.c_str());
    if (sd_lock(1000)) { SD.remove(s_tmp_path); sd_unlock(); }
  } else {
#if REC_CODEC_OPUS
    if (s_bytes_in_chunk < 512) {
      Serial.printf("REC discard small %lu <512 %s\n", (unsigned long)s_bytes_in_chunk, s_tmp_path.c_str());
      if (sd_lock(1000)) { SD.remove(s_tmp_path); sd_unlock(); }
    } else {
#else
    if (s_bytes_in_chunk < 1024) {
      Serial.printf("REC discard small %lu <1024 %s\n", (unsigned long)s_bytes_in_chunk, s_tmp_path.c_str());
      if (sd_lock(1000)) { SD.remove(s_tmp_path); sd_unlock(); }
    } else {
#endif
      bool fs_ok = false;
      bool tmp_ok = false;
      bool renamed = false;
      if (sd_lock(2000)) {
        int card_type = SD.cardType();
        fs_ok = SD.exists(REC_DIR);
        tmp_ok = SD.exists(s_tmp_path);
        Serial.printf("REC postclose cardType=%d rec_dir=%d tmp=%d\n", card_type, fs_ok, tmp_ok);
        if (fs_ok && tmp_ok) {
          if (SD.exists(s_final_path)) SD.remove(s_final_path);
          renamed = SD.rename(s_tmp_path, s_final_path);
        }
        Serial.printf("REC rename result=%d rec_dir=%d tmp=%d final=%d\n", renamed, (int)SD.exists(REC_DIR), (int)SD.exists(s_tmp_path), (int)SD.exists(s_final_path));
        sd_unlock();
      } else {
        Serial.println("REC postclose sd_lock 2000 failed");
      }
      if (!renamed && (!fs_ok || !tmp_ok)) {
        Serial.println("REC FS lost before rename, attempting remount + retry");
        sd_end();
        vTaskDelay(pdMS_TO_TICKS(300));
        if (sd_begin()) {
          Serial.printf("REC remount after FS lost -> OK cardType %d rec_dir %d\n", (int)SD.cardType(), (int)SD.exists(REC_DIR));
          if (sd_lock(2000)) {
            if (SD.exists(REC_DIR) && SD.exists(s_tmp_path)) {
              if (SD.exists(s_final_path)) SD.remove(s_final_path);
              renamed = SD.rename(s_tmp_path, s_final_path);
              Serial.printf("REC retry rename result=%d\n", renamed);
            }
            sd_unlock();
          }
        } else {
          Serial.println("REC remount after FS lost FAILED");
        }
      }
      if (!renamed) {
        Serial.printf("REC rename failed %s tmp=%s (preserved for recovery)\n", s_final_path.c_str(), s_tmp_path.c_str());
        s_current = "";
        s_bytes_in_chunk = 0;
        if (sd_mounted()) {
          sd_end();
        }
        vTaskDelay(pdMS_TO_TICKS(500));
        return;
      }
      Serial.printf("REC rename OK %s -> %s bytes %lu\n", s_tmp_path.c_str(), s_final_path.c_str(), (unsigned long)s_bytes_in_chunk);
      vTaskDelay(pdMS_TO_TICKS(300));
      s_chunks++;
      uint32_t total_size = s_bytes_in_chunk;
#if !REC_CODEC_OPUS
      total_size = s_bytes_in_chunk + 44;
#endif
      bool m_ok = manifest_add_file(s_final_path, total_size);
      Serial.printf("REC manifest add %s %s pending %u\n",
                    s_final_path.c_str(),
                    m_ok ? "OK" : "FAIL",
                    (unsigned)manifest_pending_count());
    }
  }
  s_current = "";
  s_bytes_in_chunk = 0;
  s_opus_frames = 0;
}

bool recorder_init() {
  if (s_boot_id == 0) {
    s_boot_id = load_boot_id();
  }
  if (!s_mutex) s_mutex = xSemaphoreCreateMutex();
  if (!s_enc_mutex) s_enc_mutex = xSemaphoreCreateMutex();
  if (!s_ring) {
    s_ring = xRingbufferCreateWithCaps(REC_RING_BYTES, RINGBUF_TYPE_BYTEBUF, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_ring) s_ring = xRingbufferCreate(REC_RING_BYTES, RINGBUF_TYPE_BYTEBUF);
  }
  if (!s_opus_ring) {
    s_opus_ring = xRingbufferCreateWithCaps(REC_OPUS_RING_BYTES, RINGBUF_TYPE_BYTEBUF, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_opus_ring) s_opus_ring = xRingbufferCreate(REC_OPUS_RING_BYTES, RINGBUF_TYPE_BYTEBUF);
  }
#if REC_CODEC_OPUS
  if (!s_encoder) {
    int err = 0;
    s_encoder = opus_encoder_create(REC_SAMPLE_RATE, REC_CHANNELS, OPUS_APPLICATION_VOIP, &err);
    if (s_encoder) {
      opus_codec_init_defaults(s_encoder);
      // Explicit calls for grep compat
      opus_encoder_ctl(s_encoder, OPUS_SET_BITRATE(REC_OPUS_BITRATE));
      opus_encoder_ctl(s_encoder, OPUS_SET_COMPLEXITY(0));
      opus_encoder_ctl(s_encoder, OPUS_SET_VBR(0));
      Serial.printf("REC opus encoder OK bitrate %d err %d\n", REC_OPUS_BITRATE, err);
    } else {
      Serial.printf("REC opus encoder FAIL err %d\n", err);
    }
  }
#endif
#if VAD_ENABLE
  {
    struct VadConfig vc;
    vc.mode = VAD_MODE;
    vc.onset_ms = VAD_ONSET_MS;
    vc.hangover_ms = VAD_HANGOVER_MS;
    vc.session_extend_ms = VAD_SESSION_EXTEND_MS;
    vc.min_speech_ms = VAD_MIN_SPEECH_MS;
    vc.preroll_ms = VAD_PREROLL_MS;
    vc.abs_floor_dbfs = VAD_ABS_FLOOR_DBFS;
    vad_configure(&vc);
    Serial.printf("REC VAD ON mode %d onset %dms hang %dms sess +%dms min %dms preroll %dms\n",
                  VAD_MODE, VAD_ONSET_MS, VAD_HANGOVER_MS,
                  VAD_SESSION_EXTEND_MS, VAD_MIN_SPEECH_MS, VAD_PREROLL_MS);
  }
  if (!s_preroll) {
    s_preroll_frames = vad_preroll_frames_cfg();
    size_t bytes = (size_t)s_preroll_frames * REC_OPUS_SAMPLES_PER_FRAME * sizeof(int16_t);
    s_preroll = (int16_t*)heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_preroll) s_preroll = (int16_t*)malloc(bytes);
    if (s_preroll) {
      memset(s_preroll, 0, bytes);
      Serial.printf("REC VAD pre-roll %lu frames (%luB)\n",
                    (unsigned long)s_preroll_frames, (unsigned long)bytes);
    } else {
      s_preroll_frames = 0;
      Serial.println("REC VAD pre-roll alloc FAIL, continuing without");
    }
    s_preroll_head = 0;
    s_preroll_count = 0;
  }
  s_vad_open_req = false;
  s_vad_close_req = false;
  s_vad_utt_frames = 0;
#else
  Serial.println("REC VAD OFF (continuous chunks)");
#endif
  i2s_config_t cfg = {};
  cfg.mode = (i2s_mode_t)(I2S_MODE_MASTER | I2S_MODE_RX);
  cfg.sample_rate = REC_SAMPLE_RATE;
  cfg.bits_per_sample = I2S_BITS_PER_SAMPLE_32BIT;
  // STEREO on purpose, mono in software: legacy driver computes
  // BCLK = SR * bits * chan. With ONLY_LEFT (chan=1) BCLK=512k and the
  // Philips hardware still divides by 64 slots -> WS runs at ~8k, i.e. every
  // recording plays ~2-3x fast (50s wall -> ~15-25s file). With RIGHT_LEFT
  // (chan=2) BCLK=1024k -> WS=16k exact. L/R=GND so LEFT slot is the mic;
  // we keep LEFT and drop RIGHT in recorder_task. Costs 2x DMA bandwidth.
  cfg.channel_format = I2S_CHANNEL_FMT_RIGHT_LEFT;
  cfg.communication_format = I2S_COMM_FORMAT_STAND_I2S;
  cfg.intr_alloc_flags = ESP_INTR_FLAG_LEVEL1;
  cfg.dma_buf_count = REC_I2S_DMA_BUF_COUNT;
  cfg.dma_buf_len = REC_I2S_DMA_BUF_LEN;
  cfg.use_apll = false;
  cfg.tx_desc_auto_clear = false;
  cfg.fixed_mclk = 0;
  i2s_pin_config_t pins = {};
  pins.bck_io_num = HW_I2S_BCLK_GPIO;
  pins.ws_io_num = HW_I2S_WS_GPIO;
  pins.data_out_num = I2S_PIN_NO_CHANGE;
  pins.data_in_num = HW_I2S_DIN_GPIO;
  esp_err_t err = i2s_driver_install(REC_I2S_PORT, &cfg, 0, nullptr);
  if (err != ESP_OK) return false;
  err = i2s_set_pin(REC_I2S_PORT, &pins);
  if (err != ESP_OK) return false;
  i2s_zero_dma_buffer(REC_I2S_PORT);
  return true;
}

bool recorder_start() {
  if (s_task) {
    Serial.println("REC start: already running, ignoring");
    return true;
  }
  s_recording = true;
  s_bookmark = false;
#if VAD_ENABLE
  // Fresh unmute/remount: clear stale onset/offset requests and filter state
  // (config from recorder_init is kept).
  vad_reset();
  s_vad_open_req = false;
  s_vad_close_req = false;
  s_vad_discard_req = false;
  s_vad_utt_frames = 0;
  s_preroll_head = 0;
  s_preroll_count = 0;
#endif
  Serial.printf("REC start: creating recorder task at uptime %lu ms\n", (unsigned long)millis());
  BaseType_t r = xTaskCreatePinnedToCore(recorder_task, "recorder", TASK_STACK_RECORDER, nullptr, TASK_PRIO_RECORDER, &s_task, 1);
  if (r != pdPASS) {
    Serial.printf("REC start: FAILED recorder r=%d\n", r);
    s_recording = false;
    return false;
  }
#if REC_CODEC_OPUS
  BaseType_t r2 = xTaskCreatePinnedToCore(opus_encode_task, "opus_enc", TASK_STACK_ENCODER, nullptr, TASK_PRIO_ENCODER, &s_encode_task, 1);
  if (r2 != pdPASS) {
    Serial.printf("REC start: FAILED opus_enc r=%d\n", r2);
    // keep recorder running even if encode fails (fallback PCM)
  } else {
    Serial.println("REC start: opus encode task OK");
  }
#endif
  Serial.println("REC start: OK - mic ON, recording resumed");
  return r == pdPASS;
}

void recorder_stop() {
  if (!s_recording && !s_task) {
    Serial.println("REC stop: already stopped, ignoring");
    return;
  }
  Serial.printf("REC stop: stopping recorder at uptime %lu ms file=%s bytes=%lu\n", (unsigned long)millis(), s_current.c_str(), (unsigned long)s_bytes_in_chunk);
  s_recording = false;
  if (s_task) {
    vTaskDelay(pdMS_TO_TICKS(300));
    s_task = nullptr;
    Serial.println("REC stop: task stopped - mic OFF, BLE sync still active");
  }
  if (s_encode_task) {
    vTaskDelay(pdMS_TO_TICKS(100));
    s_encode_task = nullptr;
  }
#if VAD_ENABLE
  s_vad_open_req = false;
  s_vad_close_req = false;
  s_vad_discard_req = false;
  s_vad_utt_frames = 0;
#endif
  if (!s_task) Serial.println("REC stop: no task handle, mic OFF");
}

bool recorder_is_recording() { return s_recording && s_task; }
uint32_t recorder_chunks_written() { return s_chunks; }
String recorder_current_file() { return s_current; }
void recorder_notify_bookmark() {
  Serial.println("REC bookmark: ignored (feature removed, mic toggle active)");
}
uint32_t recorder_dropped_bytes() { return s_dropped_bytes; }
uint32_t recorder_drop_events() { return s_drop_events; }
bool recorder_vad_active() {
#if VAD_ENABLE
  return s_file && vad_should_record();
#else
  return recorder_is_recording();
#endif
}
bool recorder_vad_speech() {
#if VAD_ENABLE
  return vad_is_speech();
#else
  return recorder_is_recording();
#endif
}
float recorder_vad_level_dbfs() {
#if VAD_ENABLE
  return vad_level_dbfs();
#else
  return 0.0f;
#endif
}
uint32_t recorder_vad_utterances() {
#if VAD_ENABLE
  return vad_utterances();
#else
  return s_chunks;
#endif
}

static bool write_audio_sliced(const uint8_t *data, size_t len, size_t *written_out, bool *fs_lost_out) {
  if (fs_lost_out) *fs_lost_out = false;
  if (written_out) *written_out = 0;
  if (!s_file || len == 0) return false;
  const size_t SLICE_SIZE = 1024;
  size_t written = 0;
  while (written < len) {
    size_t to_write = (len - written > SLICE_SIZE) ? SLICE_SIZE : (len - written);
    if (!sd_lock(500)) {
      Serial.println("REC write slice lock timeout");
      if (written_out) *written_out = written;
      return false;
    }
    size_t w = s_file.write(data + written, to_write);
    if (w == to_write) {
      s_bytes_in_chunk += w;
      written += w;
      sd_unlock();
      vTaskDelay(pdMS_TO_TICKS(4));
    } else {
      int ct = SD.cardType();
      bool rec_ok = SD.exists(REC_DIR);
      Serial.printf("REC WRITE FAIL requested=%u wrote=%u bytes=%lu cardType=%d rec=%d\n",
                    (unsigned)to_write, (unsigned)w, (unsigned long)s_bytes_in_chunk,
                    ct, (int)rec_ok);
      if (!rec_ok || ct == CARD_NONE) {
        if (fs_lost_out) *fs_lost_out = true;
      }
      sd_unlock();
      if (written_out) *written_out = written;
      return false;
    }
  }
  if (written_out) *written_out = written;
  return true;
}

// write_opus_sliced for OGG pages (same as sliced but tracks OGG bytes)
static bool write_opus_sliced(const uint8_t *opus, size_t olen, bool *fs_lost_out) {
  if (!s_file || olen==0) return false;
  if (fs_lost_out) *fs_lost_out = false;
  uint64_t granule = s_opus_granule + 960;
  // Build OGG page in RAM then slice write
  uint8_t page[350];
  const uint8_t *pkts[1]={opus};
  size_t lens[1]={olen};
  size_t plen = ogg_build_page(page, granule, s_opus_seq, 0x00, pkts, lens, 1, s_opus_serial);
  bool ok = write_audio_sliced(page, plen, nullptr, fs_lost_out);
  if (ok) { s_opus_granule = granule; s_opus_seq++; s_opus_frames++; }
  return ok;
}

void recorder_task(void *arg) {
  (void)arg;
  size_t bytes_per_read = 1024;
  uint8_t *raw = (uint8_t *)heap_caps_malloc(bytes_per_read * 4, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  uint8_t *pcm = (uint8_t *)heap_caps_malloc(bytes_per_read * 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!raw) raw = (uint8_t *)malloc(bytes_per_read * 4);
  if (!pcm) pcm = (uint8_t *)malloc(bytes_per_read * 2);
  if (!raw || !pcm) {
    vTaskDelete(nullptr);
    return;
  }
  // VAD mode starts fileless (mic ON, listening, no SD file) — first ONSET
  // opens the chunk. Legacy mode opens immediately as before.
#if VAD_ENABLE
  ui_signal_vad_listening();
  Serial.println("REC VAD listening (fileless until onset)");
#else
  if (!open_chunk()) {
    vTaskDelay(pdMS_TO_TICKS(1000));
    if (!open_chunk()) {
      ui_signal_error();
    }
  }
#endif
  while (s_recording) {
#if HW_HAS_SD_DETECT
    if (!sd_present() || !sd_mounted()) {
#else
    if (!sd_mounted()) {
#endif
      if (s_file) {
        Serial.printf("REC SD lost, preserving tmp %s bytes %lu\n", s_tmp_path.c_str(), (unsigned long)s_bytes_in_chunk);
        s_file.close();
        s_current = "";
        s_bytes_in_chunk = 0;
      }
      ui_signal_error();
      vTaskDelay(pdMS_TO_TICKS(200));
      continue;
    }
    if (!s_file) {
#if VAD_ENABLE
      // Fileless listening: VAD (opus_encode_task) owns the open decision.
      // Keep I2S->s_ring flowing below so VAD stays fed; skip SD work here.
      // Retry while in session so a transient open failure mid-utterance
      // doesn't strand encoded pre-roll in opus_ring until the next onset.
      if (s_vad_open_req || (vad_should_record() && !s_file)) {
        s_vad_open_req = false;
        if (!open_chunk()) {
          ui_signal_error();
          vTaskDelay(pdMS_TO_TICKS(200));
        } else {
          // NOTE: s_vad_utt_frames already counts the flushed pre-roll
          // (zeroed at ONSET before flush) — do not reset here.
          ui_signal_recording(true);
          Serial.printf("REC VAD onset open %s\n", s_tmp_path.c_str());
        }
      }
#else
      if (!open_chunk()) {
        ui_signal_error();
        vTaskDelay(pdMS_TO_TICKS(200));
        continue;
      }
#endif
    }
    size_t bytes_read = 0;
    esp_err_t err = i2s_read(REC_I2S_PORT, raw, bytes_per_read * 4, &bytes_read, pdMS_TO_TICKS(100));
    if (err == ESP_OK && bytes_read > 0) {
      // STEREO frame from driver: [L0(4B) R0(4B) L1 R1 ...], L/R=GND => LEFT is mic.
      // bytes_read is a multiple of 8; keep LEFT only -> true 16k mono.
      size_t frames = bytes_read / 8;
      size_t odd = bytes_read % 8;
      if (odd) bytes_read -= odd; // keep frame alignment (drop trailing partial)
      for (size_t i = 0; i < frames; i++) {
        int32_t s32 = ((int32_t)raw[i * 8 + 3] << 24) | ((int32_t)raw[i * 8 + 2] << 16) | ((int32_t)raw[i * 8 + 1] << 8) | raw[i * 8];
        s32 >>= 8;
        int16_t s16 = (int16_t)(s32 >> 8);
        pcm[i * 2] = s16 & 0xFF;
        pcm[i * 2 + 1] = (s16 >> 8) & 0xFF;
        s_i2s_frames++;
      }
      size_t pcm_bytes = frames * 2;
      s_pcm_bytes_total += pcm_bytes;
      BaseType_t sent = xRingbufferSend(s_ring, pcm, pcm_bytes, pdMS_TO_TICKS(10));
      if (sent != pdTRUE) {
        s_dropped_bytes += pcm_bytes;
        s_drop_events++;
        if ((s_drop_events % 10) == 1) {
          Serial.printf("REC drop %luB events %lu\n",
                        (unsigned long)s_dropped_bytes,
                        (unsigned long)s_drop_events);
        }
        if (s_dropped_bytes >= 8192 || s_drop_events >= 3) {
          ui_signal_error();
        }
      }
    } else if (err != ESP_OK) {
      static uint32_t last_i2s_err = 0;
      if (millis() - last_i2s_err >= 5000) {
        last_i2s_err = millis();
        Serial.printf("REC i2s_read err %d bytes %u\n", err, (unsigned)bytes_read);
      }
    }
#if REC_CODEC_OPUS
    // drain opus_ring to SD as OGG pages (12/loop ~= 240fps capacity, need 50)
    for (int drain_try = 0; drain_try < 12; drain_try++) {
#if VAD_ENABLE
      // Fileless listening: leave encoded pre-roll buffered in opus_ring
      // until ONSET opens the chunk. Draining here would discard it.
      if (!s_file) break;
#endif
      size_t item_sz = 0;
      uint8_t *item = (uint8_t *)xRingbufferReceive(s_opus_ring, &item_sz, 0);
      if (!item) break;
      if (s_file && item_sz >= 2) {
        uint16_t olen = item[0] | (item[1]<<8);
        if (olen + 2 <= item_sz && olen <= REC_OPUS_MAX_FRAME_BYTES) {
          bool fs_lost=false;
          bool ok = write_opus_sliced(item+2, olen, &fs_lost);
          if (fs_lost) {
            Serial.println("REC FS lost detected, preserving tmp and unmounting");
            if (s_file) { s_file.close(); s_current=""; }
            sd_end();
            ui_signal_error();
          }
          if (!ok) {
            size_t unwritten = item_sz;
            s_dropped_bytes += unwritten;
            s_drop_events++;
          }
        }
      }
      vRingbufferReturnItem(s_opus_ring, item);
    }
#else
    // WAV: drain PCM ring to SD via sliced writes (original path)
    for (int drain_try = 0; drain_try < 2; drain_try++) {
      size_t item_sz = 0;
      uint8_t *item = (uint8_t *)xRingbufferReceive(s_ring, &item_sz, 0);
      if (!item) break;
      if (s_file && item_sz) {
        size_t written = 0;
        bool fs_lost = false;
        bool ok = write_audio_sliced(item, item_sz, &written, &fs_lost);
        if (fs_lost) {
          Serial.println("REC FS lost detected, preserving tmp and unmounting");
          if (s_file) { s_file.close(); s_current=""; }
          sd_end();
          ui_signal_error();
        }
        if (!ok) {
          size_t unwritten = item_sz - written;
          if (unwritten > 0) {
            uint8_t *copy = (uint8_t *)heap_caps_malloc(unwritten, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
            if (!copy) copy = (uint8_t *)heap_caps_malloc(unwritten, MALLOC_CAP_8BIT);
            bool use_heap_caps = (copy != nullptr);
            if (!copy) copy = (uint8_t *)malloc(unwritten);
            if (copy) {
              memcpy(copy, item + written, unwritten);
              vRingbufferReturnItem(s_ring, item);
              BaseType_t sent2 = xRingbufferSend(s_ring, copy, unwritten, 0);
              if (use_heap_caps) heap_caps_free(copy); else free(copy);
              if (sent2 != pdTRUE) { s_dropped_bytes += unwritten; s_drop_events++; }
            } else {
              vRingbufferReturnItem(s_ring, item);
              s_dropped_bytes += unwritten; s_drop_events++;
            }
          } else {
            vRingbufferReturnItem(s_ring, item);
          }
          break;
        }
      }
      vRingbufferReturnItem(s_ring, item);
    }
#endif
    if (s_bookmark) {
      s_bookmark = false;
      ui_signal_bookmark();
    }
    {
      static uint32_t last_dbg = 0;
      if (millis() - last_dbg >= 5000) {
        last_dbg = millis();
        uint32_t avg = s_encode_frames? (uint32_t)(s_encode_time_us_sum / s_encode_frames):0;
        UBaseType_t rec_hw = uxTaskGetStackHighWaterMark(nullptr);
        UBaseType_t enc_hw = s_encode_task ? uxTaskGetStackHighWaterMark(s_encode_task) : 0;
        uint32_t elapsed = millis() - s_chunk_start_ms;
        uint32_t exp_frames = elapsed / REC_OPUS_FRAME_MS;
        uint32_t i2s_hz = elapsed ? (uint32_t)((uint64_t)s_i2s_frames * 1000 / elapsed) : 0;
        Serial.printf("REC dbg file=%s bytes=%lu elapsed=%lu ms i2s=%luHz exp_frames=%lu opus_frames=%lu drop=%lu events=%lu pending=%u avg_enc=%luus stack_rec=%u stack_enc=%u"
#if VAD_ENABLE
                      " vad_st=%d vad_lvl=%.1f vad_mod=%.1f utt=%lu utt_fr=%lu"
#endif
                      "\n",
                      s_tmp_path.c_str(), (unsigned long)s_bytes_in_chunk,
                      (unsigned long)elapsed,
                      (unsigned long)i2s_hz, (unsigned long)exp_frames,
                      (unsigned long)s_opus_frames,
                      (unsigned long)s_dropped_bytes, (unsigned long)s_drop_events,
                      (unsigned)manifest_pending_count(), (unsigned long)avg,
                      (unsigned)rec_hw, (unsigned)enc_hw
#if VAD_ENABLE
                      , (int)vad_state(), vad_level_dbfs(), vad_mod_db(),
                      (unsigned long)vad_utterances(), (unsigned long)s_vad_utt_frames
#endif
                      );
      }
    }
#if VAD_ENABLE
    // VAD OFFSET: close the utterance file (or discard blips).
    if (s_vad_close_req && s_file) {
      s_vad_close_req = false;
      uint32_t min_fr = vad_min_speech_frames_cfg();
      bool too_short = (min_fr > 0 && s_vad_utt_frames < min_fr);
      Serial.printf("REC VAD offset close utt_frames=%lu min=%lu bytes=%lu\n",
                    (unsigned long)s_vad_utt_frames, (unsigned long)min_fr,
                    (unsigned long)s_bytes_in_chunk);
      // flush remaining opus_ring before closing (same as rotation path)
      size_t item_sz0 = 0;
      uint8_t *item0 = nullptr;
      while ((item0 = (uint8_t *)xRingbufferReceive(s_opus_ring, &item_sz0, 0)) != nullptr) {
        if (s_file && item_sz0>=2) {
          uint16_t olen = item0[0] | (item0[1]<<8);
          if (olen+2 <= item_sz0) write_opus_sliced(item0+2, olen, nullptr);
        }
        vRingbufferReturnItem(s_opus_ring, item0);
      }
      close_chunk(!too_short);
      s_vad_utt_frames = 0;
      ui_signal_vad_listening();
      // Do not reopen here: next ONSET will open. Skip rotation check below.
    } else if (s_vad_close_req && !s_file) {
      s_vad_close_req = false;
      s_vad_utt_frames = 0;
    }
    // VAD HUMDISCARD: sustained flat hum — purge buffered encoded frames
    // WITHOUT writing (unlike OFFSET/rotation flushes), then drop the tmp.
    if (s_vad_discard_req) {
      s_vad_discard_req = false;
      size_t dit_sz = 0;
      uint8_t *dit = nullptr;
      uint32_t purged = 0;
      while ((dit = (uint8_t *)xRingbufferReceive(s_opus_ring, &dit_sz, 0)) != nullptr) {
        purged++;
        vRingbufferReturnItem(s_opus_ring, dit);
      }
      Serial.printf("REC VAD discard purged=%lu tmp=%s\n",
                    (unsigned long)purged, s_tmp_path.c_str());
      if (s_file) close_chunk(false);
      s_vad_utt_frames = 0;
      ui_signal_vad_listening();
    }
#endif
    bool time_done = s_file && (millis() - s_chunk_start_ms) >= (REC_CHUNK_SEC * 1000UL);
    bool size_done = s_file && (s_bytes_in_chunk >= REC_CHUNK_BYTES);
    // For Opus, size threshold never hit (100KB <<1.6M), time_done drives rotation
    // Keep size_done for PCM fallback compat
    if (time_done || size_done) {
      Serial.printf("REC chunk done trigger time_done=%d size_done=%d bytes=%lu elapsed=%lu opus_frames=%lu\n",
                    time_done, size_done, (unsigned long)s_bytes_in_chunk, (unsigned long)(millis() - s_chunk_start_ms), (unsigned long)s_opus_frames);
#if REC_CODEC_OPUS
      // flush remaining opus_ring before closing
      size_t item_sz = 0;
      uint8_t *item = nullptr;
      while ((item = (uint8_t *)xRingbufferReceive(s_opus_ring, &item_sz, 0)) != nullptr) {
        if (s_file && item_sz>=2) {
          uint16_t olen = item[0] | (item[1]<<8);
          if (olen+2 <= item_sz) write_opus_sliced(item+2, olen, nullptr);
        }
        vRingbufferReturnItem(s_opus_ring, item);
      }
#else
      // WAV: flush remaining PCM ring before closing
      size_t item_sz = 0;
      uint8_t *item = nullptr;
      while ((item = (uint8_t *)xRingbufferReceive(s_ring, &item_sz, 0)) != nullptr) {
        if (s_file && item_sz) {
          size_t written=0; bool fs_lost=false;
          write_audio_sliced(item, item_sz, &written, &fs_lost);
        }
        vRingbufferReturnItem(s_ring, item);
      }
#endif
      // also flush any pending PCM via direct encode path (same as close_chunk handles)
      close_chunk(true);
#if VAD_ENABLE
      // 50s cap hit mid-utterance: keep capturing iff VAD still in session,
      // else fall back to fileless listening until the next onset.
      if (vad_should_record() && !s_vad_close_req && !s_vad_discard_req) {
        if (!open_chunk()) {
          ui_signal_error();
          vTaskDelay(pdMS_TO_TICKS(500));
        } else {
          // Utterance continues across the 50s rotation: keep the counter so
          // MIN_SPEECH accounting spans the whole utterance, not per file.
          ui_signal_recording(true);
        }
      } else {
        s_vad_utt_frames = 0;
        ui_signal_vad_listening();
      }
#else
      ui_signal_recording(s_recording);
      if (!open_chunk()) {
        ui_signal_error();
        vTaskDelay(pdMS_TO_TICKS(500));
      }
#endif
    }
    vTaskDelay(pdMS_TO_TICKS(1));
  }
  // final drain
#if REC_CODEC_OPUS
  size_t item_sz = 0;
  uint8_t *item = nullptr;
  while ((item = (uint8_t *)xRingbufferReceive(s_opus_ring, &item_sz, 0)) != nullptr) {
    if (s_file && item_sz>=2) {
      uint16_t olen = item[0] | (item[1]<<8);
      if (olen+2 <= item_sz) write_opus_sliced(item+2, olen, nullptr);
    }
    vRingbufferReturnItem(s_opus_ring, item);
  }
#else
  size_t item_sz = 0;
  uint8_t *item = nullptr;
  while ((item = (uint8_t *)xRingbufferReceive(s_ring, &item_sz, 0)) != nullptr) {
    if (s_file && item_sz) {
      size_t written=0; bool fs_lost=false;
      write_audio_sliced(item, item_sz, &written, &fs_lost);
    }
    vRingbufferReturnItem(s_ring, item);
  }
#endif
  close_chunk(true);
  free(raw);
  free(pcm);
  vTaskDelete(nullptr);
}

static void opus_encode_task(void *arg) {
  (void)arg;
  uint8_t *pcm_buf = (uint8_t*)heap_caps_malloc(REC_OPUS_SAMPLES_PER_FRAME*2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!pcm_buf) pcm_buf = (uint8_t*)malloc(REC_OPUS_SAMPLES_PER_FRAME*2);
  // NOTE: pcm_s16/frame_bytes/packet live on the heap, not the task stack.
  // They stay live across the ~20KB-deep opus_encode() call (SILK+CELT);
  // 640B+80B+82B of on-stack locals was part of the (opus_enc) canary trip.
  short *pcm_s16 = (short*)heap_caps_malloc(REC_OPUS_SAMPLES_PER_FRAME*sizeof(short), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!pcm_s16) pcm_s16 = (short*)malloc(REC_OPUS_SAMPLES_PER_FRAME*sizeof(short));
  uint8_t *frame_bytes = (uint8_t*)heap_caps_malloc(REC_OPUS_MAX_FRAME_BYTES, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!frame_bytes) frame_bytes = (uint8_t*)malloc(REC_OPUS_MAX_FRAME_BYTES);
  uint8_t *packet = (uint8_t*)heap_caps_malloc(REC_OPUS_MAX_FRAME_BYTES+2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!packet) packet = (uint8_t*)malloc(REC_OPUS_MAX_FRAME_BYTES+2);
  if (!pcm_buf || !pcm_s16 || !frame_bytes || !packet) {
    Serial.println("REC opus_enc: heap alloc fail, task exit");
    if (pcm_buf) heap_caps_free(pcm_buf);
    if (pcm_s16) heap_caps_free(pcm_s16);
    if (frame_bytes) heap_caps_free(frame_bytes);
    if (packet) heap_caps_free(packet);
    vTaskDelete(nullptr);
    return;
  }
  size_t pcm_filled = 0;
  while (s_recording) {
#if VAD_ENABLE
    if (!s_ring || !s_opus_ring) { vTaskDelay(pdMS_TO_TICKS(5)); continue; }
    // NOTE: no !s_file stall here — VAD must keep draining s_ring while
    // fileless so silence never overflows the PCM ring.
#else
    if (!s_ring || !s_opus_ring || !s_file) { vTaskDelay(pdMS_TO_TICKS(5)); continue; }
#endif
    // Ensure we have 640B PCM for one Opus frame
    while (pcm_filled < REC_OPUS_SAMPLES_PER_FRAME*2) {
      size_t item_sz = 0;
      uint8_t *item = (uint8_t*)xRingbufferReceive(s_ring, &item_sz, pdMS_TO_TICKS(10));
      if (!item) break;
      size_t copy = item_sz;
      if (pcm_filled + copy > REC_OPUS_SAMPLES_PER_FRAME*2) copy = REC_OPUS_SAMPLES_PER_FRAME*2 - pcm_filled;
      memcpy(pcm_buf + pcm_filled, item, copy);
      pcm_filled += copy;
      // If item larger than needed, requeue remainder
      if (copy < item_sz) {
        size_t remain = item_sz - copy;
        uint8_t *rem = (uint8_t*)heap_caps_malloc(remain, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!rem) rem = (uint8_t*)malloc(remain);
        if (rem) {
          memcpy(rem, item+copy, remain);
          vRingbufferReturnItem(s_ring, item);
          xRingbufferSend(s_ring, rem, remain, 0);
          if (rem) { heap_caps_free(rem); } else free(rem);
          break;
        }
      }
      vRingbufferReturnItem(s_ring, item);
      if (copy == 0) break;
    }
    if (pcm_filled < REC_OPUS_SAMPLES_PER_FRAME*2) {
      vTaskDelay(pdMS_TO_TICKS(2));
      continue;
    }
    // Have full frame
    for (int i=0;i<REC_OPUS_SAMPLES_PER_FRAME;i++) {
      pcm_s16[i] = (int16_t)(pcm_buf[i*2] | (pcm_buf[i*2+1]<<8));
    }
#if VAD_ENABLE
    // ---- VAD gate: run detection on every frame, encode only speech ----
    // Pre-roll always tracks the latest frames so ONSET can flush audio
    // from BEFORE the trigger (avoids first-syllable clipping).
    if (s_preroll && s_preroll_frames > 0) {
      memcpy(&s_preroll[s_preroll_head * REC_OPUS_SAMPLES_PER_FRAME], pcm_s16,
             REC_OPUS_SAMPLES_PER_FRAME * sizeof(int16_t));
      s_preroll_head = (s_preroll_head + 1) % s_preroll_frames;
      if (s_preroll_count < s_preroll_frames) s_preroll_count++;
    }
    {
      int voiced = vad_process_frame(pcm_s16, REC_OPUS_SAMPLES_PER_FRAME);
      if (voiced < 0) voiced = 0;
      enum VadEvent ev = vad_update(voiced);
      if (ev == VAD_EV_ONSET) {
        s_vad_utt_frames = 0;
        if (!s_file) s_vad_open_req = true;
        // Flush pre-roll oldest->newest, then the trigger frame itself.
        // opus_ring buffers them in order; recorder_task drains after open.
        if (s_preroll && s_preroll_count > 0) {
          uint32_t start = (s_preroll_head + s_preroll_frames - s_preroll_count) % s_preroll_frames;
          // Newest slot always holds the current frame (written above) —
          // skip it here, it is encoded once below.
          uint32_t newest = (s_preroll_head + s_preroll_frames - 1) % s_preroll_frames;
          for (uint32_t k = 0; k < s_preroll_count; k++) {
            uint32_t slot = (start + k) % s_preroll_frames;
            if (slot == newest) continue;
            vad_encode_push(&s_preroll[slot * REC_OPUS_SAMPLES_PER_FRAME],
                            frame_bytes, packet);
          }
        }
        vad_encode_push(pcm_s16, frame_bytes, packet);
        Serial.printf("REC VAD ONSET lvl=%.1fdB pre=%lu\n",
                      vad_level_dbfs(), (unsigned long)s_preroll_count);
      } else if (ev == VAD_EV_SPEECH) {
        vad_encode_push(pcm_s16, frame_bytes, packet);
      } else if (ev == VAD_EV_PAUSE) {
        Serial.printf("REC VAD PAUSE lvl=%.1fdB utt_fr=%lu (writes suspended, file kept)\n",
                      vad_level_dbfs(), (unsigned long)s_vad_utt_frames);
      } else if (ev == VAD_EV_OFFSET) {
        s_vad_close_req = true;
        Serial.printf("REC VAD OFFSET utt_fr=%lu\n", (unsigned long)s_vad_utt_frames);
      } else if (ev == VAD_EV_DISCARD) {
        // 2s spectrally-flat session (hum/tune): drop everything, keep nothing.
        s_vad_discard_req = true;
        Serial.printf("REC VAD HUMDISCARD utt_fr=%lu mod=%.1fdB\n",
                      (unsigned long)s_vad_utt_frames, vad_mod_db());
      } else {
        // VAD_EV_SILENCE: fileless idle or in-session pause — discard frame.
        // (s_ring already drained above, so no overflow. opus_encode skipped
        // = the main CPU saving of voice-triggered recording.)
      }
    }
#else
    int64_t t0 = esp_timer_get_time();
    int n = 0;
    if (enc_lock()) {
      n = opus_encode(s_encoder, pcm_s16, REC_OPUS_SAMPLES_PER_FRAME, frame_bytes, REC_OPUS_MAX_FRAME_BYTES);
      enc_unlock();
    }
    int64_t dt = esp_timer_get_time() - t0;
    s_encode_time_us_sum += dt; s_encode_frames++;
    if (n > 0 && n <= REC_OPUS_MAX_FRAME_BYTES) {
      // Push encoded frame to opus_ring with 2B len prefix for recorder drain? Or write OGG directly via sd_lock
      // For double-ring design, push to opus_ring as raw length-prefixed frame
      packet[0]= n & 0xFF; packet[1]=(n>>8)&0xFF;
      memcpy(packet+2, frame_bytes, n);
      BaseType_t sent = xRingbufferSend(s_opus_ring, packet, n+2, pdMS_TO_TICKS(10));
      if (sent != pdTRUE) {
        s_dropped_bytes += n;
        s_drop_events++;
      }
      if (dt > 8000) Serial.printf("REC encode slow %lldus n=%d frames %lu\n", dt, n, (unsigned long)s_encode_frames);
    }
#endif
    pcm_filled = 0;
  }
  if (pcm_buf) heap_caps_free(pcm_buf);
  if (pcm_s16) heap_caps_free(pcm_s16);
  if (frame_bytes) heap_caps_free(frame_bytes);
  if (packet) heap_caps_free(packet);
  vTaskDelete(nullptr);
}



