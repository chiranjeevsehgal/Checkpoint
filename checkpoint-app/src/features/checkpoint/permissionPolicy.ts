export type GateResult = 'granted' | 'denied' | 'needs-settings';

const BLUETOOTH_SCAN = 'android.permission.BLUETOOTH_SCAN';
const BLUETOOTH_CONNECT = 'android.permission.BLUETOOTH_CONNECT';
const FINE_LOCATION = 'android.permission.ACCESS_FINE_LOCATION';

// Numeric copies of react-native-ble-plx BleErrorCode, kept as plain numbers
// so this module stays importable (and unit-testable) without the BLE stack.
const SCAN_BT_UNAUTHORIZED = 101;
const SCAN_BT_POWERED_OFF = 102;
const SCAN_BT_UNKNOWN_STATE = 103;
const SCAN_BT_RESETTING = 104;
const SCAN_START_FAILED = 600;
const SCAN_LOCATION_DISABLED = 601;

/** Runtime permissions needed for BLE scanning. Empty outside Android. */
export function requiredPermissions(platform: string, apiLevel: number): string[] {
  if (platform !== 'android') return [];
  // NOTE: our manifest declares BLUETOOTH_SCAN *without* neverForLocation,
  // so on API 31+ the OS treats scanning as location-deriving and also
  // requires FINE_LOCATION (enforced strictly on Samsung devices).
  if (apiLevel >= 31) return [BLUETOOTH_SCAN, BLUETOOTH_CONNECT, FINE_LOCATION];
  return [FINE_LOCATION];
}

/**
 * Fold per-permission outcomes into one verdict. A permanently blocked
 * ("never ask again") permission outranks a plain denial because only the
 * system Settings screen can recover it.
 */
export function summarizeGrants(granted: boolean[], blocked: boolean[]): GateResult {
  if (blocked.some(Boolean)) return 'needs-settings';
  return granted.every(Boolean) ? 'granted' : 'denied';
}

/** Human cause for a ble-plx scan-callback failure. Never throws. */
export function describeScanError(errorCode: number, fallback: string): string {
  switch (errorCode) {
    case SCAN_BT_POWERED_OFF:
      return 'Bluetooth is off — turn it on and retry';
    case SCAN_BT_UNKNOWN_STATE:
    case SCAN_BT_RESETTING:
      return 'Bluetooth is resetting — retrying';
    case SCAN_START_FAILED:
      return 'Bluetooth scan could not start — retrying shortly';
    case SCAN_BT_UNAUTHORIZED:
      return 'missing Bluetooth permission — grant Nearby devices and retry';
    case SCAN_LOCATION_DISABLED:
      return 'Location services are off — turn them on and retry';
    default:
      return fallback || `scan error ${errorCode}`;
  }
}
