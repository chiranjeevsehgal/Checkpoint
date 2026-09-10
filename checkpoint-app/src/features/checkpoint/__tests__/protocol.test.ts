import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { PKT_CMD, PKT_STATUS_REQ } from "../config.ts";
import {
  buildPacket,
  crc32,
  packetName,
  parsePacket,
} from "../protocol.ts";
import { hexToBytes } from "../crypto.ts";

function hex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

describe("crc32", () => {
  it("matches zlib for empty input", () => {
    assert.equal(crc32(new Uint8Array(0)).toString(16).padStart(8, "0"), "00000000");
  });

  it("matches zlib for abc", () => {
    assert.equal(
      crc32(new TextEncoder().encode("abc")).toString(16).padStart(8, "0"),
      "352441c2",
    );
  });
});

describe("buildPacket", () => {
  it("matches client_app proto_build byte for byte", () => {
    const pkt = buildPacket(PKT_CMD, 0x1234, new Uint8Array([0x10, 0x00, 0x1e]));
    assert.equal(hex(pkt), "03203412030010001e2f8c4995");
  });

  it("matches empty-payload build", () => {
    assert.equal(hex(buildPacket(PKT_STATUS_REQ, 1)), "0322010000000cc8eb34");
  });
});

describe("parsePacket", () => {
  it("round-trips the known packet", () => {
    const parsed = parsePacket(hexToBytes("03203412030010001e2f8c4995"));
    assert.ok(parsed);
    assert.equal(parsed.version, 3);
    assert.equal(parsed.type, 32);
    assert.equal(parsed.seq, 4660);
    assert.equal(hex(parsed.payload), "10001e");
  });

  it("rejects short input", () => {
    assert.equal(parsePacket(new Uint8Array([0x03, 0x01])), null);
  });

  it("rejects wrong version", () => {
    const pkt = buildPacket(PKT_CMD, 1, new Uint8Array([0xaa]));
    const bad = Uint8Array.from(pkt);
    bad[0] = 0x02;
    assert.equal(parsePacket(bad), null);
  });

  it("rejects bad crc", () => {
    const pkt = buildPacket(PKT_CMD, 1, new Uint8Array([0xaa]));
    const bad = Uint8Array.from(pkt);
    const last = bad.length - 1;
    bad[last] = bad[last]! ^ 0xff;
    assert.equal(parsePacket(bad), null);
  });

  it("rejects truncated payload", () => {
    const pkt = buildPacket(PKT_CMD, 1, new Uint8Array([0xaa, 0xbb]));
    assert.equal(parsePacket(pkt.slice(0, pkt.length - 2)), null);
  });
});

describe("packetName", () => {
  it("names known packets", () => {
    assert.equal(packetName(0x01), "HELLO");
    assert.equal(packetName(0x28), "READY");
  });

  it("falls back to hex", () => {
    assert.equal(packetName(0xff), "0xff");
  });
});
