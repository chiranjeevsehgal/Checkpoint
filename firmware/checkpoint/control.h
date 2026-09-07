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
};

// Status codes carried in PKT_CMD_RESP payload[1].
enum CtrlStatus : uint8_t {
  CTRL_OK = 0x00,
  CTRL_ERR_NOT_READY = 0x01,
  CTRL_ERR_NO_SD = 0x02,
  CTRL_ERR_BAD_ARG = 0x03,
  CTRL_ERR_DENIED = 0x04,
};

// STATUS_RESP payload layout (16 bytes, little-endian where noted):
// [0]=recording [1]=vad_active [2]=vad_speech [3]=muted [4]=brightness
// [5]=level_dbfs int8 [6..7]=pending u16 [8..11]=chunks u32 [12..15]=utterances u32
#define CTRL_STATUS_LEN 16

// LED brightness bounds. 0 is only honored while muted; unmuted values
// below MIN are clamped up so "dim" never accidentally means "dark".
#define CTRL_BRIGHT_MIN 5
#define CTRL_BRIGHT_MAX 255

void control_init();
void control_poll();
// Returns true when pkt was a control packet (caller must not treat as error).
bool control_on_packet(const Packet *pkt);
