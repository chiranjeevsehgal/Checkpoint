#pragma once
#include <Arduino.h>
#include "protocol.h"

// Remote transport + LED control over BLE CTRL characteristic.
// Threading: control_on_packet() runs in the NimBLE callback task — it only
// stores request flags and returns fast. control_poll() runs in loop() and
// owns recorder_* / ui_* / NVS / ble_send_packet work.

// Command IDs carried in PKT_CMD payload[0].
enum CtrlCmd : uint8_t {
  CTRL_CMD_REC_START = 0x01,
  CTRL_CMD_REC_STOP = 0x02,
  CTRL_CMD_LED_SET = 0x10,
  CTRL_CMD_LED_GET = 0x11,
  CTRL_CMD_SYNC_SET = 0x12,   // payload[1] = 0/1 (off/on)
  CTRL_CMD_SYNC_GET = 0x13,   // reply extra [enabled]
  CTRL_CMD_TIME_SET = 0x14,   // payload[1..8] = unix seconds u64 LE
  CTRL_CMD_FILE_DELETE = 0x20,  // payload[1..] = full "/rec/..." path bytes
  CTRL_CMD_STORAGE_ERASE = 0x21, // payload[1] = CTRL_ERASE_ARM / CTRL_ERASE_CONFIRM
  CTRL_CMD_FILE_FETCH = 0x22,   // payload[1..] = full "/rec/..." path (preview, keeps file)
  CTRL_CMD_GET_CLOUD_SECRET = 0x23,    // reply: [cmd,status, nonce12, cipher32, tag8]
  CTRL_CMD_CLEAR_TRUSTED_SLOTS = 0x24, // ACK first, then wipe client slots + session
  CTRL_CMD_FORGET_SELF = 0x25,         // ACK first, then drop the caller's own slot + session
};

// Status codes carried in PKT_CMD_RESP payload[1].
enum CtrlStatus : uint8_t {
  CTRL_OK = 0x00,
  CTRL_ERR_NOT_READY = 0x01,
  CTRL_ERR_NO_SD = 0x02,
  CTRL_ERR_BAD_ARG = 0x03,
  CTRL_ERR_DENIED = 0x04,
  CTRL_ERR_BUSY = 0x05,     // target is the active recording or in-flight transfer
  CTRL_ERR_NOT_FOUND = 0x06,
};

// STATUS_RESP payload layout (17 bytes, little-endian where noted):
// [0]=recording [1]=vad_active [2]=vad_speech [3]=muted [4]=brightness
// [5]=level_dbfs int8 [6..7]=pending u16 [8..11]=chunks u32 [12..15]=utterances u32
// [16]=sync_enabled (BLE auto-upload on/off)
// Replies echo the STATUS_REQ seq; unsolicited pushes use seq 0 so the host
// can distinguish polls from hardware-side change events.
#define CTRL_STATUS_LEN 17

// LED brightness bounds. 0 is only honored while muted; unmuted values
// below MIN are clamped up so "dim" never accidentally means "dark".
#define CTRL_BRIGHT_MIN 5
#define CTRL_BRIGHT_MAX 255

// STORAGE_RESP payload (20 bytes, little-endian):
// [0..7]=total card bytes u64 (0 = unknown), [8..15]=used-by-recordings u64,
// [16..17]=file count u16, [18..19]=pending count u16.
#define CTRL_STORAGE_LEN 20

// LIST_REQ payload: [0..1]=start index u16.
// LIST_RESP payload: [0..1]=start u16, [2..3]=total files u16,
// [4]=entry count u8, then entries: [namelen u8, name bytes,
// size u32 LE, flags u8]. Packed until PROTO_MAX_PAYLOAD is hit.
#define CTRL_LIST_FLAG_PENDING 0x01
#define CTRL_LIST_FLAG_CRC 0x02
#define CTRL_LIST_FLAG_ACTIVE 0x04 // open file currently being recorded

// STORAGE_ERASE steps (payload[1]).
#define CTRL_ERASE_ARM 0x01
#define CTRL_ERASE_CONFIRM 0x02
#define CTRL_ERASE_ARM_VALID_MS 30000

void control_init();
void control_poll();
// BLE auto-upload gate, persisted in NVS (default on). transfer_task reads
// this; recording to SD is independent and unaffected.
bool control_sync_enabled();
// Returns true when pkt was a control packet (caller must not treat as error).
bool control_on_packet(const Packet *pkt);
