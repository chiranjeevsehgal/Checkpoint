import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  describeScanError,
  requiredPermissions,
  summarizeGrants,
} from "../permissionPolicy.ts";

describe("requiredPermissions", () => {
  it("requests scan+connect on Android 12+ (API 31)", () => {
    assert.deepEqual(requiredPermissions("android", 31), [
      "android.permission.BLUETOOTH_SCAN",
      "android.permission.BLUETOOTH_CONNECT",
    ]);
  });

  it("requests scan+connect on newer APIs too", () => {
    assert.equal(requiredPermissions("android", 36).length, 2);
  });

  it("requests fine location below API 31", () => {
    assert.deepEqual(requiredPermissions("android", 30), [
      "android.permission.ACCESS_FINE_LOCATION",
    ]);
  });

  it("requests nothing off Android", () => {
    assert.deepEqual(requiredPermissions("ios", 18), []);
    assert.deepEqual(requiredPermissions("web", 0), []);
  });
});

describe("summarizeGrants", () => {
  it("grants when every permission is granted", () => {
    assert.equal(summarizeGrants([true, true], [false, false]), "granted");
  });

  it("denies on a plain denial", () => {
    assert.equal(summarizeGrants([true, false], [false, false]), "denied");
  });

  it("prefers needs-settings when permanently blocked", () => {
    assert.equal(summarizeGrants([true, false], [false, true]), "needs-settings");
  });

  it("grants vacuously with no permissions wanted", () => {
    assert.equal(summarizeGrants([], []), "granted");
  });
});

describe("describeScanError", () => {
  it("maps known ble-plx codes", () => {
    assert.match(describeScanError(102, "fallback"), /Bluetooth is off/);
    assert.match(describeScanError(101, "fallback"), /Nearby devices/);
    assert.match(describeScanError(601, "fallback"), /Location services/);
  });

  it("falls back to the native message", () => {
    assert.equal(describeScanError(999, "weird failure"), "weird failure");
  });

  it("falls back to the code when messageless", () => {
    assert.equal(describeScanError(999, ""), "scan error 999");
  });
});
