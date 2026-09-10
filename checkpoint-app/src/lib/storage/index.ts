import { Platform } from 'react-native';

import { storage as nativeStorage } from './storage.native';
import { storage as webStorage } from './storage.web';

export interface KeyValueStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

export const storage: KeyValueStore =
  Platform.OS === 'web' ? webStorage : nativeStorage;
