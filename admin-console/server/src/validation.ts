const DEVICE_ID = /^[0-9a-f]{32}$/;
const CLAIM_HASH = /^[0-9a-f]{64}$/;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isDeviceId(value: string): boolean {
  return DEVICE_ID.test(value.trim());
}

export function isClaimHash(value: string): boolean {
  return CLAIM_HASH.test(value.trim());
}

export function isUuid(value: string): boolean {
  return UUID.test(value.trim());
}

export function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

export function isDatabaseUrl(value: string): boolean {
  return /^postgres(ql)?:\/\/\S+$/.test(value);
}

export function hasNoWhitespace(value: string): boolean {
  return !/\s/.test(value);
}

// Embedded in an SSH command, so keep it to characters safe for a remote shell.
export function isSafeRemotePath(value: string): boolean {
  return /^[A-Za-z0-9_./~-]+$/.test(value);
}
