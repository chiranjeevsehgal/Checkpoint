import { useSyncExternalStore } from 'react';

import { networkMonitor, type NetworkSnapshot } from '../networkMonitor.ts';

export function useNetworkStatus(): NetworkSnapshot {
  return useSyncExternalStore(networkMonitor.subscribe, networkMonitor.getSnapshot);
}
