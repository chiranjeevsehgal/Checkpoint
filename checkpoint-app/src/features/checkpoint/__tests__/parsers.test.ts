import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { hexToBytes } from "../crypto.ts";
import {
  buildFileDeletePayload,
  buildLedSetPayload,
  buildListReqPayload,
  buildStorageErasePayload,
  buildSyncSetPayload,
  ctrlStatusText,
  fileStateLabel,
  formatBytes,
  isValidUserId,
  parseCmdResp,
  parseFileList,
  parseStatus,
  parseStorage,
} from "../parsers.ts";

describe("parseStatus", () => {
  it("matches client_app output", () => {
    const status = parseStatus(
      hexToBytes("010100011ed603002a0000000500000001"),
    );
    assert.deepEqual(status, {
      recording: true,
      vadActive: true,
      vadSpeech: false,
      muted: true,
      brightness: 30,
      levelDbfs: -42,
      pending: 3,
      chunks: 42,
      utterances: 5,
      sync: true,
    });
  });

  it("returns null when short", () => {
    assert.equal(parseStatus(new Uint8Array([1, 2])), null);
  });

  it("defaults sync true on 16-byte payload", () => {
    const status = parseStatus(
      hexToBytes("00000000000000000000000000000000"),
    );
    if (!status) throw new Error("parse returned null");
    assert.equal(status.sync, true);
  });
});

describe("parseStorage", () => {
  it("matches client_app output", () => {
    const info = parseStorage(
      hexToBytes("0000000008000000000000800100000029000300"),
    );
    assert.deepEqual(info, {
      total: 34359738368,
      used: 6442450944,
      files: 41,
      pending: 3,
    });
  });

  it("returns null when short", () => {
    assert.equal(parseStorage(new Uint8Array(10)), null);
  });
});

describe("parseFileList", () => {
  it("matches client_app output", () => {
    const list = parseFileList(
      hexToBytes(
        "0000020002086368756e6b3030310090010001086368756e6b3030320020030004",
      ),
    );
    assert.deepEqual(list, {
      start: 0,
      total: 2,
      entries: [
        { name: "chunk001", size: 102400, flags: 1 },
        { name: "chunk002", size: 204800, flags: 4 },
      ],
    });
  });

  it("returns null when short", () => {
    assert.equal(parseFileList(new Uint8Array([0, 0])), null);
  });
});

describe("parseCmdResp", () => {
  it("reads led-get fields", () => {
    const res = parseCmdResp(0x11, new Uint8Array([0x11, 0x00, 0x01, 0x1e]));
    assert.deepEqual(res, {
      cmd: 0x11,
      status: 0,
      led: { muted: true, brightness: 30 },
    });
  });

  it("reads sync-get fields", () => {
    const res = parseCmdResp(0x13, new Uint8Array([0x13, 0x00, 0x01]));
    assert.deepEqual(res, { cmd: 0x13, status: 0, sync: true });
  });

  it("reads erase removed count", () => {
    const res = parseCmdResp(0x21, new Uint8Array([0x21, 0x00, 0x07, 0x00]));
    assert.deepEqual(res, { cmd: 0x21, status: 0, removed: 7 });
  });
});

describe("command payloads", () => {
  it("clamps led brightness to minimum when unmuted", () => {
    assert.deepEqual(
      Array.from(buildLedSetPayload(false, 2)),
      [0x10, 0x00, 5],
    );
  });

  it("allows zero brightness when muted", () => {
    assert.deepEqual(
      Array.from(buildLedSetPayload(true, 0)),
      [0x10, 0x01, 0],
    );
  });

  it("builds sync payload", () => {
    assert.deepEqual(Array.from(buildSyncSetPayload(false)), [0x12, 0x00]);
  });

  it("builds file delete payload", () => {
    const payload = buildFileDeletePayload("a.ogg");
    assert.equal(payload[0], 0x20);
    assert.equal(
      new TextDecoder().decode(payload.slice(1)),
      "a.ogg",
    );
  });

  it("builds erase payload", () => {
    assert.deepEqual(Array.from(buildStorageErasePayload(1)), [0x21, 0x01]);
  });

  it("builds little-endian list payload", () => {
    assert.deepEqual(Array.from(buildListReqPayload(0x1234)), [0x34, 0x12]);
  });
});

describe("labels", () => {
  it("ctrlStatusText names codes", () => {
    assert.equal(ctrlStatusText(0), "ok");
    assert.equal(ctrlStatusText(1), "not-ready");
    assert.equal(ctrlStatusText(2), "no-sd");
    assert.equal(ctrlStatusText(3), "bad-arg");
    assert.equal(ctrlStatusText(4), "denied");
    assert.equal(ctrlStatusText(5), "busy");
    assert.equal(ctrlStatusText(6), "not-found");
    assert.equal(ctrlStatusText(9), "0x09");
  });

  it("fileStateLabel prioritizes recording", () => {
    assert.equal(fileStateLabel(0x04), "recording");
    assert.equal(fileStateLabel(0x01), "pending");
    assert.equal(fileStateLabel(0x00), "synced");
  });

  it("formatBytes scales", () => {
    assert.equal(formatBytes(512), "512B");
    assert.equal(formatBytes(2048), "2KB");
    assert.equal(formatBytes(3 * 1024 * 1024), "3.0MB");
  });
});

describe("isValidUserId", () => {
  it("accepts the dev identity", () => {
    assert.equal(isValidUserId("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), true);
  });

  it("accepts uppercase and padded input", () => {
    assert.equal(isValidUserId("AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA"), true);
    assert.equal(isValidUserId("  aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa  "), true);
  });

  it("rejects empty and non-UUID values", () => {
    assert.equal(isValidUserId(""), false);
    assert.equal(isValidUserId("null"), false);
    assert.equal(isValidUserId("Bearer aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), false);
    assert.equal(isValidUserId("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa"), false);
    assert.equal(
      isValidUserId("checkpoint://claim?key=" + "ab".repeat(32)),
      false,
    );
    assert.equal(isValidUserId("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" + "\u200B"), false);
    assert.equal(isValidUserId("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" + "\u00A0"), true);
  });
});
