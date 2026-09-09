export const env = {
  apiUrl: process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8080',
  appEnv: process.env.EXPO_PUBLIC_ENV ?? 'development',
} as const;

// Anything WITHOUT the EXPO_PUBLIC_ prefix is never exposed to the client.
// Never read secrets here — this object is bundled into the app.
