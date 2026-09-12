import { PermissionsAndroid, Platform, type Permission } from 'react-native';

import { requiredPermissions, summarizeGrants, type GateResult } from './permissionPolicy.ts';

/**
 * Request Android BLE runtime permissions. No-op off Android (iOS permission
 * is declared in InfoPlist and granted via the OS pairing dialog instead).
 */
export async function ensureBlePermissions(): Promise<GateResult> {
  if (Platform.OS !== 'android') return 'granted';
  const wanted = requiredPermissions(Platform.OS, currentApiLevel()) as Permission[];
  const results = await PermissionsAndroid.requestMultiple(wanted);
  const granted = wanted.map(
    (name) =>
      (results[name] ?? PermissionsAndroid.RESULTS.DENIED) === PermissionsAndroid.RESULTS.GRANTED,
  );
  const blocked = wanted.map(
    (name) =>
      (results[name] ?? PermissionsAndroid.RESULTS.DENIED) ===
      PermissionsAndroid.RESULTS.NEVER_ASK_AGAIN,
  );
  return summarizeGrants(granted, blocked);
}

/**
 * Check BLE permissions without prompting. Used by auto-connect on launch so a
 * cold start never shows a permission dialog unrelated to user intent.
 */
export async function hasBlePermissions(): Promise<boolean> {
  if (Platform.OS !== 'android') return true;
  const wanted = requiredPermissions(Platform.OS, currentApiLevel()) as Permission[];
  const checks = await Promise.all(wanted.map((name) => PermissionsAndroid.check(name)));
  return checks.every(Boolean);
}

function currentApiLevel(): number {
  return typeof Platform.Version === 'number' ? Platform.Version : 0;
}
