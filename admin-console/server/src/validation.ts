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
