import {
  CTRL_CMD_CLEAR_TRUSTED_SLOTS,
  CTRL_CMD_FORGET_SELF,
  CTRL_CMD_GET_CLOUD_SECRET,
  CTRL_ERR_NOT_READY,
  PKT_CMD,
  PKT_LIST_REQ,
  PKT_STATUS_REQ,
  PKT_STORAGE_REQ,
} from './config.ts';
import {
  buildFileDeletePayload,
  buildFileFetchPayload,
  buildLedSetPayload,
  buildListReqPayload,
  buildStorageErasePayload,
  buildSyncSetPayload,
  buildTimeSetPayload,
  type CmdResponse,
} from './parsers.ts';
import type { DeviceFileList, DeviceStatus, StorageInfo } from './types.ts';

/** Minimal transport surface the device commands need from the BLE client. */
export interface CommandTransport {
  roundtrip(
    type: number,
    payload: Uint8Array,
    timeoutMs?: number,
  ): Promise<CmdResponse | DeviceStatus | StorageInfo | DeviceFileList>;
}

function statusOf(res: CmdResponse): number {
  return res.status ?? CTRL_ERR_NOT_READY;
}

export async function cmdRecStart(transport: CommandTransport): Promise<number> {
  const res = (await transport.roundtrip(PKT_CMD, new Uint8Array([0x01]))) as CmdResponse;
  return statusOf(res);
}

export async function cmdRecStop(transport: CommandTransport): Promise<number> {
  const res = (await transport.roundtrip(PKT_CMD, new Uint8Array([0x02]))) as CmdResponse;
  return statusOf(res);
}

export async function cmdLedSet(
  transport: CommandTransport,
  muted: boolean,
  brightness: number,
): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    buildLedSetPayload(muted, brightness),
  )) as CmdResponse;
  return statusOf(res);
}

export async function cmdLedGet(transport: CommandTransport): Promise<CmdResponse> {
  return (await transport.roundtrip(PKT_CMD, new Uint8Array([0x11]))) as CmdResponse;
}

export async function cmdSyncSet(
  transport: CommandTransport,
  enabled: boolean,
): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    buildSyncSetPayload(enabled),
  )) as CmdResponse;
  return statusOf(res);
}

export async function cmdSyncGet(transport: CommandTransport): Promise<CmdResponse> {
  return (await transport.roundtrip(PKT_CMD, new Uint8Array([0x13]))) as CmdResponse;
}

export async function cmdTimeSet(
  transport: CommandTransport,
  unixSeconds: number,
): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    buildTimeSetPayload(unixSeconds),
  )) as CmdResponse;
  return statusOf(res);
}

export async function reqStatus(transport: CommandTransport): Promise<DeviceStatus> {
  const res = await transport.roundtrip(PKT_STATUS_REQ, new Uint8Array(0));
  if (!('recording' in res)) throw new Error('Bad STATUS_RESP');
  return res as DeviceStatus;
}

export async function reqStorage(transport: CommandTransport): Promise<StorageInfo> {
  const res = await transport.roundtrip(PKT_STORAGE_REQ, new Uint8Array(0));
  if (!('total' in res)) throw new Error('Bad STORAGE_RESP');
  return res as StorageInfo;
}

export async function reqList(
  transport: CommandTransport,
  start = 0,
): Promise<DeviceFileList> {
  const res = await transport.roundtrip(PKT_LIST_REQ, buildListReqPayload(start));
  if (!('entries' in res)) throw new Error('Bad LIST_RESP');
  return res as DeviceFileList;
}

export async function cmdFileDelete(
  transport: CommandTransport,
  path: string,
): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    buildFileDeletePayload(path),
  )) as CmdResponse;
  return statusOf(res);
}

export async function cmdFileFetch(
  transport: CommandTransport,
  path: string,
): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    buildFileFetchPayload(path),
  )) as CmdResponse;
  return statusOf(res);
}

export async function cmdStorageErase(
  transport: CommandTransport,
  step: number,
): Promise<CmdResponse> {
  return (await transport.roundtrip(
    PKT_CMD,
    buildStorageErasePayload(step),
    10000,
  )) as CmdResponse;
}

export async function getCloudSecret(
  transport: CommandTransport,
): Promise<Uint8Array | null> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    new Uint8Array([CTRL_CMD_GET_CLOUD_SECRET]),
  )) as CmdResponse;
  return res.status === 0 ? (res.secret ?? null) : null;
}

export async function clearTrustedSlots(transport: CommandTransport): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    new Uint8Array([CTRL_CMD_CLEAR_TRUSTED_SLOTS]),
  )) as CmdResponse;
  return statusOf(res);
}

// Drops this phone's own trusted slot on the pendant (used when the backend
// refuses ownership). Older firmware acks BAD_ARG; callers treat it best-effort.
export async function forgetSelf(transport: CommandTransport): Promise<number> {
  const res = (await transport.roundtrip(
    PKT_CMD,
    new Uint8Array([CTRL_CMD_FORGET_SELF]),
  )) as CmdResponse;
  return statusOf(res);
}
