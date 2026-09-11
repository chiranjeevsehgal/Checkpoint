import type { CheckpointEvent } from "./types.ts";
import { classifyOutcome, isPendingValue, type TransferOutcome } from "./transferView.ts";

export type { TransferOutcome };

export interface TransferRecord {
  fileId: string;
  createdAt: number;
  updatedAt: number;
  totalBytes: number;
  received: number;
  totalFrags: number;
  vad: string;
  ingest: string;
  outcome: TransferOutcome;
  uploadId?: string;
  localUri?: string;
  attempts: number;
  nextAttemptAt?: number;
}

const TERMINAL_OUTCOMES: ReadonlySet<TransferOutcome> = new Set<TransferOutcome>([
  "uploaded",
  "filtered",
  "skipped",
  "disabled",
]);

export function isTerminal(record: TransferRecord): boolean {
  return TERMINAL_OUTCOMES.has(record.outcome);
}

export function isComplete(record: TransferRecord): boolean {
  return record.totalFrags > 0 && record.received >= record.totalFrags;
}

function withOutcome(record: TransferRecord, now: number, patch: Partial<TransferRecord>): TransferRecord {
  const next = { ...record, ...patch, updatedAt: now };
  next.outcome = classifyOutcome(isComplete(next) && !isPendingValue(next.ingest), next.ingest);
  return next;
}

export function patchTransfer(
  records: TransferRecord[],
  fileId: string,
  now: number,
  patch: Partial<TransferRecord>,
): TransferRecord[] {
  return records.map((record) =>
    record.fileId === fileId ? withOutcome(record, now, patch) : record,
  );
}

export function applyEventToRecords(
  records: TransferRecord[],
  event: CheckpointEvent,
  now: number,
  localUri?: string,
): TransferRecord[] {
  switch (event.type) {
    case "announce": {
      if (records.some((record) => record.fileId === event.fileId)) return records;
      return [
        ...records,
        {
          fileId: event.fileId,
          createdAt: now,
          updatedAt: now,
          totalBytes: event.totalBytes,
          received: 0,
          totalFrags: event.totalFrags,
          vad: "—",
          ingest: "pending",
          outcome: "pending",
          attempts: 0,
        },
      ];
    }
    case "progress":
      return patchTransfer(records, event.fileId, now, { received: event.received });
    case "file_done":
      return records.map((record) =>
        record.fileId === event.fileId
          ? withOutcome(record, now, {
              received: record.totalFrags,
              ingest: event.ingestStatus || "pending",
              vad: event.vadStatus || "—",
              ...(localUri ? { localUri } : {}),
            })
          : record,
      );
    case "vad":
      return patchTransfer(records, event.fileId, now, {
        vad: `${event.vadStatus} ${event.vadSpeechS}s`.trim(),
      });
    case "ingest": {
      const ingest = event.ingestError
        ? `${event.ingestStatus}: ${event.ingestError.slice(0, 60)}`
        : `${event.ingestStatus} ${event.uploadId.slice(0, 8)}`.trim();
      return patchTransfer(records, event.fileId, now, {
        ingest,
        ...(event.uploadId ? { uploadId: event.uploadId } : {}),
        ...(event.vadStatus
          ? { vad: `${event.vadStatus} ${event.vadSpeechS ?? ""}s`.trim() }
          : {}),
      });
    }
    default:
      return records;
  }
}

export function sortTransfers(records: TransferRecord[]): TransferRecord[] {
  return [...records].sort(
    (a, b) => b.createdAt - a.createdAt || b.fileId.localeCompare(a.fileId),
  );
}

export function pruneExpired(
  records: TransferRecord[],
  now: number,
  retentionMs: number,
): TransferRecord[] {
  if (retentionMs <= 0) return records;
  return records.filter(
    (record) => !(isTerminal(record) && now - record.createdAt >= retentionMs),
  );
}
