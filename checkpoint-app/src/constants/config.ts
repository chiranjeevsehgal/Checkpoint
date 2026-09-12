import { env } from '@/lib/env';

export const config = {
  apiUrl: env.apiUrl,
  env: env.appEnv,
} as const;
