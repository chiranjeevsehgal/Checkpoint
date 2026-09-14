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
// The API and Kratos run on the same machine, so default to its LAN address
// instead of localhost, which is unreachable from hardware. An explicit
// non-local EXPO_PUBLIC_* value always wins; a saved in-app Settings value
// wins over this for the API URL.
function debuggerHostUrl(port: number): string | null {
  const hostUri = Constants.expoConfig?.hostUri;
  if (!hostUri) return null;
  const host = hostUri.split(':')[0] ?? '';
  if (isLanIp(host)) return `http://${host}:${port}`;
  // Android emulator: host loopback is reachable at 10.0.2.2.
  if (Platform.OS === 'android' && (host === 'localhost' || host === '127.0.0.1')) {
    return `http://10.0.2.2:${port}`;
  }
  return null;
}

function defaultApiUrl(): string {
  const configured = process.env.EXPO_PUBLIC_API_URL;
  if (configured && !isLocalUrl(configured)) return configured;
  return debuggerHostUrl(8080) ?? configured ?? FALLBACK_API_URL;
}

function defaultKratosUrl(): string {
  const configured = process.env.EXPO_PUBLIC_KRATOS_URL;
  if (configured && !isLocalUrl(configured)) return configured;
  return debuggerHostUrl(4433) ?? configured ?? 'http://localhost:4433';
}

export const env = {
  apiUrl: defaultApiUrl(),
  kratosUrl: defaultKratosUrl(),
  appEnv: process.env.EXPO_PUBLIC_ENV ?? 'development',
  devBuild: process.env.EXPO_PUBLIC_DEV_BUILD === '1',
} as const;

// Anything WITHOUT the EXPO_PUBLIC_ prefix is never exposed to the client.
// Never read secrets here — this object is bundled into the app.
