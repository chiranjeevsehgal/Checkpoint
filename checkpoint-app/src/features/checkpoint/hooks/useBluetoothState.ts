import { useSyncExternalStore } from 'react';

import { syncEngine, type BluetoothStatus } from '../syncEngine.ts';

export function useBluetoothState(): BluetoothStatus {
  const snapshot = useSyncExternalStore(syncEngine.subscribe, syncEngine.getSnapshot);
  return snapshot.bluetooth;
}
