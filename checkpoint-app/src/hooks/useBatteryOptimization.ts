import * as Battery from 'expo-battery';
import { useEffect, useState } from 'react';
import { AppState, Platform } from 'react-native';

/**
 * Whether Android battery optimization is enabled for this app (true = the app
 * is restricted from running in the background). Re-checks when the app returns
 * to the foreground so the UI updates after the user changes the setting.
 */
export function useBatteryOptimization(): boolean | null {
  const [restricted, setRestricted] = useState<boolean | null>(
    Platform.OS === 'android' ? null : false,
  );

  useEffect(() => {
    if (Platform.OS !== 'android') return;
    let cancelled = false;
    const check = async () => {
      try {
        const value = await Battery.isBatteryOptimizationEnabledAsync();
        if (!cancelled) setRestricted(value);
      } catch {
        if (!cancelled) setRestricted(null);
      }
    };
    void check();
    const subscription = AppState.addEventListener('change', (state) => {
      if (state === 'active') void check();
    });
    return () => {
      cancelled = true;
      subscription.remove();
    };
  }, []);

  return restricted;
}
