export const config = {
  apiUrl: process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8080',
  env: process.env.EXPO_PUBLIC_ENV ?? 'development',
} as const;
