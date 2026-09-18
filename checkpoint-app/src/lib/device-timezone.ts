import { getCalendars } from 'expo-localization';

/**
 * Returns the device's IANA timezone id (e.g. "Europe/Berlin"), or null when
 * the platform does not expose one (notably web).
 */
export function getDeviceTimeZone(): string | null {
  const [calendar] = getCalendars();
  const timeZone = calendar?.timeZone?.trim();
  return timeZone && timeZone !== '' ? timeZone : null;
}
