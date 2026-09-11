import { AppState } from "react-native";
import { BleManager } from "react-native-ble-plx";
import { Directory, File, Paths } from "expo-file-system";
import { isAvailableAsync, shareAsync } from "expo-sharing";

import { CheckpointClient, type CompletedFile } from "./client.ts";
import { parseClaimHex } from "./claim.ts";
import { getEnrolledDeviceId } from "./credentials.ts";
import { ensureBlePermissions, hasBlePermissions } from "./permissions.ts";
import {
  CTRL_ERASE_ARM,
  CTRL_ERASE_CONFIRM,
  CTRL_OK,
  DEVICE_NAME,
  KAFKA_TOPIC_HINT,
  MAX_LOG_LINES,
  STATUS_POLL_INTERVAL_S,
} from "./config.ts";
import { IngestionUploader } from "./ingestion.ts";
import { networkMonitor } from "./networkMonitor.ts";
import { ctrlStatusText } from "./parsers.ts";
import { defaultSettings, type CheckpointSettings } from "./settings.ts";
import { checkSpeech, shouldUpload, vadSkipReason } from "./vad.ts";
import { applyEventToRecords, type TransferRecord } from "./transferStore.ts";
import type {
  CheckpointEvent,
  DeviceFileList,
  DeviceStatus,
  StorageInfo,
} from "./types.ts";

export interface ListPage {
  start: number;
  total: number;
  count: number;
}

export interface EngineSnapshot {
  connected: boolean;
  busy: boolean;
  linkState: string;
  status: DeviceStatus | null;
  storage: StorageInfo | null;
  fileList: DeviceFileList | null;
  listPage: ListPage;
  transfers: TransferRecord[];
  logs: string[];
  needsSettings: boolean;
}

const INITIAL_SNAPSHOT: EngineSnapshot = {
  connected: false,
  busy: false,
  linkState: "idle",
  status: null,
  storage: null,
  fileList: null,
  listPage: { start: 0, total: 0, count: 0 },
  transfers: [],
  logs: [],
  needsSettings: false,
};

function pushLog(lines: string[], line: string): string[] {
  const next = [...lines, line];
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
    this.appendLog("[sync] engine started");
    this.appStateSubscription = AppState.addEventListener("change", (state) => {
      if (state === "active") {
        void this.onForeground();
      }
    });
    await this.maybeAutoConnect();
  }

  async stop(): Promise<void> {
    if (!this.started) return;
    this.started = false;
    this.appStateSubscription?.remove();
    this.appStateSubscription = null;
    await this.disconnect();
  }

  async applyAutoSyncIfConnected(): Promise<void> {
    if (!this.snapshot.connected || !this.client) return;
    await this.applyDesiredSync(this.client);
  }

  async testConnection(): Promise<boolean> {
    this.appendLog(`[net] testing ${this.settings.serverUrl} …`);
    const probe = await networkMonitor.probeNow();
    if (!probe) {
      this.appendLog("[net] test skipped: no internet connection");
      return false;
    }
    this.appendLog(`[net] ${probe.ok ? "reachable" : "unreachable"} in ${probe.latencyMs}ms`);
    return probe.ok;
  }

  private async maybeAutoConnect(): Promise<void> {
    if (!this.settings.autoSyncEnabled) {
      this.appendLog("[sync] auto-connect skipped: auto-sync is off");
      return;
    }
    if (this.snapshot.connected || this.snapshot.busy) return;
    const enrolled = await getEnrolledDeviceId();
    if (!enrolled) {
      this.appendLog("[sync] auto-connect skipped: no enrolled pendant");
      return;
    }
    if (!(await hasBlePermissions())) {
      this.appendLog("[sync] auto-connect deferred: Bluetooth permission missing");
      return;
    }
    if (!this.manager) this.manager = new BleManager();
    let adapter: string;
    try {
      adapter = await this.manager.state();
    } catch {
      adapter = "Unknown";
    }
    if (adapter !== "PoweredOn") {
      this.appendLog(`[sync] auto-connect deferred: Bluetooth ${adapter}`);
      return;
    }
    this.appendLog("[sync] auto-connecting to enrolled pendant");
    await this.connect(DEVICE_NAME, "");
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
      this.appendLog(`[sync] apply failed: ${error instanceof Error ? error.message : "unknown"}`);
    }
  }

  private async onForeground(): Promise<void> {
    if (this.snapshot.connected) {
      await this.applyAutoSyncIfConnected();
      return;
    }
    await this.maybeAutoConnect();
  }

  private setState(patch: Partial<EngineSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) listener();
  }

  private appendLog(line: string): void {
    // Mirrored to logcat so field issues can be diagnosed over adb.
    console.log(line);
    this.setState({ logs: pushLog(this.snapshot.logs, line) });
  }

  private handleEvent(event: CheckpointEvent): void {
    if (event.type === "link") {
      this.setState({ linkState: event.state });
      return;
    }
    if (event.type === "rec_status") {
      const { type: _ignored, ...rest } = event;
      this.setState({ status: rest });
      return;
    }
    if (event.type === "storage") {
      const { type: _ignored, ...rest } = event;
      this.setState({ storage: rest });
      return;
    }
    if (event.type === "file_list") {
      const { type: _ignored, ...rest } = event;
      this.setState({
        fileList: rest,
        listPage: { start: rest.start, total: rest.total, count: rest.entries.length },
      });
      return;
    }
    this.setState({
      transfers: applyEventToRecords(this.snapshot.transfers, event, Date.now()),
    });
  }

  private async handleCompletedFile(client: CheckpointClient, file: CompletedFile): Promise<void> {
    const current = this.settings;
    const verdict = current.vadEnabled
      ? await checkSpeech(file.bytes, {
          threshold: current.vadThreshold,
          minSpeechS: current.minSpeechS,
        })
      : { status: "disabled" as const, speechS: 0 };
    if (verdict.status === "unavailable") {
      this.appendLog(`  [vad] unavailable: ${verdict.error ?? "unknown"} — uploading anyway`);
    }
    if (!shouldUpload(verdict)) {
      const reason = vadSkipReason(verdict, current.minSpeechS);
      this.appendLog(`  [vad] filtered ${file.fileIdHex}: ${reason} — skipping upload`);
      client.reportResult({
        fileId: file.fileIdHex,
        ingestStatus: "skipped-no-speech",
        ingestError: reason,
        vadStatus: verdict.status,
        vadSpeechS: verdict.speechS.toFixed(2),
      });
      if (!current.keepFiles) {
        await CheckpointClient.deleteLocalCopy(file.fileIdHex);
      }
      return;
    }
    if (!current.ingestEnabled) {
      client.reportResult({
        fileId: file.fileIdHex,
        ingestStatus: "disabled",
        vadStatus: "disabled",
        vadSpeechS: "0.00",
      });
      return;
    }
    this.appendLog(`  [ingest] uploading file_${file.fileIdHex}.ogg (${file.bytes.length}B) ...`);
    const uploader = new IngestionUploader(current.serverUrl, current.userId);
    try {
      const result = await uploader.upload(
        file.bytes,
        `file_${file.fileIdHex}.ogg`,
        "audio/ogg",
        file.fileIdHex,
      );
      this.appendLog(`  [ingest] OK upload_id=${result.uploadId} status=${result.status}`);
      client.reportResult({
        fileId: file.fileIdHex,
        uploadId: result.uploadId,
        ingestStatus: result.status,
        vadStatus: verdict.status,
        vadSpeechS: verdict.speechS.toFixed(2),
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : "unknown";
      this.appendLog(`  [!] ingest failed: ${message}`);
      client.reportResult({
        fileId: file.fileIdHex,
        ingestStatus: "failed",
        ingestError: message,
        vadStatus: verdict.status,
        vadSpeechS: verdict.speechS.toFixed(2),
      });
      return;
    }
    if (!current.keepFiles) {
      await CheckpointClient.deleteLocalCopy(file.fileIdHex);
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
      this.appendLog(`[ui] ${action} failed: ${error instanceof Error ? error.message : "unknown"}`);
      return null;
    }
  }

  async connect(deviceName: string, claimText: string): Promise<void> {
    if (this.snapshot.connected || this.snapshot.busy) return;
    this.setState({ busy: true, linkState: "connecting", needsSettings: false });
    let manager: BleManager;
    try {
      if (!this.manager) this.manager = new BleManager();
      manager = this.manager;
      const gate = await ensureBlePermissions();
      if (gate !== "granted") {
        this.appendLog(
          `[ui] missing Bluetooth permission (${gate}) — grant Nearby devices + Location and retry`,
        );
        this.setState({
          busy: false,
          needsSettings: gate === "needs-settings",
          linkState: gate === "needs-settings" ? "needs permission" : "permission denied",
        });
        return;
      }
      this.appendLog("[ui] permissions granted");
      const adapter = await manager.state();
      if (adapter !== "PoweredOn") {
        this.appendLog(
          adapter === "PoweredOff"
            ? "[ui] Bluetooth is off — turn it on and retry"
            : `[ui] Bluetooth unavailable (${adapter}) — check system settings and retry`,
        );
        this.setState({
          busy: false,
          needsSettings: adapter === "Unauthorized",
          linkState: adapter === "PoweredOff" ? "bluetooth off" : "bluetooth unavailable",
        });
        return;
      }
      this.appendLog(`[ble] adapter ${adapter} — scanning...`);
    } catch (error) {
      this.appendLog(`[ui] pre-connect check failed: ${error instanceof Error ? error.message : "unknown"}`);
      this.setState({ busy: false, linkState: "idle" });
      return;
    }
    let claimKey: Uint8Array | null = null;
    const trimmedClaim = claimText.trim();
    if (trimmedClaim !== "") {
      try {
        claimKey = parseClaimHex(trimmedClaim);
      } catch (error) {
        this.appendLog(`[ui] bad claim key: ${error instanceof Error ? error.message : "unknown"}`);
        this.setState({ busy: false, linkState: "idle" });
        return;
      }
    }
    let client: CheckpointClient;
    try {
      client = new CheckpointClient(manager, deviceName.trim() || DEVICE_NAME, {
        onEvent: (event) => this.handleEvent(event),
        log: (line) => this.appendLog(line),
        onFile: (file) => {
          const active = this.client;
          if (active) void this.handleCompletedFile(active, file);
        },
      });
    } catch (error) {
      this.appendLog(`[ui] client init failed: ${error instanceof Error ? error.message : "unknown"}`);
      this.setState({ busy: false, linkState: "idle" });
      return;
    }
    this.client = client;
    this.stopped = false;
    const target = deviceName.trim() || DEVICE_NAME;
    const enrollKey = claimKey;
    void (async () => {
      let lastPoll = 0;
      try {
        await client.supervise(target, enrollKey, {
          stopped: () => this.stopped,
          onReady: async () => {
            this.setState({ connected: true, busy: false });
            this.appendLog("[ui] listening for file transfers …");
            await this.applyDesiredSync(client);
            try {
              await client.reqStorage();
              await client.reqList(0);
            } catch (error) {
              this.appendLog(`[ui] initial storage load failed: ${error instanceof Error ? error.message : "unknown"}`);
            }
            this.appendLog(`[ui] queue-wait until SUBMITTED (Kafka ${KAFKA_TOPIC_HINT})`);
          },
          onAlive: async () => {
            this.setState({ linkState: "listening" });
          },
          onTick: async () => {
            const now = Date.now();
            if (now - lastPoll < STATUS_POLL_INTERVAL_S * 1000) return;
            lastPoll = now;
            try {
              await client.reqStatus();
            } catch (error) {
              this.appendLog(`[ui] status poll failed: ${error instanceof Error ? error.message : "unknown"}`);
            }
          },
        });
      } catch (error) {
        this.appendLog(`[ui] connect failed: ${error instanceof Error ? error.message : "unknown"}`);
      }
      this.setState({ connected: false, busy: false, linkState: "idle" });
      this.client = null;
    })();
  }

  async disconnect(): Promise<void> {
    this.stopped = true;
    const client = this.client;
    if (client) {
      try {
        await client.disconnect();
        this.appendLog("[ui] disconnected (bond kept — no re-pair needed)");
      } catch (error) {
        this.appendLog(`[ui] disconnect failed: ${error instanceof Error ? error.message : "unknown"}`);
      }
    }
    this.setState({ connected: false, linkState: "idle" });
  }

  refreshStatus = async (): Promise<void> => {
    await this.withClient("status", async (client) => client.reqStatus());
  };

  refreshStorage = async (): Promise<void> => {
    await this.withClient("storage", async (client) => {
      await client.reqStorage();
      await client.reqList(0);
    });
  };

  toggleRec = async (): Promise<void> => {
    await this.withClient("rec-toggle", async (client) => {
      const next = !(this.snapshot.status?.recording ?? false);
      const code = next ? await client.cmdRecStart() : await client.cmdRecStop();
      this.appendLog(`[ui] rec-${next ? "start" : "stop"} status=${ctrlStatusText(code)}`);
      await client.reqStatus();
    });
  };

  async applyLed(muted: boolean, brightness: number): Promise<number | null> {
    return this.withClient("led-apply", async (client) => {
      const code = await client.cmdLedSet(muted, brightness);
      this.appendLog(`[ui] led-apply muted=${muted} bright=${brightness} status=${ctrlStatusText(code)}`);
      await client.reqStatus();
      return code;
    });
  }

  async applySync(enabled: boolean): Promise<number | null> {
    return this.withClient("sync-apply", async (client) => {
      const code = await client.cmdSyncSet(enabled);
      this.appendLog(`[ui] sync-apply enabled=${enabled} status=${ctrlStatusText(code)}`);
      await client.reqStatus();
      return code;
    });
  }

  listPrev = async (): Promise<void> => {
    const page = this.snapshot.listPage;
    const start = Math.max(0, page.start - Math.max(1, page.count));
    await this.withClient("file-list", async (client) => client.reqList(start));
  };

  listNext = async (): Promise<void> => {
    const page = this.snapshot.listPage;
    const start = page.start + Math.max(1, page.count);
    if (page.total > 0 && start >= page.total) return;
    await this.withClient("file-list", async (client) => client.reqList(start));
  };

  async deleteFile(path: string): Promise<number | null> {
    const start = this.snapshot.listPage.start;
    return this.withClient("file-delete", async (client) => {
      const code = await client.cmdFileDelete(path);
      this.appendLog(`[ui] file-delete ${path} status=${ctrlStatusText(code)}`);
      await client.reqStorage();
      await client.reqList(start);
      return code;
    });
  }

  async eraseStorage(): Promise<number | null> {
    return this.withClient("storage-erase", async (client) => {
      const arm = await client.cmdStorageErase(CTRL_ERASE_ARM);
      if ((arm.status ?? 1) !== CTRL_OK) {
        this.appendLog(`[ui] erase arm refused status=${ctrlStatusText(arm.status ?? 1)}`);
        return null;
      }
      const confirmed = await client.cmdStorageErase(CTRL_ERASE_CONFIRM);
      this.appendLog(
        `[ui] erase confirm status=${ctrlStatusText(confirmed.status ?? 1)} removed=${confirmed.removed ?? "?"}`,
      );
      this.setState({ listPage: { start: 0, total: 0, count: 0 } });
      await client.reqStorage();
      await client.reqList(0);
      return confirmed.status ?? 1;
    });
  }

  clearLogs = (): void => {
    this.setState({ logs: [] });
  };

  shareBench = async (): Promise<void> => {
    const client = this.client;
    if (!client || client.bench.rows.length === 0) {
      this.appendLog("[ui] bench: no rows to share yet");
      return;
    }
    try {
      if (!(await isAvailableAsync())) {
        this.appendLog("[ui] bench: sharing is not available on this device");
        return;
      }
      const dir = new Directory(Paths.cache, "checkpoint");
      if (!dir.exists) dir.create();
      const file = new File(dir, `bench-${Date.now()}.csv`);
      if (file.exists) file.delete();
      file.create();
      file.write(new TextEncoder().encode(client.bench.toCsv()));
      await shareAsync(file.uri, { dialogTitle: "Share bench CSV" });
    } catch (error) {
      this.appendLog(`[ui] bench share failed: ${error instanceof Error ? error.message : "unknown"}`);
    }
  }
}

export const syncEngine = new SyncEngine();
