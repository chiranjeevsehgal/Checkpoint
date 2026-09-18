import { apiFetch } from './api-client';

export interface LanguageOption {
  code: string;
  name: string;
}

export interface UserSettings {
  languages: string[];
  timezone?: string;
  available: LanguageOption[];
}

export interface UserSettingsPatch {
  languages?: string[];
  timezone?: string;
}

export interface UpdatedUserSettings {
  languages: string[];
  timezone: string;
}

export async function getUserSettings(token: string): Promise<UserSettings> {
  return apiFetch<UserSettings>('/v1/me/settings', {}, token);
}

/** Sends only the provided fields; the server leaves the others untouched. */
export async function putUserSettings(
  token: string,
  patch: UserSettingsPatch,
): Promise<UpdatedUserSettings> {
  return apiFetch<UpdatedUserSettings>(
    '/v1/me/settings',
    { method: 'PUT', body: JSON.stringify(patch) },
    token,
  );
}
