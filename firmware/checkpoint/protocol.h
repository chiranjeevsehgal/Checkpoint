#pragma once
#include <Arduino.h>
#include "config.h"

enum PacketType : uint8_t {
  PKT_HELLO = 0x01,
  PKT_HELLO_ACK = 0x02,
  PKT_FILE_ANNOUNCE = 0x10,
  PKT_FILE_ANNOUNCE_ACK = 0x11,
  PKT_DATA = 0x12,
  PKT_ACK = 0x13,
  PKT_FILE_DONE = 0x14,
  PKT_FILE_DONE_ACK = 0x15,
  PKT_ERROR = 0x16,
  PKT_RESUME_REQ = 0x17,
  PKT_RESUME_RESP = 0x18,
  PKT_KEEPALIVE = 0x19,
  PKT_CMD = 0x20,
  PKT_CMD_RESP = 0x21,
  PKT_STATUS_REQ = 0x22,
  PKT_STATUS_RESP = 0x23,
  PKT_STORAGE_REQ = 0x24,
  PKT_STORAGE_RESP = 0x25,
  PKT_LIST_REQ = 0x26,
  PKT_LIST_RESP = 0x27
};

struct Packet {
  uint8_t version;
  uint8_t type;
  uint16_t seq;
  uint16_t len;
  uint8_t payload[PROTO_MAX_PAYLOAD];
  uint32_t crc;
};

uint32_t proto_crc32(const uint8_t *data, size_t len);
bool proto_build(uint8_t type, uint16_t seq, const uint8_t *payload, uint16_t payload_len, uint8_t *out, size_t *out_len);
bool proto_parse(const uint8_t *in, size_t in_len, Packet *out);
const char *proto_type_name(uint8_t type);
