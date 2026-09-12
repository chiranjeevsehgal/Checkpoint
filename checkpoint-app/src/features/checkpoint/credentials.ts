import { storage } from '@/lib/storage';
import { credentialKey } from '@/lib/storage/keys';

import { bytesToHex, hexToBytes } from './crypto.ts';

const ENROLLED_DEVICE_KEY = "checkpoint.enrolledDeviceId";

export interface DeviceCredential {
  clientId: string;
  clientKey: string;
  pending: boolean;
}

function encodeCredential(
  clientId: Uint8Array,
  clientKey: Uint8Array,
  pending: boolean,
): string {
  const record: DeviceCredential = {
    clientId: bytesToHex(clientId),
    clientKey: bytesToHex(clientKey),
    pending,
  };
  return JSON.stringify(record);
}

function decodeCredential(raw: string | null): DeviceCredential | null {
  if (!raw) return null;
  try {
    const record = JSON.parse(raw) as Partial<DeviceCredential>;
    if (
      typeof record.clientId !== "string" ||
      typeof record.clientKey !== "string"
    ) {
      return null;
    }
    return {
      clientId: record.clientId,
      clientKey: record.clientKey,
      pending: record.pending === true,
    };
  } catch {
    return null;
  }
}

export async function saveCredential(
  deviceId: Uint8Array,
  clientId: Uint8Array,
  clientKey: Uint8Array,
  pending = false,
): Promise<void> {
  await storage.set(
    credentialKey(bytesToHex(deviceId)),
    encodeCredential(clientId, clientKey, pending),
  );
}

export async function loadCredential(
  deviceId: Uint8Array,
): Promise<{ clientId: Uint8Array; clientKey: Uint8Array } | null> {
  const record = decodeCredential(
    await storage.get(credentialKey(bytesToHex(deviceId))),
  );
  if (!record) return null;
  try {
    return {
      clientId: hexToBytes(record.clientId),
      clientKey: hexToBytes(record.clientKey),
    };
  } catch {
    return null;
  }
}

export async function isCredentialPending(
  deviceId: Uint8Array,
): Promise<boolean> {
  const record = decodeCredential(
    await storage.get(credentialKey(bytesToHex(deviceId))),
  );
  return record?.pending === true;
}

export async function markCredentialActive(
  deviceId: Uint8Array,
): Promise<void> {
  const key = credentialKey(bytesToHex(deviceId));
  const record = decodeCredential(await storage.get(key));
  if (record && record.pending) {
    await storage.set(
      key,
      JSON.stringify({ ...record, pending: false }),
    );
  }
}

export async function deleteCredential(
  deviceId: Uint8Array,
): Promise<void> {
  await storage.remove(credentialKey(bytesToHex(deviceId)));
}

export async function setEnrolledDeviceId(deviceIdHex: string): Promise<void> {
  await storage.set(ENROLLED_DEVICE_KEY, deviceIdHex);
}

export async function getEnrolledDeviceId(): Promise<string | null> {
  return storage.get(ENROLLED_DEVICE_KEY);
}

export async function clearEnrolledDeviceId(): Promise<void> {
  await storage.remove(ENROLLED_DEVICE_KEY);
}
