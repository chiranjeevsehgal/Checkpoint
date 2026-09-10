import { PROTO_CRC, PROTO_HEADER, PROTO_VER } from "./config.ts";
import type { Packet } from "./types.ts";

const CRC_TABLE: Uint32Array = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let entry = n;
    for (let k = 0; k < 8; k++) {
      entry = entry & 1 ? 0xedb88320 ^ (entry >>> 1) : entry >>> 1;
    }
    table[n] = entry;
  }
  return table;
})();

export function crc32(data: Uint8Array): number {
  let crc = 0xffffffff;
  for (const byte of data) {
    crc = CRC_TABLE[(crc ^ byte) & 0xff]! ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

export function buildPacket(
  type: number,
  seq: number,
  payload: Uint8Array = new Uint8Array(0),
): Uint8Array {
  const body = new Uint8Array(PROTO_HEADER + payload.length);
  const view = new DataView(body.buffer);
  view.setUint8(0, PROTO_VER);
  view.setUint8(1, type & 0xff);
  view.setUint16(2, seq & 0xffff, true);
  view.setUint16(4, payload.length, true);
  body.set(payload, PROTO_HEADER);
  const out = new Uint8Array(body.length + PROTO_CRC);
  out.set(body, 0);
  new DataView(out.buffer).setUint32(body.length, crc32(body), true);
  return out;
}

export function parsePacket(data: Uint8Array): Packet | null {
  if (data.length < PROTO_HEADER + PROTO_CRC) return null;
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const version = view.getUint8(0);
  if (version !== PROTO_VER) return null;
  const type = view.getUint8(1);
  const seq = view.getUint16(2, true);
  const payloadLen = view.getUint16(4, true);
  const need = PROTO_HEADER + payloadLen + PROTO_CRC;
  if (data.length < need) return null;
  const payload = data.slice(PROTO_HEADER, PROTO_HEADER + payloadLen);
  const receivedCrc = view.getUint32(PROTO_HEADER + payloadLen, true);
  if (receivedCrc !== crc32(data.slice(0, PROTO_HEADER + payloadLen))) {
    return null;
  }
  return { version, type, seq, payload };
}

export const PACKET_NAMES: Record<number, string> = {
  0x01: "HELLO",
  0x02: "HELLO_ACK",
  0x03: "AUTH",
  0x04: "AUTH_OK",
  0x05: "READY_ACK",
  0x10: "FILE_ANNOUNCE",
  0x11: "FILE_ANNOUNCE_ACK",
  0x12: "DATA",
  0x13: "ACK",
  0x14: "FILE_DONE",
  0x15: "FILE_DONE_ACK",
  0x16: "ERROR",
  0x17: "RESUME_REQ",
  0x18: "RESUME_RESP",
  0x19: "KEEPALIVE",
  0x20: "CMD",
  0x21: "CMD_RESP",
  0x22: "STATUS_REQ",
  0x23: "STATUS_RESP",
  0x24: "STORAGE_REQ",
  0x25: "STORAGE_RESP",
  0x26: "LIST_REQ",
  0x27: "LIST_RESP",
  0x28: "READY",
};

export function packetName(type: number): string {
  return (
    PACKET_NAMES[type] ?? `0x${(type & 0xff).toString(16).padStart(2, "0")}`
  );
}
