import { PermissionsAndroid, Platform, type Permission } from "react-native";

import {
  requiredPermissions,
  summarizeGrants,
  type GateResult,
} from "./permissionPolicy.ts";

/**
 * Request Android BLE runtime permissions. No-op off Android (iOS permission
 * is declared in InfoPlist and granted via the OS pairing dialog instead).
 */
export async function ensureBlePermissions(): Promise<GateResult> {
  if (Platform.OS !== "android") return "granted";
  const apiLevel = typeof Platform.Version === "number" ? Platform.Version : 0;
  const wanted = requiredPermissions(Platform.OS, apiLevel) as Permission[];
  const results = await PermissionsAndroid.requestMultiple(wanted);
  const granted = wanted.map(
    (name) =>
      (results[name] ?? PermissionsAndroid.RESULTS.DENIED) ===
      PermissionsAndroid.RESULTS.GRANTED,
  );
  const blocked = wanted.map(
    (name) =>
      (results[name] ?? PermissionsAndroid.RESULTS.DENIED) ===
      PermissionsAndroid.RESULTS.NEVER_ASK_AGAIN,
  );
  return summarizeGrants(granted, blocked);
}
