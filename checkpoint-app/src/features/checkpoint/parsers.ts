import {
  CTRL_BRIGHT_MIN,
  CTRL_CMD_FILE_DELETE,
  CTRL_CMD_FILE_FETCH,
  CTRL_CMD_LED_GET,
  CTRL_CMD_LED_SET,
  CTRL_CMD_STORAGE_ERASE,
  CTRL_CMD_SYNC_GET,
  CTRL_CMD_SYNC_SET,
  CTRL_CMD_TIME_SET,
  CTRL_ERR_BAD_ARG,
  CTRL_ERR_BUSY,
  CTRL_ERR_DENIED,
  CTRL_ERR_NOT_FOUND,
  CTRL_ERR_NOT_READY,
  CTRL_ERR_NO_SD,
  CTRL_LIST_FLAG_ACTIVE,
  CTRL_LIST_FLAG_PENDING,
  CTRL_OK,
  CTRL_STORAGE_LEN,
  FILE_DONE_TIME_LEN,
  FILE_DONE_TIME_OFFSET,
} from './config.ts';
import { decodeUtf8 } from './ogg.ts';
import type { DeviceFileList, DeviceStatus, LedState, StorageInfo } from './types.ts';

const textEncoder = new TextEncoder();

export function parseStatus(payload: Uint8Array): DeviceStatus | null {
  if (payload.length < 16) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const level = view.getInt8(5);
  return {
    recording: payload[0]! !== 0,
    vadActive: payload[1]! !== 0,
    vadSpeech: payload[2]! !== 0,
    muted: payload[3]! !== 0,
    brightness: payload[4]!,
    levelDbfs: level,
    pending: view.getUint16(6, true),
    chunks: view.getUint32(8, true),
    utterances: view.getUint32(12, true),
    sync: payload.length >= 17 ? payload[16]! !== 0 : true,
  };
}

export function parseStorage(payload: Uint8Array): StorageInfo | null {
  if (payload.length < CTRL_STORAGE_LEN) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  return {
    total: Number(view.getBigUint64(0, true)),
    used: Number(view.getBigUint64(8, true)),
    files: view.getUint16(16, true),
    pending: view.getUint16(18, true),
  };
}

export function parseFileList(payload: Uint8Array): DeviceFileList | null {
  if (payload.length < 5) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const start = view.getUint16(0, true);
  const total = view.getUint16(2, true);
  const count = payload[4]!;
  const entries: DeviceFileList['entries'] = [];
  let offset = 5;
  for (let i = 0; i < count; i++) {
    if (offset + 1 > payload.length) break;
    const nameLen = payload[offset]!;
    offset += 1;
    if (offset + nameLen + 4 + 1 > payload.length) break;
    let name: string;
    try {
      name = decodeUtf8(payload.slice(offset, offset + nameLen));
    } catch {
      break;
    }
    offset += nameLen;
    const entryView = new DataView(payload.buffer, payload.byteOffset + offset, 4);
    const size = entryView.getUint32(0, true);
    const flags = payload[offset + 4]!;
    offset += 5;
    entries.push({ name, size, flags });
  }
  return { start, total, entries };
}

export interface CmdResponse {
  cmd: number;
  status: number;
  led?: LedState;
  sync?: boolean;
  removed?: number;
}

export function parseCmdResp(cmd: number, payload: Uint8Array): CmdResponse {
  const result: CmdResponse = {
    cmd,
    status: payload.length >= 2 ? payload[1]! : CTRL_ERR_NOT_READY,
  };
  if (cmd === CTRL_CMD_LED_GET && payload.length >= 4) {
    result.led = { muted: payload[2]! !== 0, brightness: payload[3]! };
  }
  if (cmd === CTRL_CMD_SYNC_GET && payload.length >= 3) {
    result.sync = payload[2]! !== 0;
  }
  if (cmd === CTRL_CMD_STORAGE_ERASE && payload.length >= 4) {
    const view = new DataView(payload.buffer, payload.byteOffset + 2, 2);
    result.removed = view.getUint16(0, true);
  }
  return result;
}

export function buildLedSetPayload(muted: boolean, brightness: number): Uint8Array {
  let bright = Math.max(0, Math.min(255, Math.trunc(brightness)));
  if (!muted && bright < CTRL_BRIGHT_MIN) bright = CTRL_BRIGHT_MIN;
  return new Uint8Array([CTRL_CMD_LED_SET, muted ? 1 : 0, bright]);
}

export function buildSyncSetPayload(enabled: boolean): Uint8Array {
  return new Uint8Array([CTRL_CMD_SYNC_SET, enabled ? 1 : 0]);
}

/** 9 bytes: command id then the phone unix time as u64 LE (SECONDS). */
export function buildTimeSetPayload(unixSeconds: number): Uint8Array {
  const out = new Uint8Array(9);
  out[0] = CTRL_CMD_TIME_SET;
  new DataView(out.buffer).setBigUint64(1, BigInt(Math.max(0, Math.floor(unixSeconds))), true);
  return out;
}

/**
 * Reads the optional FILE_DONE time trailer. Returns the recording start in
 * milliseconds, or null when the trailer is absent or the device had no
 * anchor (start_unix_s == 0).
 */
export function parseFileDoneTime(payload: Uint8Array): number | null {
  if (payload.length < FILE_DONE_TIME_LEN) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const unixSeconds = view.getBigUint64(FILE_DONE_TIME_OFFSET, true);
  return unixSeconds > 0n ? Number(unixSeconds) * 1000 : null;
}

export function buildFileDeletePayload(path: string): Uint8Array {
  const name = textEncoder.encode(path);
  const out = new Uint8Array(1 + name.length);
  out[0] = CTRL_CMD_FILE_DELETE;
  out.set(name, 1);
  return out;
}

export function buildStorageErasePayload(step: number): Uint8Array {
  return new Uint8Array([CTRL_CMD_STORAGE_ERASE, step & 0xff]);
}

export function buildFileFetchPayload(path: string): Uint8Array {
  const name = textEncoder.encode(path);
  const out = new Uint8Array(1 + name.length);
  out[0] = CTRL_CMD_FILE_FETCH;
  out.set(name, 1);
  return out;
}

export function buildListReqPayload(start: number): Uint8Array {
  const out = new Uint8Array(2);
  new DataView(out.buffer).setUint16(0, Math.max(0, start) & 0xffff, true);
  return out;
}

export function ctrlStatusText(status: number): string {
  switch (status) {
    case CTRL_OK:
      return 'ok';
    case CTRL_ERR_NOT_READY:
      return 'not-ready';
    case CTRL_ERR_NO_SD:
      return 'no-sd';
    case CTRL_ERR_BAD_ARG:
      return 'bad-arg';
    case CTRL_ERR_DENIED:
      return 'denied';
    case CTRL_ERR_BUSY:
      return 'busy';
    case CTRL_ERR_NOT_FOUND:
      return 'not-found';
    default:
      return `0x${(status & 0xff).toString(16).padStart(2, '0')}`;
  }
}

export function fileStateLabel(flags: number): string {
  if (flags & CTRL_LIST_FLAG_ACTIVE) return 'recording';
  if (flags & CTRL_LIST_FLAG_PENDING) return 'pending';
  return 'synced';
}

export function formatBytes(value: number): string {
  if (value >= 1 << 30) return `${(value / (1 << 30)).toFixed(2)}GB`;
  if (value >= 1 << 20) return `${(value / (1 << 20)).toFixed(1)}MB`;
  if (value >= 1 << 10) return `${(value / (1 << 10)).toFixed(0)}KB`;
  return `${value}B`;
}

const UUID_PATTERN =
  /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

/** Canonical UUID check for the ingestion user identity (server parses the same form). */
export function isValidUserId(raw: string): boolean {
  return UUID_PATTERN.test(raw.trim());
}
