import { bytesToHex, hexToBytes } from './crypto.ts';

import { getIdentityId } from '@/lib/session';
import { storage } from '@/lib/storage';
import { credentialKey, enrolledDeviceKey } from '@/lib/storage/keys';

export interface DeviceCredential {
  clientId: string;
  clientKey: string;
  pending: boolean;
}

function requireIdentityId(): string {
  const identityId = getIdentityId();
  if (!identityId) throw new Error('Not signed in.');
  return identityId;
}

function keyForDevice(deviceId: Uint8Array): string {
  return credentialKey(requireIdentityId(), bytesToHex(deviceId));
}

function encodeCredential(clientId: Uint8Array, clientKey: Uint8Array, pending: boolean): string {
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
    if (typeof record.clientId !== 'string' || typeof record.clientKey !== 'string') {
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
  await storage.set(keyForDevice(deviceId), encodeCredential(clientId, clientKey, pending));
}

export async function loadCredential(
  deviceId: Uint8Array,
): Promise<{ clientId: Uint8Array; clientKey: Uint8Array } | null> {
  const record = decodeCredential(await storage.get(keyForDevice(deviceId)));
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

export async function isCredentialPending(deviceId: Uint8Array): Promise<boolean> {
  const record = decodeCredential(await storage.get(keyForDevice(deviceId)));
  return record?.pending === true;
}

export async function markCredentialActive(deviceId: Uint8Array): Promise<void> {
  const key = keyForDevice(deviceId);
  const record = decodeCredential(await storage.get(key));
  if (record?.pending) {
    await storage.set(key, JSON.stringify({ ...record, pending: false }));
  }
}

export async function deleteCredential(deviceId: Uint8Array): Promise<void> {
  await storage.remove(keyForDevice(deviceId));
}

export async function setEnrolledDeviceId(deviceIdHex: string): Promise<void> {
  await storage.set(enrolledDeviceKey(requireIdentityId()), deviceIdHex);
}

export async function getEnrolledDeviceId(): Promise<string | null> {
  return storage.get(enrolledDeviceKey(requireIdentityId()));
}

export async function clearEnrolledDeviceId(): Promise<void> {
  await storage.remove(enrolledDeviceKey(requireIdentityId()));
}
