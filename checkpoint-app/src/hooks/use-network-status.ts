import { useEffect, useState } from 'react';
import { Platform } from 'react-native';

export function useNetworkStatus(): boolean {
  const [online, setOnline] = useState(() =>
    Platform.OS === 'web' && typeof window !== 'undefined' ? window.navigator.onLine : true
  );

  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const onOnline = () => setOnline(true);
    const offOnline = () => setOnline(false);
    window.addEventListener('online', onOnline);
    window.addEventListener('offline', offOnline);
    return () => {
      window.removeEventListener('online', onOnline);
      window.removeEventListener('offline', offOnline);
    };
  }, []);

  return online;
}
