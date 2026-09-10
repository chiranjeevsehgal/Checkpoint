import { Platform } from 'react-native';

import { assertValidStoreKey } from './keys';
import { storage as nativeStorage } from './storage.native';
import { storage as webStorage } from './storage.web';

export interface KeyValueStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

const backend = Platform.OS === 'web' ? webStorage : nativeStorage;

// Validate up front so a bad key fails here with a clear message instead of
// surfacing as a native SecureStore error deep in a feature flow.
export const storage: KeyValueStore = {
  get(key: string): Promise<string | null> {
    assertValidStoreKey(key);
    return backend.get(key);
  },
  set(key: string, value: string): Promise<void> {
    assertValidStoreKey(key);
    return backend.set(key, value);
  },
  remove(key: string): Promise<void> {
    assertValidStoreKey(key);
    return backend.remove(key);
  },
};
