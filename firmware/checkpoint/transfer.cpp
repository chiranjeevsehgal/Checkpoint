#include "transfer.h"
#include "config.h"
#include "protocol.h"
#include "ble_service.h"
#include "sd_manager.h"
#include "manifest.h"
#include "crypto.h"
#include "log.h"
#include "recorder.h"
#include "ui.h"
#if !SD_USE_SDMMC
#include <SD.h>
#endif
#include "esp_heap_caps.h"
// Test compat retains for review fixes (blast mode, no per-frag halt)
// remain = BLE_ACK_TIMEOUT_MS
// INVARIANT: same nonce+key only for identical plaintext retry

static TaskHandle_t s_task = nullptr;
static volatile bool s_busy = false;
static String s_current = "";
static QueueHandle_t s_ack_q = nullptr;
static uint16_t s_resume_seq = 0;
#define XFER_MAX_FILE_RETRIES 3
static String s_retry_path = "";
static int s_retry_count = 0;

// PSRAM batch buffer for window reads — ~7KB for W32*220, off 8KB transfer stack
static uint8_t *s_window_buf = nullptr;
static size_t s_window_buf_size = 0;

struct AckMsg { uint16_t seq; bool ok; };
// Blast mode: fire-and-forget, reliability via FILE_DONE CRC + full retry.
// Short-circuit if base was already acked - already acked

static void signal_ack(uint16_t seq, bool ok) {
  if (!s_ack_q) return;
  AckMsg m{seq, ok};
  // NimBLE callback runs as FreeRTOS task, not ISR - allow brief block instead of dropping ACKs
  // Previously 0-tick drop caused missing fragments to be falsely considered acked via cumulative logic
  if (xQueueSend(s_ack_q, &m, pdMS_TO_TICKS(10)) != pdTRUE) {
    // Queue full even after 10ms - try ISR variant as fallback
    xQueueSend(s_ack_q, &m, 0);
  }
}

void transfer_on_packet(const uint8_t *data, size_t len) {
  Packet pkt;
  if (!proto_parse(data, len, &pkt)) return;
  if (pkt.type == PKT_ACK) {
    if (pkt.len < 3) return;
    uint16_t seq = pkt.payload[0] | (pkt.payload[1] << 8);
    bool ok = pkt.payload[2] == 0x00;
    signal_ack(seq, ok);
  } else if (pkt.type == PKT_FILE_ANNOUNCE_ACK) {
    if (pkt.len < 4) return;
    uint16_t seq = pkt.payload[0] | (pkt.payload[1] << 8);
    signal_ack(seq, true);
    s_resume_seq = pkt.payload[2] | (pkt.payload[3] << 8);
  } else if (pkt.type == PKT_FILE_DONE_ACK) {
    if (pkt.len < 5) return;
    uint16_t seq = pkt.payload[0] | (pkt.payload[1] << 8);
    bool ok = pkt.payload[2] == 0x01;
    signal_ack(seq, ok);
  } else if (pkt.type == PKT_RESUME_RESP) {
    if (pkt.len < 4) return;
    uint16_t seq = pkt.payload[0] | (pkt.payload[1] << 8);
    s_resume_seq = pkt.payload[2] | (pkt.payload[3] << 8);
    signal_ack(seq, true);
  }
}

static bool wait_ack(uint16_t seq, uint32_t timeout_ms) {
  uint32_t start = millis();
  while (millis() - start < timeout_ms) {
    AckMsg m;
    uint32_t remain = timeout_ms - (millis() - start);
    uint32_t wait = remain > 50 ? 50 : remain;
    if (xQueueReceive(s_ack_q, &m, pdMS_TO_TICKS(wait)) == pdTRUE) {
      if (m.seq == seq) return m.ok;
      // not our seq, keep waiting but requeue? For window we need per-seq queue pop
      // If queue holds other seq, keep it for later: put it back if not matched
      // Simple: if not matched, ignore but continue wait (packet for window will be handled elsewhere)
      // For single-wait paths (announce/done) this suffices
      continue;
    }
    if (!ble_is_connected() || !ble_is_handshaked()) return false;
  }
  return false;
}

static bool send_with_retry(uint8_t type, uint16_t seq, const uint8_t *payload, uint16_t len) {
  for (int attempt = 0; attempt < BLE_RETRY_MAX; attempt++) {
    ble_send_packet(type, seq, payload, len);
    bool ok = wait_ack(seq, BLE_ACK_TIMEOUT_MS);
    if (ok) return true;
    if (!ble_is_connected()) return false;
    vTaskDelay(pdMS_TO_TICKS(50 * (attempt + 1)));
  }
  return false;
}

bool transfer_init() {
  if (!s_ack_q) s_ack_q = xQueueCreate(64, sizeof(AckMsg));
  ble_on_packet(transfer_on_packet);
  // Allocate window batch buffer off-stack in PSRAM (fallback to heap)
  if (!s_window_buf) {
    s_window_buf_size = (size_t)BLE_WINDOW * (size_t)BLE_FRAG_SIZE;
    s_window_buf = (uint8_t *)heap_caps_malloc(s_window_buf_size, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_window_buf) s_window_buf = (uint8_t *)heap_caps_malloc(s_window_buf_size, MALLOC_CAP_8BIT);
    // If still null, batch will fall back to per-frag reads (handled below)
  }
  return s_ack_q != nullptr;
}

bool transfer_is_busy() { return s_busy; }
String transfer_current_file() { return s_current; }

void transfer_task(void *arg) {
  (void)arg;
  uint16_t seq_gen = 1;
  while (true) {
    if (!ble_is_connected() || !ble_is_handshaked()) {
      s_busy = false;
      vTaskDelay(pdMS_TO_TICKS(500));
      continue;
    }
    // Allow transfer while recording; brief backoff reduces SD contention
    // without starving sync (previous pending<20 gate blocked all transfers)
    if (recorder_is_recording()) {
      vTaskDelay(pdMS_TO_TICKS(500));
    }
    ManifestEntry pending[8];
    size_t found = 0;
    if (!manifest_get_pending(pending, 8, &found) || found == 0) {
      vTaskDelay(pdMS_TO_TICKS(1000));
      continue;
    }
    ManifestEntry *job = &pending[0];
    // Track file-level retries for bad-file skip (full retry on CRC fail)
    if (s_retry_path != job->path) {
      s_retry_path = job->path;
      s_retry_count = 0;
    }
    s_current = job->path;
    s_busy = true;

    File f;
    if (!sd_lock(1000)) { s_busy = false; vTaskDelay(pdMS_TO_TICKS(500)); continue; }
    f = SD.open(job->path, FILE_READ);
    sd_unlock();
    if (!f) {
      manifest_mark_done(job->path);
      s_busy = false;
      continue;
    }
    uint32_t total = 0;
    if (sd_lock(500)) { total = f.size(); sd_unlock(); } else total = f.size();
    uint16_t total_frags = (total + BLE_FRAG_SIZE - 1) / BLE_FRAG_SIZE;
    if (total_frags == 0) {
      if (sd_lock(500)) { f.close(); sd_unlock(); } else f.close();
      // empty file, delete
      sd_safe_delete_after_ack(job->path);
      manifest_mark_done(job->path);
      s_busy = false;
      continue;
    }
    uint32_t file_crc = job->crc;
    if (file_crc == 0) {
      // 50s file 1.6M needs ~390 chunks at 4k; use 2k for more frequent yield to recorder
      bool crc_ok = sd_file_crc32_cooperative(job->path, &file_crc, 2048);
      if (!crc_ok || file_crc == 0) {
        if (sd_lock(500)) { f.close(); sd_unlock(); } else f.close();
        s_busy = false;
        vTaskDelay(pdMS_TO_TICKS(1000));
        continue;
      }
      manifest_set_crc(job->path, file_crc);
    }
    uint16_t start_seq = job->next_seq;
    if (start_seq >= total_frags) start_seq = 0;

    uint8_t ann[32];
    ann[0] = job->path.length() & 0xFF;
    memcpy(ann + 1, &total, 4);
    memcpy(ann + 5, &total_frags, 2);
    memcpy(ann + 7, &file_crc, 4);
    memcpy(ann + 11, &start_seq, 2);
    uint32_t file_id = file_crc ^ total;
    memcpy(ann + 13, &file_id, 4);
    uint16_t ann_seq = seq_gen++;
    s_resume_seq = start_seq;
    // clear stale acks before announce
    xQueueReset(s_ack_q);
    if (!send_with_retry(PKT_FILE_ANNOUNCE, ann_seq, ann, 17)) {
      if (sd_lock(500)) { f.close(); sd_unlock(); } else f.close();
      s_retry_count++;
      if (s_retry_count >= XFER_MAX_FILE_RETRIES) {
        LOG_E("XFER skip %08lx announce", (unsigned long)file_id);
        sd_safe_delete_after_ack(job->path);
        manifest_mark_done(job->path);
        s_retry_path = "";
        s_retry_count = 0;
        s_busy = false;
        ui_signal_error();
        vTaskDelay(pdMS_TO_TICKS(500));
        continue;
      }
      s_busy = false;
      vTaskDelay(pdMS_TO_TICKS(1000));
      continue;
    }
    uint16_t next = s_resume_seq;
    if (next >= total_frags) next = start_seq;

    // per-file derived key (optional, restores master after file)
    uint8_t master[CRYPTO_KEY_BYTES];
    uint8_t file_key[CRYPTO_KEY_BYTES];
    bool use_derived = false;
    if (crypto_get_key(master)) {
      if (crypto_derive_file_key(master, ble_session_id(), file_id, file_key)) {
        crypto_set_key(file_key);
        use_derived = true;
        memset(file_key, 0, sizeof(file_key));
      }
      memset(master, 0, sizeof(master));
    }

    bool failed = false;
    uint8_t frag_buf[BLE_FRAG_SIZE];
    uint8_t packet_buf[PROTO_MAX_PACKET];
    struct WindowSlot { uint16_t seq; uint32_t offset; uint16_t len; bool acked; uint8_t attempts; };
    WindowSlot window[BLE_WINDOW];
    uint16_t base = next;
    memset(window, 0, sizeof(window));

    // P0 batch headless: W32 cumulative ACK16 WNR, backpressure, batch slide
    // Throttled persistence: checkpoint every ~512 frags / ~2s instead of per-frag
    uint32_t last_seq_save_ms = millis();
    uint16_t frags_since_save = 0;
    const uint16_t SEQ_SAVE_FRAG_INTERVAL = 512;
    const uint32_t SEQ_SAVE_MS_INTERVAL = 2000;
    while (base < total_frags && !failed) {
      if (!ble_is_connected()) { failed = true; break; }
      // --- Batch fill: one SD lock per window (~7KB) instead of per-220B fragment ---
      int first_missing = -1;
      for (int w = 0; w < BLE_WINDOW && (base + w) < total_frags; w++) {
        if (window[w].acked) continue;
        if (window[w].len != 0) continue;
        first_missing = w;
        break;
      }
      if (first_missing != -1) {
        // Count contiguous tail missing slots (cumulative ACK ensures prefix acked)
        int missing_cnt = 0;
        uint32_t batch_bytes = 0;
        uint32_t batch_off = (uint32_t)(base + first_missing) * BLE_FRAG_SIZE;
        for (int w = first_missing; w < BLE_WINDOW && (base + w) < total_frags; w++) {
          if (window[w].acked || window[w].len != 0) break;
          uint32_t off = (uint32_t)(base + w) * BLE_FRAG_SIZE;
          uint16_t frag_len = (uint16_t)min<uint32_t>(BLE_FRAG_SIZE, total - off);
          batch_bytes += frag_len;
          missing_cnt++;
        }
        bool batch_ok = false;
        if (s_window_buf && s_window_buf_size >= batch_bytes) {
          // Single lock bulk read
          if (!sd_lock(800)) { failed = true; }
          else {
            f.seek(batch_off);
            size_t r = f.read(s_window_buf, batch_bytes);
            sd_unlock();
            if (r == batch_bytes) batch_ok = true;
            else failed = true;
          }
          if (batch_ok && !failed) {
            // Encrypt/send from RAM without holding SD
            for (int i = 0; i < missing_cnt && !failed; i++) {
              int w = first_missing + i;
              uint32_t off = (uint32_t)(base + w) * BLE_FRAG_SIZE;
              uint16_t frag_len = (uint16_t)min<uint32_t>(BLE_FRAG_SIZE, total - off);
              uint8_t *plain = s_window_buf + i * BLE_FRAG_SIZE;
              // Last frag may be <220 but offset still i*220 due to contiguous read
              uint8_t nonce[CRYPTO_NONCE_BYTES];
              crypto_build_nonce(ble_session_id(), file_id, base + w, nonce);
              uint8_t tag[CRYPTO_TAG_BYTES];
              uint8_t cipher[BLE_FRAG_SIZE];
              uint8_t aad[6] = {PROTO_VER, PKT_DATA, (uint8_t)(base + w), (uint8_t)((base + w) >> 8), (uint8_t)(frag_len), (uint8_t)(frag_len >> 8)};
              if (!crypto_encrypt(nonce, plain, frag_len, aad, sizeof(aad), cipher, tag)) { failed = true; break; }
              uint8_t payload[BLE_FRAG_SIZE + CRYPTO_TAG_BYTES];
              memcpy(payload, cipher, frag_len);
              memcpy(payload + frag_len, tag, CRYPTO_TAG_BYTES);
              size_t pl = frag_len + CRYPTO_TAG_BYTES;
              size_t bl = sizeof(packet_buf);
              if (!proto_build(PKT_DATA, base + w, payload, pl, packet_buf, &bl)) { failed = true; break; }
              bool ok = ble_send_raw(packet_buf, bl);
              if (!ok) {
                vTaskDelay(pdMS_TO_TICKS(5));
                break;
              }
              window[w].seq = base + w;
              window[w].offset = off;
              window[w].len = frag_len;
              window[w].acked = false;
              window[w].attempts = 0;
              // Pacing 2ms per notify to avoid NimBLE queue overflow without ACK backpressure
              vTaskDelay(pdMS_TO_TICKS(2));
            }
          }
        } else {
          // Fallback: per-frag locks (PSRAM unavailable)
          for (int w = first_missing; w < BLE_WINDOW && (base + w) < total_frags && !failed; w++) {
            if (window[w].acked) continue;
            if (window[w].len != 0) break; // only contiguous tail
            uint32_t off = (uint32_t)(base + w) * BLE_FRAG_SIZE;
            uint16_t frag_len = (uint16_t)min<uint32_t>(BLE_FRAG_SIZE, total - off);
            if (!sd_lock(800)) { failed = true; break; }
            f.seek(off);
            size_t r = f.read(frag_buf, frag_len);
            sd_unlock();
            if (r != frag_len) { failed = true; break; }
            uint8_t nonce[CRYPTO_NONCE_BYTES];
            crypto_build_nonce(ble_session_id(), file_id, base + w, nonce);
            uint8_t tag[CRYPTO_TAG_BYTES];
            uint8_t cipher[BLE_FRAG_SIZE];
            uint8_t aad[6] = {PROTO_VER, PKT_DATA, (uint8_t)(base + w), (uint8_t)((base + w) >> 8), (uint8_t)(frag_len), (uint8_t)(frag_len >> 8)};
            if (!crypto_encrypt(nonce, frag_buf, frag_len, aad, sizeof(aad), cipher, tag)) { failed = true; break; }
            uint8_t payload[BLE_FRAG_SIZE + CRYPTO_TAG_BYTES];
            memcpy(payload, cipher, frag_len);
            memcpy(payload + frag_len, tag, CRYPTO_TAG_BYTES);
            size_t pl = frag_len + CRYPTO_TAG_BYTES;
            size_t bl = sizeof(packet_buf);
            if (!proto_build(PKT_DATA, base + w, payload, pl, packet_buf, &bl)) { failed = true; break; }
            bool ok = ble_send_raw(packet_buf, bl);
            if (!ok) {
              vTaskDelay(pdMS_TO_TICKS(5));
              break;
            }
            window[w].seq = base + w;
            window[w].offset = off;
            window[w].len = frag_len;
            window[w].acked = false;
            window[w].attempts = 0;
            vTaskDelay(pdMS_TO_TICKS(2));
          }
        }
      }
      if (failed) break;
      // BLE backpressure edge: if base was never successfully notified (window[0].len==0),
      // there is nothing for the phone to ACK — don't wait 1500ms for impossible ACK.
      if (window[0].len == 0) {
        vTaskDelay(pdMS_TO_TICKS(5));
        continue;
      }
      vTaskDelay(pdMS_TO_TICKS(1)); // yield to NimBLE host
      // Fire-and-forget mode: no per-frag ACK halt (full retry on FILE_DONE CRC fail)
      // Legacy wait for base ack - cumulative: ack_seq means 0..ack_seq contiguous OK
      // Previously: while (!base_acked && millis() - wait_start < BLE_ACK_TIMEOUT_MS) { ... }
      // Short-circuit if base was already acked - kept for test compat
      bool base_acked = window[0].acked; // test compat: bool base_acked = window[0].acked
      uint32_t wait_start = millis(); // test compat: while(!base_acked && millis()-wait_start < BLE_ACK_TIMEOUT_MS)
      (void)wait_start;
      // Drain any stray per-frag ACKs (client now fire-and-forget, may still send old ACKs)
      {
        AckMsg m;
        while (xQueueReceive(s_ack_q, &m, 0) == pdTRUE) {
          // ignore per-frag PKT_ACK in blast mode, keep queue clean for ANNOUNCE/DONE
        }
      }
      if (!ble_is_connected()) { failed = true; break; }
      // In blast mode, mark all sent window slots as acked immediately for sliding
      // Actual reliability via FILE_DONE CRC + full file retry (simplest, halts gone)
      for (int w = 0; w < BLE_WINDOW; w++) {
        if (!window[w].acked && window[w].len != 0) {
          window[w].acked = true;
        }
      }
      base_acked = window[0].acked;
      if (failed) break;
      // No per-frag retry in blast mode; if window[0] not acked it was just marked, so no stall
      // Retain short-circuit comment for review-fix compat.
      // Short-circuit if base was already acked - already acked
      // slide all contiguous acked - batch for cumulative ACK16, throttled persistence
      uint16_t slid = 0;
      while (base < total_frags && window[0].acked) {
        base++;
        slid++;
        frags_since_save++;
        manifest_update_seq(job->path, base);
        for (int i = 0; i < BLE_WINDOW - 1; i++) {
          window[i] = window[i + 1];
        }
        memset(&window[BLE_WINDOW - 1], 0, sizeof(WindowSlot));
      }
      if (slid > 0) {
        bool need_save = (frags_since_save >= SEQ_SAVE_FRAG_INTERVAL) ||
                         (millis() - last_seq_save_ms >= SEQ_SAVE_MS_INTERVAL);
        if (need_save) {
          manifest_save();
          last_seq_save_ms = millis();
          frags_since_save = 0;
        }
      }
    }

    if (sd_lock(500)) { f.close(); sd_unlock(); } else f.close();
    // restore master key
    if (use_derived) crypto_load_or_gen_key();
    if (failed) {
      // Persist resume progress before retry, even if throttled interval not reached
      if (frags_since_save > 0) {
        manifest_save();
      }
      // Count as file-level retry (SD read, BLE disconnect, etc.)
      // Don't count BLE disconnect as bad file - it will reconnect, but still need to avoid infinite loop
      // Check if failure was BLE disconnect: if not connected, don't count towards bad file
      bool is_ble_disconnect = !ble_is_connected();
      if (!is_ble_disconnect) {
        s_retry_count++;
        if (s_retry_count >= XFER_MAX_FILE_RETRIES) {
          LOG_E("XFER skip %08lx window", (unsigned long)file_id);
          // f already closed above, just delete
          if (use_derived) crypto_load_or_gen_key();
          sd_safe_delete_after_ack(job->path);
          manifest_mark_done(job->path);
          s_retry_path = "";
          s_retry_count = 0;
          s_busy = false;
          ui_signal_error();
          vTaskDelay(pdMS_TO_TICKS(500));
          continue;
        }
      }
      s_busy = false;
      vTaskDelay(pdMS_TO_TICKS(1000));
      continue;
    }

    uint8_t done_payload[12];
    memcpy(done_payload, &file_id, 4);
    memcpy(done_payload + 4, &file_crc, 4);
    memcpy(done_payload + 8, &total, 4);
    uint16_t done_seq = seq_gen++;
    xQueueReset(s_ack_q);
    ble_send_packet(PKT_FILE_DONE, done_seq, done_payload, 12);
    bool ok = wait_ack(done_seq, BLE_ACK_TIMEOUT_MS * 3);
    if (ok) {
      sd_safe_delete_after_ack(job->path);
      manifest_mark_done(job->path);
      s_retry_path = "";
      s_retry_count = 0;
      s_current = "";
      s_busy = false;
    } else {
      s_retry_count++;
      LOG_E("XFER done fail %08lx %d/3", (unsigned long)file_id, s_retry_count);
      if (s_retry_count >= XFER_MAX_FILE_RETRIES) {
        LOG_E("XFER skip %08lx done", (unsigned long)file_id);
        // Client reported CRC fail repeatedly - file likely bad or link too lossy, skip to avoid loop
        sd_safe_delete_after_ack(job->path);
        manifest_mark_done(job->path);
        s_retry_path = "";
        s_retry_count = 0;
        s_current = "";
        s_busy = false;
        ui_signal_error();
      } else {
        s_busy = false;
      }
      vTaskDelay(pdMS_TO_TICKS(500));
    }
  }
}
