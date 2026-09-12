import { openSettings } from 'expo-linking';
import type { PropsWithChildren } from 'react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from 'react';

import { CTRL_OK, DEVICE_NAME } from '../config.ts';
import { networkMonitor } from '../networkMonitor.ts';
import {
  defaultSettings,
  loadSettings,
  saveSettings,
  type CheckpointSettings,
} from '../settings.ts';
import { syncEngine, type ListPage } from '../syncEngine.ts';
import type { TransferRecord } from '../transferStore.ts';
import type { DeviceFileList, DeviceStatus, StorageInfo } from '../types.ts';

import { ConfirmDialog } from '@/components/shared/confirm-dialog';
import { useToast } from '@/providers/toast-provider';

type DialogState = { kind: 'delete'; path: string } | { kind: 'erase' } | null;

interface CheckpointContextValue {
  connected: boolean;
  busy: boolean;
  linkState: string;
  deviceId: string | null;
  autoConnecting: boolean;
  deviceName: string;
  setDeviceName: (name: string) => void;
  claimText: string;
  setClaimText: (text: string) => void;
  status: DeviceStatus | null;
  storage: StorageInfo | null;
  fileList: DeviceFileList | null;
  listPage: ListPage;
  transfers: TransferRecord[];
  logs: string[];
  settings: CheckpointSettings;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  stopAutoConnect: () => Promise<void>;
  refreshStatus: () => Promise<void>;
  refreshStorage: () => Promise<void>;
  refreshTransfers: () => Promise<void>;
  listPrev: () => Promise<void>;
  listNext: () => Promise<void>;
  toggleRec: () => Promise<void>;
  applyLed: (muted: boolean, brightness: number) => Promise<void>;
  applySync: (enabled: boolean) => Promise<void>;
  requestDelete: (path: string) => void;
  requestErase: () => void;
  updateSettings: (settings: CheckpointSettings) => Promise<void>;
  shareBench: () => Promise<void>;
  clearLogs: () => void;
  needsSettings: boolean;
  openAppSettings: () => Promise<void>;
  testConnection: () => Promise<void>;
}

const CheckpointContext = createContext<CheckpointContextValue | null>(null);

export function useCheckpoint(): CheckpointContextValue {
  const value = useContext(CheckpointContext);
  if (!value) throw new Error('useCheckpoint must be used inside CheckpointProvider');
  return value;
}

export function CheckpointProvider({ children }: PropsWithChildren) {
  const { showToast } = useToast();
  const snapshot = useSyncExternalStore(syncEngine.subscribe, syncEngine.getSnapshot);
  const [deviceName, setDeviceName] = useState(DEVICE_NAME);
  const [claimText, setClaimText] = useState('');
  const [settings, setSettings] = useState<CheckpointSettings>(defaultSettings);
  const [dialog, setDialog] = useState<DialogState>(null);

  useEffect(() => {
    void loadSettings().then((loaded) => {
      setSettings(loaded);
      syncEngine.configure(loaded);
      networkMonitor.configure(loaded.serverUrl);
      void syncEngine.start();
      void networkMonitor.start();
    });
    return () => {
      void syncEngine.stop();
      networkMonitor.stop();
    };
  }, []);

  const connect = useCallback(async () => {
    syncEngine.configure(settings);
    await syncEngine.connect(deviceName, claimText);
  }, [claimText, deviceName, settings]);

  const disconnect = useCallback(async () => {
    await syncEngine.disconnect();
  }, []);

  const applyLed = useCallback(
    async (muted: boolean, brightness: number) => {
      const code = await syncEngine.applyLed(muted, brightness);
      if (code === CTRL_OK) showToast('LED settings applied.');
    },
    [showToast],
  );

  const applySync = useCallback(
    async (enabled: boolean) => {
      const code = await syncEngine.applySync(enabled);
      if (code === CTRL_OK) {
        showToast(`Auto-sync ${enabled ? 'enabled' : 'disabled'}.`);
      }
    },
    [showToast],
  );

  const deleteFile = useCallback(
    async (path: string) => {
      const code = await syncEngine.deleteFile(path);
      if (code === CTRL_OK) showToast('File deleted.');
    },
    [showToast],
  );

  const eraseStorage = useCallback(async () => {
    const code = await syncEngine.eraseStorage();
    if (code === CTRL_OK) showToast('All recordings erased.');
  }, [showToast]);

  const updateSettings = useCallback(
    async (next: CheckpointSettings) => {
      await saveSettings(next);
      setSettings(next);
      syncEngine.configure(next);
      networkMonitor.configure(next.serverUrl);
      await syncEngine.applyAutoSyncIfConnected();
      showToast('Saved.');
    },
    [showToast],
  );

  const testConnection = useCallback(async () => {
    const ok = await syncEngine.testConnection();
    showToast(ok ? 'Server reachable.' : 'Server unreachable — see debug log.');
  }, [showToast]);

  const openAppSettings = useCallback(async () => {
    try {
      await openSettings();
    } catch (error) {
      console.warn(
        `[ui] open settings failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
    }
  }, []);

  const requestDelete = useCallback((path: string) => setDialog({ kind: 'delete', path }), []);
  const requestErase = useCallback(() => setDialog({ kind: 'erase' }), []);
  const cancelDialog = useCallback(() => setDialog(null), []);

  const confirmDialog = useCallback(() => {
    const pending = dialog;
    setDialog(null);
    if (pending?.kind === 'delete') void deleteFile(pending.path);
    if (pending?.kind === 'erase') void eraseStorage();
  }, [dialog, deleteFile, eraseStorage]);

  const value = useMemo<CheckpointContextValue>(
    () => ({
      connected: snapshot.connected,
      busy: snapshot.busy,
      linkState: snapshot.linkState,
      deviceId: snapshot.deviceId,
      autoConnecting: snapshot.autoConnecting,
      deviceName,
      setDeviceName,
      claimText,
      setClaimText,
      status: snapshot.status,
      storage: snapshot.storage,
      fileList: snapshot.fileList,
      listPage: snapshot.listPage,
      transfers: snapshot.transfers,
      logs: snapshot.logs,
      settings,
      connect,
      disconnect,
      stopAutoConnect: syncEngine.stopAutoConnect,
      refreshStatus: syncEngine.refreshStatus,
      refreshStorage: syncEngine.refreshStorage,
      refreshTransfers: syncEngine.refreshTransfers,
      listPrev: syncEngine.listPrev,
      listNext: syncEngine.listNext,
      toggleRec: syncEngine.toggleRec,
      applyLed,
      applySync,
      requestDelete,
      requestErase,
      updateSettings,
      shareBench: syncEngine.shareBench,
      clearLogs: syncEngine.clearLogs,
      needsSettings: snapshot.needsSettings,
      openAppSettings,
      testConnection,
    }),
    [
      applyLed,
      applySync,
      claimText,
      connect,
      deviceName,
      disconnect,
      openAppSettings,
      requestDelete,
      requestErase,
      settings,
      snapshot,
      testConnection,
      updateSettings,
    ],
  );

  const dialogFile = dialog?.kind === 'delete' ? dialog.path : '';

  return (
    <CheckpointContext.Provider value={value}>
      {children}
      <ConfirmDialog
        visible={dialog !== null}
        title={dialog?.kind === 'erase' ? 'Erase all recordings' : 'Delete file'}
        body={
          dialog?.kind === 'erase'
            ? 'Erase ALL recordings from the pendant SD card? This cannot be undone. Type ERASE to confirm.'
            : `Delete ${dialogFile} from the pendant? This cannot be undone.`
        }
        confirmLabel={dialog?.kind === 'erase' ? 'Erase all' : 'Delete'}
        requireText={dialog?.kind === 'erase' ? 'ERASE' : undefined}
        onCancel={cancelDialog}
        onConfirm={confirmDialog}
      />
    </CheckpointContext.Provider>
  );
}
