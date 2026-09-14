import { ApiError, apiFetch } from '@/lib/api/api-client';

export interface DeviceOwnership {
  device_id: string;
  state: string;
  claimed_at?: string;
}

export async function getDevice(baseUrl: string, token: string): Promise<DeviceOwnership | null> {
  try {
    return await apiFetch<DeviceOwnership>(`${baseUrl}/v1/device`, {}, token);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

export async function claimDevice(
  baseUrl: string,
  token: string,
  deviceId: string,
  cloudClaimSecret: string,
): Promise<DeviceOwnership> {
  return apiFetch<DeviceOwnership>(
    `${baseUrl}/v1/device/claim`,
    {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, cloud_claim_secret: cloudClaimSecret }),
    },
    token,
  );
}

export async function releaseDevice(baseUrl: string, token: string): Promise<void> {
  await apiFetch<{ status: string }>(`${baseUrl}/v1/device/release`, { method: 'POST' }, token);
}
