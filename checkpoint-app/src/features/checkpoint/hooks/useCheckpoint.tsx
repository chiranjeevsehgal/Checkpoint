import { openSettings } from 'expo-linking';
import type { PropsWithChildren } from 'react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';

import { shouldRunSyncService } from '../backgroundPolicy.ts';
import { startSyncService, stopSyncService } from '../backgroundService.ts';
import { CTRL_OK } from '../config.ts';
import { networkMonitor } from '../networkMonitor.ts';
import type { HealthProbe } from '../networkStatus.ts';
import { ctrlStatusText, formatBytes } from '../parsers.ts';
import {
  defaultSettings,
  loadSettings,
  saveSettings,
  type CheckpointSettings,
} from '../settings.ts';
import { syncEngine, type ListPage, type PreviewSnapshot } from '../syncEngine.ts';
import type { TransferRecord } from '../transferStore.ts';
import type { DeviceFileList, DeviceStatus, LogEntry, StorageInfo } from '../types.ts';

import { ConfirmDialog } from '@/components/shared/confirm-dialog';
import { resolveApiUrl, subscribeServerConfig } from '@/lib/server-config';
import { useToast } from '@/providers/toast-provider';

type DialogState =
  | { kind: 'delete'; path: string }
  | { kind: 'erase' }
  | { kind: 'forget' }
  | { kind: 'release' }
  | null;

interface CheckpointContextValue {
  connected: boolean;
  busy: boolean;
  linkState: string;
  hydrated: boolean;
  deviceId: string | null;
  enrolled: boolean;
  ownedDeviceId: string | null;
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
  preview: PreviewSnapshot | null;
  deleting: string | null;
  erasing: boolean;
  logs: LogEntry[];
  settings: CheckpointSettings;
  connect: () => Promise<void>;
  reconnect: () => Promise<void>;
  disconnect: () => Promise<void>;
  forgetDevice: () => Promise<void>;
  releasePendant: () => Promise<void>;
  stopConnection: () => Promise<void>;
  refreshStatus: () => Promise<void>;
  refreshStorage: () => Promise<void>;
  refreshTransfers: () => Promise<void>;
  listPrev: () => Promise<void>;
  listNext: () => Promise<void>;
  toggleRec: () => Promise<void>;
  applyLed: (muted: boolean, brightness: number) => Promise<void>;
  requestDelete: (path: string) => void;
  requestErase: () => void;
  requestForget: () => void;
  requestRelease: () => void;
  previewStorageFile: (path: string) => Promise<number | null>;
  updateSettings: (settings: CheckpointSettings, options?: { silent?: boolean }) => Promise<void>;
  shareBench: () => Promise<void>;
  clearLogs: () => void;
  needsSettings: boolean;
  openAppSettings: () => Promise<void>;
  testConnection: () => Promise<HealthProbe | null>;
}

const CheckpointContext = createContext<CheckpointContextValue | null>(null);

export function useCheckpoint(): CheckpointContextValue {
  const value = useContext(CheckpointContext);
  if (!value) throw new Error('useCheckpoint must be used inside CheckpointProvider');
  return value;
}

/** Points sync/upload at the host chosen on the sign-in screen, so the auth
 * feature never has to reach into checkpoint settings. */
function useServerConfigSync(
  settings: CheckpointSettings,
  applySettings: (next: CheckpointSettings, options?: { silent?: boolean }) => Promise<void>,
): void {
  const applySettingsRef = useRef(applySettings);
  const settingsRef = useRef(settings);

  useEffect(() => {
    applySettingsRef.current = applySettings;
    settingsRef.current = settings;
  });

  useEffect(() => {
    return subscribeServerConfig(() => {
      void applySettingsRef.current(
        { ...settingsRef.current, serverUrl: resolveApiUrl() },
        { silent: true },
      );
    });
  }, []);
}

export function CheckpointProvider({ children }: PropsWithChildren) {
  const { showToast } = useToast();
  const snapshot = useSyncExternalStore(syncEngine.subscribe, syncEngine.getSnapshot);
  const [claimText, setClaimText] = useState('');
  const [settings, setSettings] = useState<CheckpointSettings>(defaultSettings);
  const [settingsLoaded, setSettingsLoaded] = useState(false);
  const [dialog, setDialog] = useState<DialogState>(null);

  useEffect(() => {
    void loadSettings().then((loaded) => {
      setSettings(loaded);
      syncEngine.configure(loaded);
      networkMonitor.configure(loaded.serverUrl);
      void syncEngine.start();
      void networkMonitor.start();
      setSettingsLoaded(true);
    });
  }, []);

  useEffect(() => {
    if (!settingsLoaded) return;
    const shouldRun = shouldRunSyncService({
      connected: snapshot.connected,
      autoSyncEnabled: settings.autoSyncEnabled,
    });
    if (!shouldRun) {
      void stopSyncService();
      return;
    }
    void startSyncService({
      connected: snapshot.connected,
      recording: snapshot.status?.recording ?? false,
    });
  }, [settingsLoaded, settings.autoSyncEnabled, snapshot.connected, snapshot.status?.recording]);

  const connect = useCallback(async () => {
    syncEngine.configure(settings);
    await syncEngine.connect(settings.deviceName, claimText);
  }, [claimText, settings]);

  const disconnect = useCallback(async () => {
    await syncEngine.disconnect();
  }, []);

  const forgetDevice = useCallback(async () => {
    await syncEngine.forgetDevice();
    showToast('Pendant forgotten.');
  }, [showToast]);

  const releasePendant = useCallback(async () => {
    try {
      await syncEngine.releasePendant();
      showToast('Pendant released.');
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'Release failed.');
    }
  }, [showToast]);

  const setDeviceName = useCallback(
    (name: string) => {
      const next = { ...settings, deviceName: name };
      setSettings(next);
      void saveSettings(next).catch((error: unknown) => {
        console.warn(`[ui] save device name failed: ${String(error)}`);
      });
    },
    [settings],
  );

  const applyLed = useCallback(
    async (muted: boolean, brightness: number) => {
      const code = await syncEngine.applyLed(muted, brightness);
      if (code === CTRL_OK) showToast('LED settings applied.');
    },
    [showToast],
  );

  const deleteFile = useCallback(
    async (path: string) => {
      const code = await syncEngine.deleteFile(path);
      if (code === CTRL_OK) showToast('File deleted.');
      else if (code !== null) showToast(`Delete failed: ${ctrlStatusText(code)}`);
    },
    [showToast],
  );

  const eraseStorage = useCallback(async () => {
    const code = await syncEngine.eraseStorage();
    if (code === CTRL_OK) showToast('All recordings erased.');
  }, [showToast]);

  const updateSettings = useCallback(
    async (next: CheckpointSettings, options?: { silent?: boolean }) => {
      const autoSyncChanged = next.autoSyncEnabled !== settings.autoSyncEnabled;
      await saveSettings(next);
      setSettings(next);
      syncEngine.configure(next);
      networkMonitor.configure(next.serverUrl);
      if (autoSyncChanged) {
        await syncEngine.applyAutoSyncIfConnected();
        if (next.autoSyncEnabled) await syncEngine.autoConnectNow();
      }
      if (!options?.silent) showToast('Saved.');
    },
    [settings.autoSyncEnabled, showToast],
  );

  useServerConfigSync(settings, updateSettings);

  const testConnection = useCallback(async () => {
    const probe = await syncEngine.testConnection();
    if (!probe) showToast('No internet connection — see debug log.');
    return probe;
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

  const previewStorageFile = useCallback(
    async (path: string) => {
      const code = await syncEngine.previewStorageFile(path);
      if (code !== null && code !== CTRL_OK) {
        showToast(`Could not fetch preview: ${ctrlStatusText(code)}`);
      }
      return code;
    },
    [showToast],
  );

  const requestDelete = useCallback((path: string) => setDialog({ kind: 'delete', path }), []);
  const requestErase = useCallback(() => setDialog({ kind: 'erase' }), []);
  const requestForget = useCallback(() => setDialog({ kind: 'forget' }), []);
  const requestRelease = useCallback(() => setDialog({ kind: 'release' }), []);
  const cancelDialog = useCallback(() => setDialog(null), []);

  const confirmDialog = useCallback(() => {
    const pending = dialog;
    setDialog(null);
    if (pending?.kind === 'delete') void deleteFile(pending.path);
    if (pending?.kind === 'erase') void eraseStorage();
    if (pending?.kind === 'forget') void forgetDevice();
    if (pending?.kind === 'release') void releasePendant();
  }, [dialog, deleteFile, eraseStorage, forgetDevice, releasePendant]);

  const value = useMemo<CheckpointContextValue>(
    () => ({
      connected: snapshot.connected,
      busy: snapshot.busy,
      linkState: snapshot.linkState,
      hydrated: snapshot.hydrated,
      deviceId: snapshot.deviceId,
      enrolled: snapshot.enrolled,
      ownedDeviceId: snapshot.ownedDeviceId,
      autoConnecting: snapshot.autoConnecting,
      deviceName: settings.deviceName,
      setDeviceName,
      claimText,
      setClaimText,
      status: snapshot.status,
      storage: snapshot.storage,
      fileList: snapshot.fileList,
      listPage: snapshot.listPage,
      transfers: snapshot.transfers,
      preview: snapshot.preview,
      deleting: snapshot.deleting,
      erasing: snapshot.erasing,
      logs: snapshot.logs,
      settings,
      connect,
      reconnect: syncEngine.reconnect,
      disconnect,
      forgetDevice,
      releasePendant,
      stopConnection: syncEngine.stopConnection,
      refreshStatus: syncEngine.refreshStatus,
      refreshStorage: syncEngine.refreshStorage,
      refreshTransfers: syncEngine.refreshTransfers,
      listPrev: syncEngine.listPrev,
      listNext: syncEngine.listNext,
      toggleRec: syncEngine.toggleRec,
      applyLed,
      requestDelete,
      requestErase,
      requestForget,
      requestRelease,
      previewStorageFile,
      updateSettings,
      shareBench: syncEngine.shareBench,
      clearLogs: syncEngine.clearLogs,
      needsSettings: snapshot.needsSettings,
      openAppSettings,
      testConnection,
    }),
    [
      applyLed,
      claimText,
      connect,
      disconnect,
      forgetDevice,
      releasePendant,
      openAppSettings,
      previewStorageFile,
      requestDelete,
      requestErase,
      requestForget,
      requestRelease,
      setDeviceName,
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
        title={
          dialog?.kind === 'erase'
            ? 'Erase all recordings'
            : dialog?.kind === 'forget'
              ? 'Forget pendant'
              : dialog?.kind === 'release'
                ? 'Release pendant'
                : 'Delete file'
        }
        body={
          dialog?.kind === 'erase'
            ? `Erase ALL recordings from the pendant memory? This cannot be undone.${
                snapshot.storage
                  ? ` Files: ${snapshot.storage.files} · Used: ${formatBytes(snapshot.storage.used)}.`
                  : ''
              } Type ERASE to confirm.`
            : dialog?.kind === 'forget'
              ? 'Forget this pendant? You will need its claim key to set it up again.'
              : dialog?.kind === 'release'
                ? 'Release this pendant? Local recordings are erased, the pendant forgets this phone, and cloud ownership is removed. This cannot be undone.'
                : `Delete ${dialogFile} from the pendant? This cannot be undone.`
        }
        confirmLabel={
          dialog?.kind === 'erase'
            ? 'Erase all'
            : dialog?.kind === 'forget'
              ? 'Forget'
              : dialog?.kind === 'release'
                ? 'Release'
                : 'Delete'
        }
        requireText={dialog?.kind === 'erase' ? 'ERASE' : undefined}
        onCancel={cancelDialog}
        onConfirm={confirmDialog}
      />
    </CheckpointContext.Provider>
  );
}
