import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { PropsWithChildren } from "react";
import { Alert } from "react-native";
import { BleManager } from "react-native-ble-plx";
import { openSettings } from "expo-linking";
import { Directory, File, Paths } from "expo-file-system";
import { isAvailableAsync, shareAsync } from "expo-sharing";

import { CheckpointClient, type CompletedFile } from "../client.ts";
import { parseClaimHex } from "../claim.ts";
import { ensureBlePermissions } from "../permissions.ts";
import {
  CTRL_ERASE_ARM,
  CTRL_ERASE_CONFIRM,
  CTRL_OK,
  DEVICE_NAME,
  KAFKA_TOPIC_HINT,
  STATUS_POLL_INTERVAL_S,
} from "../config.ts";
import { IngestionUploader } from "../ingestion.ts";
import { ctrlStatusText } from "../parsers.ts";
import {
  defaultSettings,
  loadSettings,
  saveSettings,
  type CheckpointSettings,
} from "../settings.ts";
import { checkSpeech, shouldUpload, vadSkipReason } from "../vad.ts";
import type {
  CheckpointEvent,
  DeviceFileList,
  DeviceStatus,
  StorageInfo,
} from "../types.ts";

export interface TransferInfo {
  fileId: string;
  totalBytes: number;
  received: number;
  totalFrags: number;
  vad: string;
  ingest: string;
}

interface ListPage {
  start: number;
  total: number;
  count: number;
}

const MAX_LOG_LINES = 200;

function pushLog(lines: string[], line: string): string[] {
  const next = [...lines, line];
  return next.length > MAX_LOG_LINES
    ? next.slice(next.length - MAX_LOG_LINES)
    : next;
}

function applyEventToTransfers(
  transfers: TransferInfo[],
  event: CheckpointEvent,
): TransferInfo[] {
  switch (event.type) {
    case "announce":
      if (transfers.some((t) => t.fileId === event.fileId)) return transfers;
      return [
        ...transfers,
        {
          fileId: event.fileId,
          totalBytes: event.totalBytes,
          received: 0,
          totalFrags: event.totalFrags,
          vad: "—",
          ingest: "pending",
        },
      ];
    case "progress":
      return transfers.map((t) =>
        t.fileId === event.fileId
          ? { ...t, received: event.received }
          : t,
      );
    case "file_done":
      return transfers.map((t) =>
        t.fileId === event.fileId
          ? {
              ...t,
              ingest: event.ingestStatus || t.ingest,
              vad: event.vadStatus || t.vad,
            }
          : t,
      );
    case "vad":
      return transfers.map((t) =>
        t.fileId === event.fileId
          ? { ...t, vad: `${event.vadStatus} ${event.vadSpeechS}s`.trim() }
          : t,
      );
    case "ingest": {
      const text = event.ingestError
        ? `${event.ingestStatus}: ${event.ingestError.slice(0, 60)}`
        : `${event.ingestStatus} ${event.uploadId.slice(0, 8)}`.trim();
      return transfers.map((t) =>
        t.fileId === event.fileId
          ? {
              ...t,
              ingest: text,
              vad: event.vadStatus
                ? `${event.vadStatus} ${event.vadSpeechS ?? ""}s`.trim()
                : t.vad,
            }
          : t,
      );
    }
    default:
      return transfers;
  }
}

interface CheckpointContextValue {
  connected: boolean;
  busy: boolean;
  linkState: string;
  deviceName: string;
  setDeviceName: (name: string) => void;
  claimText: string;
  setClaimText: (text: string) => void;
  status: DeviceStatus | null;
  storage: StorageInfo | null;
  fileList: DeviceFileList | null;
  listPage: ListPage;
  transfers: TransferInfo[];
  logs: string[];
  settings: CheckpointSettings;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  refreshStatus: () => Promise<void>;
  refreshStorage: () => Promise<void>;
  listPrev: () => Promise<void>;
  listNext: () => Promise<void>;
  toggleRec: () => Promise<void>;
  applyLed: (muted: boolean, brightness: number) => Promise<void>;
  applySync: (enabled: boolean) => Promise<void>;
  deleteFile: (path: string) => void;
  eraseStorage: () => void;
  updateSettings: (settings: CheckpointSettings) => Promise<void>;
  shareBench: () => Promise<void>;
  clearLogs: () => void;
  needsSettings: boolean;
  openAppSettings: () => Promise<void>;
}

const CheckpointContext = createContext<CheckpointContextValue | null>(null);

export function useCheckpoint(): CheckpointContextValue {
  const value = useContext(CheckpointContext);
  if (!value) throw new Error("useCheckpoint must be used inside CheckpointProvider");
  return value;
}

export function CheckpointProvider({ children }: PropsWithChildren) {
  const [connected, setConnected] = useState(false);
  const [busy, setBusy] = useState(false);
  const [linkState, setLinkState] = useState("idle");
  const [deviceName, setDeviceName] = useState(DEVICE_NAME);
  const [claimText, setClaimText] = useState("");
  const [status, setStatus] = useState<DeviceStatus | null>(null);
  const [storage, setStorage] = useState<StorageInfo | null>(null);
  const [fileList, setFileList] = useState<DeviceFileList | null>(null);
  const [listPage, setListPage] = useState<ListPage>({ start: 0, total: 0, count: 0 });
  const [transfers, setTransfers] = useState<TransferInfo[]>([]);
  const [logs, setLogs] = useState<string[]>([]);
  const [settings, setSettings] = useState<CheckpointSettings>(defaultSettings);
  const [needsSettings, setNeedsSettings] = useState(false);

  const managerRef = useRef<BleManager | null>(null);
  const clientRef = useRef<CheckpointClient | null>(null);
  const stoppedRef = useRef(false);
  const settingsRef = useRef(settings);

  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  const appendLog = useCallback((line: string) => {
    // Mirrored to logcat so field issues can be diagnosed over adb.
    console.log(line);
    setLogs((prev) => pushLog(prev, line));
  }, []);

  const applyEvent = useCallback((event: CheckpointEvent) => {
    if (event.type === "link") {
      setLinkState(event.state);
      return;
    }
    if (event.type === "rec_status") {
      const { type: _ignored, ...rest } = event;
      setStatus(rest);
      return;
    }
    if (event.type === "storage") {
      const { type: _ignored, ...rest } = event;
      setStorage(rest);
      return;
    }
    if (event.type === "file_list") {
      const { type: _ignored, ...rest } = event;
      setFileList(rest);
      setListPage({
        start: rest.start,
        total: rest.total,
        count: rest.entries.length,
      });
      return;
    }
    setTransfers((prev) => applyEventToTransfers(prev, event));
  }, []);

  const handleCompletedFile = useCallback(
    async (client: CheckpointClient, file: CompletedFile) => {
      const current = await loadSettings();
      const verdict = current.vadEnabled
        ? await checkSpeech(file.bytes, {
            threshold: current.vadThreshold,
            minSpeechS: current.minSpeechS,
          })
        : { status: "disabled" as const, speechS: 0 };
      if (!shouldUpload(verdict)) {
        const reason = vadSkipReason(verdict, current.minSpeechS);
        appendLog(`  [vad] filtered ${file.fileIdHex}: ${reason} — skipping upload`);
        client.bench.updateIngest(
          file.fileIdHex,
          "",
          "skipped-no-speech",
          reason,
          verdict.status,
          verdict.speechS.toFixed(2),
        );
        if (!current.keepFiles) {
          await CheckpointClient.deleteLocalCopy(file.fileIdHex);
        }
        return;
      }
      if (!current.ingestEnabled) {
        client.bench.updateIngest(file.fileIdHex, "", "disabled", "", "disabled", "0.00");
        return;
      }
      appendLog(`  [ingest] uploading file_${file.fileIdHex}.ogg (${file.bytes.length}B) ...`);
      const uploader = new IngestionUploader(current.serverUrl, current.userId);
      try {
        const result = await uploader.upload(
          file.bytes,
          `file_${file.fileIdHex}.ogg`,
          "audio/ogg",
          file.fileIdHex,
        );
        appendLog(`  [ingest] OK upload_id=${result.uploadId} status=${result.status}`);
        client.bench.updateIngest(
          file.fileIdHex,
          result.uploadId,
          result.status,
          "",
          verdict.status,
          verdict.speechS.toFixed(2),
        );
      } catch (error) {
        const message = error instanceof Error ? error.message.slice(0, 200) : "unknown";
        appendLog(`  [!] ingest failed: ${message}`);
        client.bench.updateIngest(
          file.fileIdHex,
          "",
          "failed",
          message,
          verdict.status,
          verdict.speechS.toFixed(2),
        );
        return;
      }
      if (!current.keepFiles) {
        await CheckpointClient.deleteLocalCopy(file.fileIdHex);
      }
    },
    [appendLog],
  );

  const withClient = useCallback(
    async <T,>(action: string, run: (client: CheckpointClient) => Promise<T>): Promise<T | null> => {
      const client = clientRef.current;
      if (!client) return null;
      try {
        return await run(client);
      } catch (error) {
        appendLog(`[ui] ${action} failed: ${error instanceof Error ? error.message : "unknown"}`);
        return null;
      }
    },
    [appendLog],
  );

  const refreshStatus = useCallback(async () => {
    await withClient("status", async (client) => client.reqStatus());
  }, [withClient]);

  const refreshStorage = useCallback(async () => {
    await withClient("storage", async (client) => {
      await client.reqStorage();
      await client.reqList(0);
    });
  }, [withClient]);

  const connect = useCallback(async () => {
    if (connected || busy) return;
    setBusy(true);
    setLinkState("connecting");
    setNeedsSettings(false);
    let manager: BleManager;
    try {
      const current = await loadSettings();
      setSettings(current);
      settingsRef.current = current;
      if (!managerRef.current) managerRef.current = new BleManager();
      manager = managerRef.current;
      const gate = await ensureBlePermissions();
      if (gate !== "granted") {
        appendLog(
          `[ui] missing Bluetooth permission (${gate}) — grant Nearby devices + Location and retry`,
        );
        setNeedsSettings(gate === "needs-settings");
        setBusy(false);
        setLinkState(gate === "needs-settings" ? "needs permission" : "permission denied");
        return;
      }
      appendLog("[ui] permissions granted");
      const adapter = await manager.state();
      if (adapter !== "PoweredOn") {
        appendLog(
          adapter === "PoweredOff"
            ? "[ui] Bluetooth is off — turn it on and retry"
            : `[ui] Bluetooth unavailable (${adapter}) — check system settings and retry`,
        );
        setNeedsSettings(adapter === "Unauthorized");
        setBusy(false);
        setLinkState(adapter === "PoweredOff" ? "bluetooth off" : "bluetooth unavailable");
        return;
      }
      appendLog(`[ble] adapter ${adapter} — scanning...`);
    } catch (error) {
      appendLog(`[ui] pre-connect check failed: ${error instanceof Error ? error.message : "unknown"}`);
      setBusy(false);
      setLinkState("idle");
      return;
    }
    let claimKey: Uint8Array | null = null;
    const trimmedClaim = claimText.trim();
    if (trimmedClaim !== "") {
      try {
        claimKey = parseClaimHex(trimmedClaim);
      } catch (error) {
        appendLog(`[ui] bad claim key: ${error instanceof Error ? error.message : "unknown"}`);
        setBusy(false);
        setLinkState("idle");
        return;
      }
    }
    let client: CheckpointClient;
    try {
      client = new CheckpointClient(manager, deviceName.trim() || DEVICE_NAME, {
        onEvent: applyEvent,
        log: appendLog,
        onFile: (file) => {
          const active = clientRef.current;
          if (active) void handleCompletedFile(active, file);
        },
      });
    } catch (error) {
      appendLog(`[ui] client init failed: ${error instanceof Error ? error.message : "unknown"}`);
      setBusy(false);
      setLinkState("idle");
      return;
    }
    clientRef.current = client;
    stoppedRef.current = false;
    const target = deviceName.trim() || DEVICE_NAME;
    const enrollKey = claimKey;
    void (async () => {
      let lastPoll = 0;
      try {
        await client.supervise(target, enrollKey, {
          stopped: () => stoppedRef.current,
          onReady: async () => {
            setConnected(true);
            setBusy(false);
            appendLog("[ui] listening for file transfers …");
            try {
              await client.reqStatus();
            } catch (error) {
              appendLog(`[ui] initial status failed: ${error instanceof Error ? error.message : "unknown"}`);
            }
            try {
              await client.reqStorage();
              await client.reqList(0);
            } catch (error) {
              appendLog(`[ui] initial storage load failed: ${error instanceof Error ? error.message : "unknown"}`);
            }
            appendLog(`[ui] queue-wait until SUBMITTED (Kafka ${KAFKA_TOPIC_HINT})`);
          },
          onAlive: async () => {
            setLinkState("listening");
          },
          onTick: async () => {
            const now = Date.now();
            if (now - lastPoll < STATUS_POLL_INTERVAL_S * 1000) return;
            lastPoll = now;
            try {
              await client.reqStatus();
            } catch (error) {
              appendLog(`[ui] status poll failed: ${error instanceof Error ? error.message : "unknown"}`);
            }
          },
        });
      } catch (error) {
        appendLog(`[ui] connect failed: ${error instanceof Error ? error.message : "unknown"}`);
      }
      setConnected(false);
      setBusy(false);
      setLinkState("idle");
      clientRef.current = null;
    })();
  }, [applyEvent, appendLog, busy, claimText, connected, deviceName, handleCompletedFile]);

  const disconnect = useCallback(async () => {
    stoppedRef.current = true;
    const client = clientRef.current;
    if (client) {
      try {
        await client.disconnect();
        appendLog("[ui] disconnected (bond kept — no re-pair needed)");
      } catch (error) {
        appendLog(`[ui] disconnect failed: ${error instanceof Error ? error.message : "unknown"}`);
      }
    }
    setConnected(false);
    setLinkState("idle");
  }, [appendLog]);

  const toggleRec = useCallback(async () => {
    await withClient("rec-toggle", async (client) => {
      const next = !(status?.recording ?? false);
      const code = next ? await client.cmdRecStart() : await client.cmdRecStop();
      appendLog(`[ui] rec-${next ? "start" : "stop"} status=${ctrlStatusText(code)}`);
      await client.reqStatus();
    });
  }, [appendLog, status?.recording, withClient]);

  const applyLed = useCallback(
    async (muted: boolean, brightness: number) => {
      await withClient("led-apply", async (client) => {
        const code = await client.cmdLedSet(muted, brightness);
        appendLog(`[ui] led-apply muted=${muted} bright=${brightness} status=${ctrlStatusText(code)}`);
        await client.reqStatus();
      });
    },
    [appendLog, withClient],
  );

  const applySync = useCallback(
    async (enabled: boolean) => {
      await withClient("sync-apply", async (client) => {
        const code = await client.cmdSyncSet(enabled);
        appendLog(`[ui] sync-apply enabled=${enabled} status=${ctrlStatusText(code)}`);
        await client.reqStatus();
      });
    },
    [appendLog, withClient],
  );

  const listPrev = useCallback(async () => {
    const start = Math.max(0, listPage.start - Math.max(1, listPage.count));
    await withClient("file-list", async (client) => client.reqList(start));
  }, [listPage, withClient]);

  const listNext = useCallback(async () => {
    const start = listPage.start + Math.max(1, listPage.count);
    if (listPage.total > 0 && start >= listPage.total) return;
    await withClient("file-list", async (client) => client.reqList(start));
  }, [listPage, withClient]);

  const deleteFile = useCallback(
    async (path: string) => {
      await withClient("file-delete", async (client) => {
        const code = await client.cmdFileDelete(path);
        appendLog(`[ui] file-delete ${path} status=${ctrlStatusText(code)}`);
        await client.reqStorage();
        await client.reqList(listPage.start);
      });
    },
    [appendLog, listPage.start, withClient],
  );

  const eraseStorage = useCallback(async () => {
    await withClient("storage-erase", async (client) => {
      const arm = await client.cmdStorageErase(CTRL_ERASE_ARM);
      if ((arm.status ?? 1) !== CTRL_OK) {
        appendLog(`[ui] erase arm refused status=${ctrlStatusText(arm.status ?? 1)}`);
        return;
      }
      const confirmed = await client.cmdStorageErase(CTRL_ERASE_CONFIRM);
      appendLog(
        `[ui] erase confirm status=${ctrlStatusText(confirmed.status ?? 1)} removed=${confirmed.removed ?? "?"}`,
      );
      setListPage({ start: 0, total: 0, count: 0 });
      await client.reqStorage();
      await client.reqList(0);
    });
  }, [appendLog, withClient]);

  const updateSettings = useCallback(
    async (next: CheckpointSettings) => {
      await saveSettings(next);
      setSettings(next);
      settingsRef.current = next;
      appendLog("[ui] settings saved");
    },
    [appendLog],
  );

  const clearLogs = useCallback(() => setLogs([]), []);

  const openAppSettings = useCallback(async () => {
    try {
      await openSettings();
    } catch (error) {
      appendLog(`[ui] open settings failed: ${error instanceof Error ? error.message : "unknown"}`);
    }
  }, [appendLog]);

  const shareBench = useCallback(async () => {
    const client = clientRef.current;
    if (!client || client.bench.rows.length === 0) {
      appendLog("[ui] bench: no rows to share yet");
      return;
    }
    try {
      if (!(await isAvailableAsync())) {
        appendLog("[ui] bench: sharing is not available on this device");
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
      appendLog(`[ui] bench share failed: ${error instanceof Error ? error.message : "unknown"}`);
    }
  }, [appendLog]);

  const confirmDelete = useCallback(
    (path: string) => {
      Alert.alert("Delete file", `Delete ${path} from the pendant? This cannot be undone.`, [
        { text: "Cancel", style: "cancel" },
        { text: "Delete", style: "destructive", onPress: () => void deleteFile(path) },
      ]);
    },
    [deleteFile],
  );

  const confirmErase = useCallback(() => {
    Alert.alert(
      "Erase all recordings",
      "Erase ALL recordings from the pendant SD card? This cannot be undone.",
      [
        { text: "Cancel", style: "cancel" },
        { text: "Erase all", style: "destructive", onPress: () => void eraseStorage() },
      ],
    );
  }, [eraseStorage]);

  const value = useMemo<CheckpointContextValue>(
    () => ({
      connected,
      busy,
      linkState,
      deviceName,
      setDeviceName,
      claimText,
      setClaimText,
      status,
      storage,
      fileList,
      listPage,
      transfers,
      logs,
      settings,
      connect,
      disconnect,
      refreshStatus,
      refreshStorage,
      listPrev,
      listNext,
      toggleRec,
      applyLed,
      applySync,
      deleteFile: confirmDelete,
      eraseStorage: confirmErase,
      updateSettings,
      shareBench,
      clearLogs,
      needsSettings,
      openAppSettings,
    }),
    [
      applyLed,
      applySync,
      busy,
      claimText,
      clearLogs,
      confirmDelete,
      confirmErase,
      connect,
      connected,
      deviceName,
      disconnect,
      fileList,
      linkState,
      listNext,
      listPage,
      listPrev,
      logs,
      needsSettings,
      openAppSettings,
      refreshStatus,
      refreshStorage,
      settings,
      shareBench,
      status,
      storage,
      toggleRec,
      transfers,
      updateSettings,
    ],
  );

  return <CheckpointContext.Provider value={value}>{children}</CheckpointContext.Provider>;
}
