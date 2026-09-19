// SecureStore (native) only accepts non-empty keys of alphanumerics plus
// ".", "-" and "_". All store keys in this app are built here so the format
// is defined once and stays valid on every platform.

const STORE_KEY_PATTERN = /^[A-Za-z0-9._-]+$/;

export function isValidStoreKey(key: string): boolean {
  return STORE_KEY_PATTERN.test(key);
}

export function assertValidStoreKey(key: string): void {
  if (!isValidStoreKey(key)) {
    throw new Error(`Invalid store key ${JSON.stringify(key)}: use only A-Z a-z 0-9 . - _`);
  }
}

export function settingsKey(name: string): string {
  return `checkpoint.settings.${name}`;
}

export function prefKey(name: string): string {
  return `checkpoint.pref.${name}`;
}

export function sessionKey(): string {
  return 'checkpoint.session';
}

export function credentialKey(identityId: string, deviceIdHex: string): string {
  return `Checkpoint.${identityId}.${deviceIdHex}`;
}

export function enrolledDeviceKey(identityId: string): string {
  return `checkpoint.${identityId}.enrolledDeviceId`;
}

export function notificationSubscriptionKey(): string {
  return 'checkpoint.notifications.subscription';
}
