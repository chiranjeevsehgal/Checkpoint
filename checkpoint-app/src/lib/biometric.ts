/**
 * Device-owner check shown before revealing an MCP access key or confirming
 * account deletion. Uses the OS biometric / PIN prompt when the device
 * supports it; devices without biometric enrollment fall through so the
 * actions stay usable.
 */
interface LocalAuthModule {
  hasHardwareAsync(): Promise<boolean>;
  isEnrolledAsync(): Promise<boolean>;
  authenticateAsync(options: {
    promptMessage: string;
    cancelLabel: string;
    disableDeviceFallback: boolean;
  }): Promise<{ success: boolean }>;
}

export async function requireDeviceCheck(reason: string): Promise<boolean> {
  let auth: LocalAuthModule;
  try {
    auth = (await import('expo-local-authentication')) as unknown as LocalAuthModule;
  } catch {
    return true;
  }
  try {
    const [hasHardware, enrolled] = await Promise.all([
      auth.hasHardwareAsync(),
      auth.isEnrolledAsync(),
    ]);
    if (!hasHardware || !enrolled) return true;
    const result = await auth.authenticateAsync({
      promptMessage: reason,
      cancelLabel: 'Cancel',
      disableDeviceFallback: false,
    });
    return result.success;
  } catch {
    return false;
  }
}
