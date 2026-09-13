import Constants from 'expo-constants';
import { Platform } from 'react-native';

const FALLBACK_API_URL = 'http://localhost:8080';

function isLocalUrl(url: string): boolean {
  return /^(https?:\/\/)?(localhost|127\.0\.0\.1)(:\d+)?(\/|$)/.test(url.trim());
}

function isLanIp(host: string): boolean {
  return /^\d+\.\d+\.\d+\.\d+$/.test(host) && host !== '127.0.0.1';
}

// In dev, Metro tells the app where the bundler lives (e.g. 192.168.1.4:8081).
// The API runs on the same machine, so default to it instead of localhost,
// which is unreachable from hardware. An explicit non-local EXPO_PUBLIC_API_URL
// always wins; a saved in-app Settings value wins over all of this.
function debuggerApiUrl(): string | null {
  const hostUri = Constants.expoConfig?.hostUri;
  if (!hostUri) return null;
  const host = hostUri.split(':')[0] ?? '';
  if (isLanIp(host)) return `http://${host}:8080`;
  // Android emulator: host loopback is reachable at 10.0.2.2.
  if (Platform.OS === 'android' && (host === 'localhost' || host === '127.0.0.1')) {
    return 'http://10.0.2.2:8080';
  }
  return null;
}

function defaultApiUrl(): string {
  const configured = process.env.EXPO_PUBLIC_API_URL;
  if (configured && !isLocalUrl(configured)) return configured;
  return debuggerApiUrl() ?? configured ?? FALLBACK_API_URL;
}

export const env = {
  apiUrl: defaultApiUrl(),
  appEnv: process.env.EXPO_PUBLIC_ENV ?? 'development',
  devBuild: process.env.EXPO_PUBLIC_DEV_BUILD === '1',
} as const;

// Anything WITHOUT the EXPO_PUBLIC_ prefix is never exposed to the client.
// Never read secrets here — this object is bundled into the app.
