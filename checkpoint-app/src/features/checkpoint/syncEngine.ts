import { Directory, File, Paths } from 'expo-file-system';
import { isAvailableAsync, shareAsync } from 'expo-sharing';
import { AppState } from 'react-native';
import { BleManager, State } from 'react-native-ble-plx';

import { parseClaimHex } from './claim.ts';
import { CheckpointClient, type CompletedFile } from './client.ts';
import {
  CTRL_ERASE_ARM,
  CTRL_ERASE_CONFIRM,
  CTRL_OK,
  DEVICE_NAME,
  KAFKA_TOPIC_HINT,
  MAX_LOG_LINES,
  STATUS_POLL_INTERVAL_S,
  TRANSFER_TICK_MS,
} from './config.ts';
import { clearEnrolledDeviceId, deleteCredential, getEnrolledDeviceId } from './credentials.ts';
import { hexToBytes } from './crypto.ts';
import { IngestionUploader } from './ingestion.ts';
import { networkMonitor } from './networkMonitor.ts';
import type { HealthProbe } from './networkStatus.ts';
import { ctrlStatusText } from './parsers.ts';
import { ensureBlePermissions, hasBlePermissions } from './permissions.ts';
import { playback } from './playback.ts';
import { nextRetryDelayMs } from './retry.ts';
import { defaultSettings, type CheckpointSettings } from './settings.ts';
import {
  deleteSaved,
  loadTransfers,
  readSavedBytes,
  receivedFile,
  saveTransfers,
} from './store.ts';
import {
  applyEventToRecords,
  isTerminal,
  patchTransfer,
  pruneExpired,
  sortTransfers,
  type TransferRecord,
} from './transferStore.ts';
import type {
  CheckpointEvent,
  DeviceFileList,
  DeviceStatus,
  LogEntry,
  StorageInfo,
} from './types.ts';
import { checkSpeech, shouldUpload, vadSkipReason } from './vad.ts';

export interface ListPage {
  start: number;
  total: number;
  count: number;
}

export type BluetoothStatus = 'on' | 'off' | 'unauthorized' | 'unsupported' | 'unknown' | null;

export interface PreviewSnapshot {
  path: string;
  fileId: string;
  received: number;
  totalFrags: number;
  totalBytes: number;
}

export interface EngineSnapshot {
  connected: boolean;
  busy: boolean;
  linkState: string;
  hydrated: boolean;
  status: DeviceStatus | null;
  storage: StorageInfo | null;
  fileList: DeviceFileList | null;
  listPage: ListPage;
  transfers: TransferRecord[];
  preview: PreviewSnapshot | null;
  deleting: string | null;
  erasing: boolean;
  logs: LogEntry[];
  needsSettings: boolean;
  bluetooth: BluetoothStatus;
  deviceId: string | null;
  enrolled: boolean;
  autoConnecting: boolean;
}

const INITIAL_SNAPSHOT: EngineSnapshot = {
  connected: false,
  busy: false,
  linkState: 'idle',
  hydrated: false,
  status: null,
  storage: null,
  fileList: null,
  listPage: { start: 0, total: 0, count: 0 },
  transfers: [],
  preview: null,
  deleting: null,
  erasing: false,
  logs: [],
  needsSettings: false,
  bluetooth: null,
  deviceId: null,
  enrolled: false,
  autoConnecting: false,
};

function mapBluetoothState(state: State): BluetoothStatus {
  switch (state) {
    case State.PoweredOn:
      return 'on';
    case State.PoweredOff:
      return 'off';
    case State.Unauthorized:
      return 'unauthorized';
    case State.Unsupported:
      return 'unsupported';
    default:
      return 'unknown';
  }
}

function pushLog(entries: LogEntry[], entry: LogEntry): LogEntry[] {
  const next = [...entries, entry];
  return next.length > MAX_LOG_LINES ? next.slice(next.length - MAX_LOG_LINES) : next;
}

class SyncEngine {
  private manager: BleManager | null = null;
  private client: CheckpointClient | null = null;
  private stopped = true;
  private settings: CheckpointSettings = defaultSettings();
  private listeners = new Set<() => void>();
  private snapshot: EngineSnapshot = INITIAL_SNAPSHOT;
  private started = false;
  private appStateSubscription: ReturnType<typeof AppState.addEventListener> | null = null;
  private draining = false;
  private persistTimer: ReturnType<typeof setTimeout> | null = null;
  private tickTimer: ReturnType<typeof setInterval> | null = null;
  private networkUnsubscribe: (() => void) | null = null;
  private bluetoothSubscription: { remove: () => void } | null = null;
  private inFlight = new Set<string>();
  private autoConnectGaveUp = false;
  private connectTask: Promise<void> | null = null;
  private connectOrigin: 'user' | 'auto' | null = null;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): EngineSnapshot => this.snapshot;

  configure(settings: CheckpointSettings): void {
    this.settings = settings;
  }

  async start(): Promise<void> {
    if (this.started) return;
    this.started = true;
    this.appendLog('[sync] engine started');
    const stored = await loadTransfers();
    if (!this.started) return;
    if (stored.length > 0) this.setState({ transfers: sortTransfers(stored) });
    this.setState({ hydrated: true });
    this.ensureManager();
    this.cleanup();
    this.appStateSubscription = AppState.addEventListener('change', (state) => {
      if (state === 'active') {
        void this.onForeground();
      }
    });
    this.networkUnsubscribe = networkMonitor.subscribe(() => {
      if (networkMonitor.getSnapshot().state === 'online') void this.drainQueue();
    });
    this.tickTimer = setInterval(() => {
      this.cleanup();
      void this.drainQueue();
    }, TRANSFER_TICK_MS);
    this.autoConnectGaveUp = false;
    this.setState({ enrolled: (await getEnrolledDeviceId()) !== null });
    await this.maybeAutoConnect();
  }

  async stop(): Promise<void> {
    if (!this.started) return;
    this.started = false;
    this.appStateSubscription?.remove();
    this.appStateSubscription = null;
    this.networkUnsubscribe?.();
    this.networkUnsubscribe = null;
    this.bluetoothSubscription?.remove();
    this.bluetoothSubscription = null;
    if (this.tickTimer) {
      clearInterval(this.tickTimer);
      this.tickTimer = null;
    }
    this.flushPersist();
    await this.disconnect();
  }

  async applyAutoSyncIfConnected(): Promise<void> {
    if (!this.snapshot.connected || !this.client) return;
    await this.applyDesiredSync(this.client);
  }

  async testConnection(): Promise<HealthProbe | null> {
    this.appendLog(`[net] testing ${this.settings.serverUrl} …`);
    const probe = await networkMonitor.probeNow();
    if (!probe) {
      this.appendLog('[net] test skipped: no internet connection');
      return null;
    }
    this.appendLog(`[net] ${probe.ok ? 'reachable' : 'unreachable'} in ${probe.latencyMs}ms`);
    return probe;
  }

  private ensureManager(): BleManager {
    if (!this.manager) {
      this.manager = new BleManager();
      try {
        this.bluetoothSubscription = this.manager.onStateChange(
          (state) => this.setState({ bluetooth: mapBluetoothState(state) }),
          true,
        );
      } catch (error) {
        this.appendLog(
          `[ble] state listener failed: ${error instanceof Error ? error.message : 'unknown'}`,
        );
      }
    }
    return this.manager;
  }

  private async maybeAutoConnect(): Promise<void> {
    if (!this.settings.autoSyncEnabled) {
      this.appendLog('[sync] auto-connect skipped: auto-sync is off');
      return;
    }
    if (this.snapshot.connected || this.snapshot.busy || this.snapshot.autoConnecting) return;
    const enrolled = await getEnrolledDeviceId();
    if (!enrolled) {
      this.appendLog('[sync] auto-connect skipped: no enrolled pendant');
      return;
    }
    if (!(await hasBlePermissions())) {
      this.appendLog('[sync] auto-connect deferred: Bluetooth permission missing');
      return;
    }
    const manager = this.ensureManager();
    let adapter: string;
    try {
      adapter = await manager.state();
    } catch {
      adapter = 'Unknown';
    }
    if (adapter !== 'PoweredOn') {
      this.appendLog(`[sync] auto-connect deferred: Bluetooth ${adapter}`);
      return;
    }
    this.appendLog('[sync] auto-connecting to enrolled pendant');
    await this.connect(DEVICE_NAME, '', 'auto');
  }

  private async applyDesiredSync(client: CheckpointClient): Promise<void> {
    try {
      const status = await client.reqStatus();
      if (status.sync === this.settings.autoSyncEnabled) {
        this.appendLog(`[sync] pendant sync already ${status.sync}`);
        return;
      }
      const code = await client.cmdSyncSet(this.settings.autoSyncEnabled);
      this.appendLog(
        `[sync] pendant sync set ${this.settings.autoSyncEnabled} status=${ctrlStatusText(code)}`,
      );
      await client.reqStatus();
    } catch (error) {
      this.appendLog(`[sync] apply failed: ${error instanceof Error ? error.message : 'unknown'}`);
    }
  }

  private async onForeground(): Promise<void> {
    this.cleanup();
    void this.drainQueue();
    if (this.snapshot.connected) {
      await this.applyAutoSyncIfConnected();
      return;
    }
    if (!this.autoConnectGaveUp) await this.maybeAutoConnect();
  }

  private patchRecord(fileId: string, patch: Partial<TransferRecord>): void {
    const transfers = sortTransfers(
      patchTransfer(this.snapshot.transfers, fileId, Date.now(), patch),
    );
    this.setState({ transfers });
    this.schedulePersist();
  }

  private reportIngest(
    fileId: string,
    uploadId: string,
    ingestStatus: string,
    ingestError: string,
    vadStatus?: string,
    vadSpeechS?: string,
  ): void {
    this.client?.bench.updateIngest(
      fileId,
      uploadId,
      ingestStatus,
      ingestError,
      vadStatus,
      vadSpeechS,
    );
    this.handleEvent({
      type: 'ingest',
      fileId,
      uploadId,
      ingestStatus,
      ingestError,
      vadStatus,
      vadSpeechS,
    });
  }

  private schedulePersist(): void {
    if (this.persistTimer) return;
    this.persistTimer = setTimeout(() => {
      this.persistTimer = null;
      saveTransfers(this.snapshot.transfers);
    }, 500);
  }

  private flushPersist(): void {
    if (this.persistTimer) {
      clearTimeout(this.persistTimer);
      this.persistTimer = null;
    }
    saveTransfers(this.snapshot.transfers);
  }

  private cleanup(): void {
    const retentionMs = Math.max(0, this.settings.retentionHours) * 60 * 60 * 1000;
    const before = this.snapshot.transfers;
    const after = pruneExpired(before, Date.now(), retentionMs);
    if (after.length === before.length) return;
    const removed = before.filter((record) => !after.includes(record));
    for (const record of removed) void deleteSaved(record.fileId);
    this.setState({ transfers: sortTransfers(after) });
    this.flushPersist();
    this.appendLog(`[sync] cleaned ${removed.length} old transfer(s)`);
  }

  private async drainQueue(): Promise<void> {
    if (this.draining) return;
    this.draining = true;
    try {
      const now = Date.now();
      const queue = this.snapshot.transfers.filter(
        (record) =>
          !isTerminal(record) &&
          record.localUri !== undefined &&
          (record.nextAttemptAt === undefined || record.nextAttemptAt <= now),
      );
      for (const record of queue) {
        if (networkMonitor.getSnapshot().state !== 'online') break;
        await this.uploadRecord(record);
      }
    } finally {
      this.draining = false;
    }
  }

  private async uploadRecord(record: TransferRecord): Promise<void> {
    if (!record.localUri || isTerminal(record) || this.inFlight.has(record.fileId)) return;
    this.inFlight.add(record.fileId);
    try {
      const bytes = await readSavedBytes(record.localUri);
      if (!bytes) {
        this.appendLog(`[sync] local audio missing for ${record.fileId}`);
        this.reportIngest(record.fileId, '', 'failed', 'local audio missing');
        this.patchRecord(record.fileId, { localUri: undefined });
        return;
      }
      this.appendLog(`  [ingest] uploading file_${record.fileId}.ogg (${bytes.length}B) ...`);
      const uploader = new IngestionUploader(this.settings.serverUrl, this.settings.userId);
      try {
        const result = await uploader.upload(
          bytes,
          `file_${record.fileId}.ogg`,
          'audio/ogg',
          record.fileId,
        );
        this.appendLog(`  [ingest] OK upload_id=${result.uploadId} status=${result.status}`);
        this.reportIngest(record.fileId, result.uploadId, result.status, '');
        this.patchRecord(record.fileId, { attempts: 0, nextAttemptAt: undefined });
      } catch (error) {
        const message = error instanceof Error ? error.message : 'unknown';
        this.appendLog(`  [!] ingest failed: ${message}`);
        const attempts = record.attempts + 1;
        this.reportIngest(record.fileId, '', 'failed', message);
        this.patchRecord(record.fileId, {
          attempts,
          nextAttemptAt: Date.now() + nextRetryDelayMs(attempts),
        });
      }
    } finally {
      this.inFlight.delete(record.fileId);
    }
  }

  private setState(patch: Partial<EngineSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) listener();
  }

  private appendLog(text: string): void {
    // Mirrored to logcat so field issues can be diagnosed over adb.
    console.debug(text);
    this.setState({ logs: pushLog(this.snapshot.logs, { at: Date.now(), text }) });
  }

  private handlePreviewEvent(event: CheckpointEvent): void {
    if (event.type === 'announce') {
      this.setState({
        preview: {
          path: event.path ?? this.snapshot.preview?.path ?? 'preview',
          fileId: event.fileId,
          received: 0,
          totalFrags: event.totalFrags,
          totalBytes: event.totalBytes,
        },
      });
      return;
    }
    if (event.type === 'progress') {
      const preview = this.snapshot.preview;
      if (!preview) return;
      this.setState({
        preview: { ...preview, received: event.received, totalFrags: event.totalFrags },
      });
      return;
    }
    if (event.type === 'file_done' && !event.crcOk) {
      this.appendLog(`[preview] ${event.fileId} checksum failed`);
      this.setState({ preview: null });
    }
  }

  private handleEvent(event: CheckpointEvent): void {
    if (event.type === 'link') {
      this.setState(
        event.state === 'down'
          ? { linkState: event.state, preview: null }
          : { linkState: event.state },
      );
      return;
    }
    if ('preview' in event && event.preview) {
      this.handlePreviewEvent(event);
      return;
    }
    if (event.type === 'rec_status') {
      const { type: _ignored, ...rest } = event;
      this.setState({ status: rest });
      return;
    }
    if (event.type === 'storage') {
      const { type: _ignored, ...rest } = event;
      this.setState({ storage: rest });
      return;
    }
    if (event.type === 'file_list') {
      const { type: _ignored, ...rest } = event;
      this.setState({
        fileList: rest,
        listPage: { start: rest.start, total: rest.total, count: rest.entries.length },
      });
      return;
    }
    const transfers = sortTransfers(
      applyEventToRecords(this.snapshot.transfers, event, Date.now()),
    );
    this.setState({ transfers });
    if (event.type !== 'progress') this.schedulePersist();
  }

  private async handleCompletedFile(file: CompletedFile): Promise<void> {
    const current = this.settings;
    const verdict = current.vadEnabled
      ? await checkSpeech(file.bytes, {
          threshold: current.vadThreshold,
          minSpeechS: current.minSpeechS,
        })
      : { status: 'disabled' as const, speechS: 0 };
    if (verdict.status === 'unavailable') {
      this.appendLog(`  [vad] unavailable: ${verdict.error ?? 'unknown'} — uploading anyway`);
    }
    const vad = `${verdict.status} ${verdict.speechS.toFixed(2)}s`.trim();
    if (!shouldUpload(verdict)) {
      const reason = vadSkipReason(verdict, current.minSpeechS);
      this.appendLog(`  [vad] filtered ${file.fileIdHex}: ${reason} — skipping upload`);
      this.reportIngest(
        file.fileIdHex,
        '',
        'skipped-no-speech',
        reason,
        verdict.status,
        verdict.speechS.toFixed(2),
      );
      CheckpointClient.deleteLocalCopy(file.fileIdHex);
      return;
    }
    if (!current.ingestEnabled) {
      this.reportIngest(file.fileIdHex, '', 'disabled', '', 'disabled', '0.00');
      return;
    }
    this.patchRecord(file.fileIdHex, {
      localUri: receivedFile(file.fileIdHex, '.ogg').uri,
      vad,
    });
    const record = this.snapshot.transfers.find((item) => item.fileId === file.fileIdHex);
    if (record) await this.uploadRecord(record);
  }

  private async handlePreviewFile(file: CompletedFile): Promise<void> {
    const label = this.snapshot.preview?.path ?? 'preview';
    this.setState({ preview: null });
    try {
      await playback.playBytes(file.bytes, label);
      this.appendLog(`[preview] playing ${label}`);
    } catch (error) {
      this.appendLog(
        `[preview] playback failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
    }
  }

  private async withClient<T>(
    action: string,
    run: (client: CheckpointClient) => Promise<T>,
  ): Promise<T | null> {
    const client = this.client;
    if (!client) return null;
    try {
      return await run(client);
    } catch (error) {
      this.appendLog(
        `[ui] ${action} failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
      return null;
    }
  }

  async connect(
    deviceName: string,
    claimText: string,
    origin: 'user' | 'auto' = 'user',
  ): Promise<void> {
    if (origin === 'user') await this.stopAutoConnect();
    if (this.connectTask || this.snapshot.connected || this.snapshot.busy) return;
    this.connectOrigin = origin;
    this.setState(
      origin === 'user'
        ? { busy: true, autoConnecting: false, linkState: 'connecting', needsSettings: false }
        : { busy: false, autoConnecting: true, linkState: 'idle', needsSettings: false },
    );
    let manager: BleManager;
    try {
      manager = this.ensureManager();
      const gate = await ensureBlePermissions();
      if (gate !== 'granted') {
        this.appendLog(
          `[ui] missing Bluetooth permission (${gate}) — grant Nearby devices + Location and retry`,
        );
        this.connectOrigin = null;
        this.setState({
          busy: false,
          autoConnecting: false,
          needsSettings: gate === 'needs-settings',
          linkState: gate === 'needs-settings' ? 'needs permission' : 'permission denied',
        });
        return;
      }
      this.appendLog('[ui] permissions granted');
      const adapter = await manager.state();
      if (adapter !== 'PoweredOn') {
        this.appendLog(
          adapter === 'PoweredOff'
            ? '[ui] Bluetooth is off — turn it on and retry'
            : `[ui] Bluetooth unavailable (${adapter}) — check system settings and retry`,
        );
        this.connectOrigin = null;
        this.setState({
          busy: false,
          autoConnecting: false,
          needsSettings: adapter === 'Unauthorized',
          linkState: adapter === 'PoweredOff' ? 'bluetooth off' : 'bluetooth unavailable',
        });
        return;
      }
      this.appendLog(`[ble] adapter ${adapter} — scanning...`);
    } catch (error) {
      this.appendLog(
        `[ui] pre-connect check failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
      this.connectOrigin = null;
      this.setState({ busy: false, autoConnecting: false, linkState: 'idle' });
      return;
    }
    let claimKey: Uint8Array | null = null;
    const trimmedClaim = claimText.trim();
    if (trimmedClaim !== '') {
      try {
        claimKey = parseClaimHex(trimmedClaim);
      } catch (error) {
        this.appendLog(`[ui] bad claim key: ${error instanceof Error ? error.message : 'unknown'}`);
        this.connectOrigin = null;
        this.setState({ busy: false, autoConnecting: false, linkState: 'idle' });
        return;
      }
    }
    let client: CheckpointClient;
    try {
      client = new CheckpointClient(manager, deviceName.trim() || DEVICE_NAME, {
        onEvent: (event) => this.handleEvent(event),
        log: (line) => this.appendLog(line),
        onFile: (file) => {
          if (file.preview) void this.handlePreviewFile(file);
          else void this.handleCompletedFile(file);
        },
      });
    } catch (error) {
      this.appendLog(
        `[ui] client init failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
      this.connectOrigin = null;
      this.setState({ busy: false, autoConnecting: false, linkState: 'idle' });
      return;
    }
    this.client = client;
    this.stopped = false;
    const target = deviceName.trim() || DEVICE_NAME;
    const enrollKey = claimKey;
    this.connectTask = (async () => {
      let lastPoll = 0;
      let reachedReady = false;
      try {
        await client.supervise(target, enrollKey, {
          stopped: () => this.stopped,
          onReady: async () => {
            reachedReady = true;
            this.autoConnectGaveUp = false;
            this.setState({ connected: true, busy: false, autoConnecting: false });
            this.setState({ deviceId: await getEnrolledDeviceId(), enrolled: true });
            this.appendLog('[ui] listening for file transfers …');
            await this.applyDesiredSync(client);
            try {
              await client.reqStorage();
              await client.reqList(0);
            } catch (error) {
              this.appendLog(
                `[ui] initial storage load failed: ${error instanceof Error ? error.message : 'unknown'}`,
              );
            }
            this.appendLog(`[ui] queue-wait until SUBMITTED (Kafka ${KAFKA_TOPIC_HINT})`);
            void this.drainQueue();
          },
          onAlive: () => {
            this.setState({ linkState: 'listening' });
          },
          onTick: async () => {
            const now = Date.now();
            if (now - lastPoll < STATUS_POLL_INTERVAL_S * 1000) return;
            lastPoll = now;
            try {
              await client.reqStatus();
            } catch (error) {
              this.appendLog(
                `[ui] status poll failed: ${error instanceof Error ? error.message : 'unknown'}`,
              );
            }
          },
        });
      } catch (error) {
        this.appendLog(
          `[ui] connect failed: ${error instanceof Error ? error.message : 'unknown'}`,
        );
      }
      if (!reachedReady) this.autoConnectGaveUp = true;
      this.setState({
        connected: false,
        busy: false,
        autoConnecting: false,
        linkState: 'idle',
        deviceId: null,
      });
      this.client = null;
      this.connectTask = null;
      this.connectOrigin = null;
    })();
  }

  stopAutoConnect = async (): Promise<void> => {
    if (this.connectOrigin !== 'auto') return;
    this.appendLog('[ui] auto-connect cancelled');
    this.stopped = true;
    this.client?.requestStop();
    await this.connectTask;
  };

  async disconnect(): Promise<void> {
    this.stopped = true;
    this.autoConnectGaveUp = true;
    const client = this.client;
    if (client) {
      try {
        await client.disconnect();
        this.appendLog('[ui] disconnected (bond kept — no re-pair needed)');
      } catch (error) {
        this.appendLog(
          `[ui] disconnect failed: ${error instanceof Error ? error.message : 'unknown'}`,
        );
      }
    }
    this.setState({
      connected: false,
      autoConnecting: false,
      linkState: 'idle',
      deviceId: null,
      preview: null,
    });
  }

  async forgetDevice(): Promise<void> {
    await this.disconnect();
    const enrolled = await getEnrolledDeviceId();
    if (enrolled) {
      try {
        await deleteCredential(hexToBytes(enrolled));
      } catch (error) {
        this.appendLog(
          `[ui] forget credential failed: ${error instanceof Error ? error.message : 'unknown'}`,
        );
      }
    }
    await clearEnrolledDeviceId();
    this.setState({ enrolled: false, deviceId: null });
    this.appendLog('[ui] pendant forgotten');
  }

  refreshStatus = async (): Promise<void> => {
    await this.withClient('status', async (client) => client.reqStatus());
  };

  refreshStorage = async (): Promise<void> => {
    const start = this.snapshot.listPage.start;
    await this.withClient('storage', async (client) => {
      await client.reqStorage();
      await client.reqList(start);
    });
  };

  previewStorageFile = async (path: string): Promise<number | null> => {
    const client = this.client;
    if (!client) return null;
    this.setState({
      preview: { path, fileId: '', received: 0, totalFrags: 0, totalBytes: 0 },
    });
    try {
      const code = await client.cmdFileFetch(path);
      if (code !== CTRL_OK) {
        this.setState({ preview: null });
        this.appendLog(`[preview] fetch ${path} rejected status=${ctrlStatusText(code)}`);
      }
      return code;
    } catch (error) {
      this.setState({ preview: null });
      this.appendLog(
        `[preview] fetch failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
      return null;
    }
  };

  refreshTransfers = async (): Promise<void> => {
    this.setState({
      transfers: this.snapshot.transfers.map((record) =>
        isTerminal(record) ? record : { ...record, nextAttemptAt: undefined },
      ),
    });
    this.schedulePersist();
    this.appendLog('[ui] retry: backoff reset, draining queue');
    await this.drainQueue();
  };

  toggleRec = async (): Promise<void> => {
    await this.withClient('rec-toggle', async (client) => {
      const next = !(this.snapshot.status?.recording ?? false);
      const code = next ? await client.cmdRecStart() : await client.cmdRecStop();
      this.appendLog(`[ui] rec-${next ? 'start' : 'stop'} status=${ctrlStatusText(code)}`);
      await client.reqStatus();
    });
  };

  async applyLed(muted: boolean, brightness: number): Promise<number | null> {
    return this.withClient('led-apply', async (client) => {
      const code = await client.cmdLedSet(muted, brightness);
      this.appendLog(
        `[ui] led-apply muted=${muted} bright=${brightness} status=${ctrlStatusText(code)}`,
      );
      await client.reqStatus();
      return code;
    });
  }

  listPrev = async (): Promise<void> => {
    const page = this.snapshot.listPage;
    const start = Math.max(0, page.start - Math.max(1, page.count));
    await this.withClient('file-list', async (client) => client.reqList(start));
  };

  listNext = async (): Promise<void> => {
    const page = this.snapshot.listPage;
    const start = page.start + Math.max(1, page.count);
    if (page.total > 0 && start >= page.total) return;
    await this.withClient('file-list', async (client) => client.reqList(start));
  };

  async deleteFile(path: string): Promise<number | null> {
    if (!this.client) return null;
    this.setState({ deleting: path });
    try {
      const code = await this.withClient('file-delete', async (client) => {
        const status = await client.cmdFileDelete(path);
        this.appendLog(`[ui] file-delete ${path} status=${ctrlStatusText(status)}`);
        return status;
      });
      await this.refreshStorage();
      return code;
    } finally {
      this.setState({ deleting: null });
    }
  }

  async eraseStorage(): Promise<number | null> {
    if (!this.client) return null;
    this.setState({ erasing: true });
    try {
      return await this.withClient('storage-erase', async (client) => {
        const arm = await client.cmdStorageErase(CTRL_ERASE_ARM);
        if ((arm.status ?? 1) !== CTRL_OK) {
          this.appendLog(`[ui] erase arm refused status=${ctrlStatusText(arm.status ?? 1)}`);
          return null;
        }
        const confirmed = await client.cmdStorageErase(CTRL_ERASE_CONFIRM);
        this.appendLog(
          `[ui] erase confirm status=${ctrlStatusText(confirmed.status ?? 1)} removed=${confirmed.removed ?? '?'}`,
        );
        this.setState({ listPage: { start: 0, total: 0, count: 0 } });
        await client.reqStorage();
        await client.reqList(0);
        return confirmed.status ?? 1;
      });
    } finally {
      this.setState({ erasing: false });
    }
  }

  clearLogs = (): void => {
    this.setState({ logs: [] });
  };

  shareBench = async (): Promise<void> => {
    const client = this.client;
    if (!client || client.bench.rows.length === 0) {
      this.appendLog('[ui] bench: no rows to share yet');
      return;
    }
    try {
      if (!(await isAvailableAsync())) {
        this.appendLog('[ui] bench: sharing is not available on this device');
        return;
      }
      const dir = new Directory(Paths.cache, 'checkpoint');
      if (!dir.exists) dir.create();
      const file = new File(dir, `bench-${Date.now()}.csv`);
      if (file.exists) file.delete();
      file.create();
      file.write(new TextEncoder().encode(client.bench.toCsv()));
      await shareAsync(file.uri, { dialogTitle: 'Share bench CSV' });
    } catch (error) {
      this.appendLog(
        `[ui] bench share failed: ${error instanceof Error ? error.message : 'unknown'}`,
      );
    }
  };
}

export const syncEngine = new SyncEngine();
