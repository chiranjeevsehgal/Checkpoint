import { apiFetch } from './api-client';

export interface LanguageOption {
  code: string;
  name: string;
}

export interface UserSettings {
  languages: string[];
  available: LanguageOption[];
}

export async function getUserSettings(token: string): Promise<UserSettings> {
  return apiFetch<UserSettings>('/v1/me/settings', {}, token);
}

export async function putUserSettings(token: string, languages: string[]): Promise<string[]> {
  const result = await apiFetch<{ languages: string[] }>(
    '/v1/me/settings',
    { method: 'PUT', body: JSON.stringify({ languages }) },
    token,
  );
  return result.languages;
}
