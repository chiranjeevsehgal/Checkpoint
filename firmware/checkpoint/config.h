#pragma once
#include <Arduino.h>

// Hardware pins — frozen to HW/HARDWARE.md Rev B and HW/CONNECTIONS.md
// Variant flag: 1 = Rev B 13-pin Adafruit 4682 with DET on GPIO7,
//              0 = 8-pin 3V Only (DAT2/DAT1/CS/MOSI/MISO/CLK/GND/3V3) with no DET, GPIO7 NC
#ifndef HW_HAS_SD_DETECT
#define HW_HAS_SD_DETECT 0
#endif
#define HW_I2S_WS_GPIO 4
#define HW_I2S_BCLK_GPIO 5
#define HW_I2S_DIN_GPIO 6
#if HW_HAS_SD_DETECT
#define HW_SD_DETECT_GPIO 7
#else
#define HW_SD_DETECT_GPIO -1 // NC — do not configure, sd_present() returns true
#endif
#define HW_SD_CS_GPIO 10
#define HW_SD_MOSI_GPIO 11
#define HW_SD_CLK_GPIO 12
#define HW_SD_MISO_GPIO 13
#define HW_BUTTON_GPIO 1
#define HW_RECORD_LED_GPIO 2 // deprecated — RGB-only build, kept for compat
#define HW_RGB_PIN 48        // onboard WS2812 (v1.1 clones: 38)
#define HW_RGB_BRIGHTNESS 30 // dim: visible, ~10mA

// Audio — 50s trial (was 30s->20s keeps BLE <30s at 15ms W4, 60s >60s backlog)
// Codec: OGG-Opus 16k mono VOIP C0 16kbps default (~100KB/50s 455 frags vs 1.6MB/7273 PCM)
// Override at build: -DREC_OPUS_BITRATE=24000 for 24k field-test
#define REC_SAMPLE_RATE 16000
#define REC_BITS_PER_SAMPLE 16
#define REC_CHANNELS 1
#define REC_BYTES_PER_SAMPLE 2
#define REC_BYTES_PER_SEC (REC_SAMPLE_RATE * REC_BYTES_PER_SAMPLE * REC_CHANNELS)
#define REC_CHUNK_SEC 50
#define REC_CHUNK_BYTES (REC_CHUNK_SEC * REC_BYTES_PER_SEC)
#define REC_I2S_PORT I2S_NUM_0
#define REC_I2S_DMA_BUF_COUNT 8
#define REC_I2S_DMA_BUF_LEN 512
#define REC_RING_KB 96
#define REC_RING_BYTES (REC_RING_KB * 1024)
// Opus — fixed-point 16k mono VOIP, 20ms frames (320 samples = 640B PCM → ~40B @16k)
#ifndef REC_OPUS_BITRATE
#define REC_OPUS_BITRATE 16000
#endif
#define REC_OPUS_FRAME_MS 20
#define REC_OPUS_SAMPLES_PER_FRAME (REC_SAMPLE_RATE * REC_OPUS_FRAME_MS / 1000) // 320
#define REC_OPUS_MAX_FRAME_BYTES 80 // 24k → 60B, headroom 80
#define REC_OPUS_RING_KB 64
#define REC_OPUS_RING_BYTES (REC_OPUS_RING_KB * 1024)
#define REC_OPUS_EXT ".ogg"
#define REC_OPUS_HEAD_SIZE 19
#define REC_OPUS_TAGS_SIZE 32
// Keep WAV for migration/rollback: old .wav files still listed by manifest
// Real Opus via PCMFlowOpus 0.2.0 (vendored libopus 1.3.1) — 16k mono VOIP C0 16kbps default
#define REC_CODEC_OPUS 1

// VAD — voice-triggered recording (spectral VAD in vad.{h,cpp}, 16k 20ms frames)
// IDLE: mic ON, VAD running, no SD file. ONSET (~50% voiced density over a
// 200ms window + 40ms consecutive run + syllabic energy wobble with >=1 IAC
// flip and >=3.5dB range, mode 1, IDLE-only impulse veto) opens a chunk and
// flushes pre-roll. Philosophy is recall-first: onset is moderately easy,
// then MIN_SPEECH (true voiced frames) discards blips afterward — a missed
// word cannot be recovered, but a false clip can be deleted. NOTE: pre-roll
// preserves audio from BEFORE the trigger but cannot cause one — onset still
// needs ~100ms of voiced evidence (5/10), so isolated sub-100ms clicks
// cannot trigger. HANGOVER (1500ms silence) suspends writes but keeps the
// file open; +SESSION_EXTEND (2000ms) with no speech closes it.
// A 2s spectrally-flat session is discarded as hum. VAD_ENABLE 0 = legacy
// continuous 50s chunks.
#define VAD_ENABLE 1
#define VAD_MODE 1
#define VAD_ONSET_MS 200
#define VAD_HANGOVER_MS 1500
#define VAD_SESSION_EXTEND_MS 2000
#define VAD_MIN_SPEECH_MS 150
#define VAD_PREROLL_MS 750
#define VAD_ABS_FLOOR_DBFS -54.0f

// SD / filesystem
// NOTE: SD_MOUNT_POINT is the VFS mountpoint passed to SD.begin (default "/sd").
// Arduino FS prepends mountpoint internally (vfs_api.cpp snprintf "%s%s", mountpoint, path),
// so REC_DIR/REC_MANIFEST must be FS-relative ("/rec"), NOT "/sd/rec" (would become "/sd/sd/rec").
#define SD_MOUNT_POINT "/sd"
#define SD_SPI_HOST SPI2_HOST
#define SD_SPI_FREQ_KHZ 2000
#define SD_MAX_FILES 4096
#define REC_DIR "/rec"
#define REC_MANIFEST "/rec/manifest.json"
// SDMMC 1-bit alternative for breadboard SI issues — set to 1 to use SD_MMC 1-bit (CLK 12, CMD 11, D0 13)
// Requires rewiring DAT1/DAT2 pull-ups (10k to 3.3V) per sd_pullup_requirements; SPI 4MHz remains default 0
// NOTE: Card stays in SPI mode until power-cycle — must unplug USB 5s after switching
#define SD_USE_SDMMC 1
#define REC_TMP_EXT ".tmp"
#define REC_WAV_EXT ".wav"
#define REC_DEL_EXT ".del"

// BLE — P0 battery headless: W32/15ms, MTU 517+DLE 251, 1500ms, ACK16 WNR IDLE100, iOS-safe, auto ADV no Serial needed
#define BLE_DEVICE_NAME "Checkpoint"
#define BLE_SERVICE_UUID "9a8b0001-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
#define BLE_CTRL_UUID "9a8b0002-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
#define BLE_DATA_UUID "9a8b0003-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
#define BLE_ACK_UUID "9a8b0004-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
#define BLE_MTU 517
#define BLE_FRAG_SIZE 220
#define BLE_WINDOW 32
#define BLE_ACK_EVERY 16
#define BLE_IDLE_MS 100
#define BLE_ACK_TIMEOUT_MS 1500
#define BLE_RETRY_MAX 8
#define BLE_HANDSHAKE_TIMEOUT_MS 5000
#define BLE_KEEPALIVE_MS 5000

// Protocol — DATA is frag(220)+CCM tag(8)=228, need header+crc
#define PROTO_VER 1
#define PROTO_MAX_PAYLOAD (BLE_FRAG_SIZE + CRYPTO_TAG_BYTES)
#define PROTO_HEADER 6
#define PROTO_CRC 4
#define PROTO_MAX_PACKET (PROTO_HEADER + PROTO_MAX_PAYLOAD + PROTO_CRC)

// Crypto AES-128-CCM
#define CRYPTO_KEY_BYTES 16
#define CRYPTO_NONCE_BYTES 12
#define CRYPTO_TAG_BYTES 8

// UI
#define UI_DEBOUNCE_MS 50
#define UI_LED_ON_MS 120
#define UI_LED_BOOKMARK_MS 250

// System
// NOTE: libopus opus_encode() needs ~20-30KB call depth (SILK+CELT, incl. ROM
// memmove loop at 0x40056f5c-72 seen in backtraces). 12KB overflows the
// opus_enc canary immediately on the first frame ("Stack canary watchpoint
// triggered (opus_enc)"). Recorder also calls opus_encode() in its close_chunk
// PCM-drain path, so it needs the same headroom. 32KB each is ~64KB DRAM,
// safe on ESP32-S3 (512KB SRAM).
#define TASK_STACK_RECORDER 32768
#define TASK_STACK_TRANSFER 8192
#define TASK_STACK_ENCODER 32768
#define TASK_STACK_UI 4096
#define TASK_PRIO_RECORDER 6
#define TASK_PRIO_ENCODER 5
#define TASK_PRIO_TRANSFER 4
#define TASK_PRIO_UI 2
