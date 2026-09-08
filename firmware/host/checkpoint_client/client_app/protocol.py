"""BLE framing. Mirrors protocol.cpp (CRC32, header layout)."""

import struct
import zlib
from dataclasses import dataclass

from . import config as cfg


def crc32(data: bytes) -> int:
    return zlib.crc32(data) & 0xFFFFFFFF


def proto_build(ptype: int, seq: int, payload: bytes = b"") -> bytes:
    header = struct.pack("<BBHH", cfg.PROTO_VER, ptype, seq, len(payload))
    body = header + payload
    return body + struct.pack("<I", crc32(body))


@dataclass
class Packet:
    version: int
    type: int
    seq: int
    payload: bytes


def proto_parse(data: bytes) -> Packet | None:
    if len(data) < cfg.PROTO_HEADER + cfg.PROTO_CRC:
        return None
    ver, ptype, seq, plen = struct.unpack("<BBHH", data[:cfg.PROTO_HEADER])
    if ver != cfg.PROTO_VER:
        return None
    need = cfg.PROTO_HEADER + plen + cfg.PROTO_CRC
    if len(data) < need:
        return None
    payload = data[cfg.PROTO_HEADER:cfg.PROTO_HEADER + plen]
    recv_crc = struct.unpack("<I", data[cfg.PROTO_HEADER + plen:need])[0]
    if recv_crc != crc32(data[:cfg.PROTO_HEADER + plen]):
        print(f"  [!] CRC mismatch on packet type={ptype}")
        return None
    return Packet(ver, ptype, seq, payload)


PKT_NAMES = {
    cfg.PKT_HELLO: "HELLO",
    cfg.PKT_HELLO_ACK: "HELLO_ACK",
    cfg.PKT_FILE_ANNOUNCE: "FILE_ANNOUNCE",
    cfg.PKT_FILE_ANNOUNCE_ACK: "FILE_ANNOUNCE_ACK",
    cfg.PKT_DATA: "DATA",
    cfg.PKT_ACK: "ACK",
    cfg.PKT_FILE_DONE: "FILE_DONE",
    cfg.PKT_FILE_DONE_ACK: "FILE_DONE_ACK",
    cfg.PKT_ERROR: "ERROR",
    cfg.PKT_RESUME_REQ: "RESUME_REQ",
    cfg.PKT_RESUME_RESP: "RESUME_RESP",
    cfg.PKT_KEEPALIVE: "KEEPALIVE",
    cfg.PKT_CMD: "CMD",
    cfg.PKT_CMD_RESP: "CMD_RESP",
    cfg.PKT_STATUS_REQ: "STATUS_REQ",
    cfg.PKT_STATUS_RESP: "STATUS_RESP",
    cfg.PKT_STORAGE_REQ: "STORAGE_REQ",
    cfg.PKT_STORAGE_RESP: "STORAGE_RESP",
    cfg.PKT_LIST_REQ: "LIST_REQ",
    cfg.PKT_LIST_RESP: "LIST_RESP",
    cfg.PKT_READY: "READY",
}
