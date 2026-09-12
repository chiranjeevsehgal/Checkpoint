import { BENCH_FIELDNAMES } from "./config.ts";
import { fileIdHex } from "./transfer.ts";

export interface BenchRow {
  ts: string;
  fileId: string;
  totalBytes: number;
  totalFrags: number;
  mtu: number;
  fragSize: number;
  goodputKBps: string;
  medianRttMs: string;
  p95RttMs: string;
  duplicates: number;
  retries: number;
  decryptFail: number;
  crcOk: boolean;
  resumeFrom: number;
  elapsedS: string;
  ingestUploadId: string;
  ingestStatus: string;
  ingestError: string;
  vadStatus: string;
  vadSpeechS: string;
}

function percentile(sorted: number[], frac: number): number {
  if (sorted.length === 0) return 0;
  return sorted[Math.min(Math.floor(sorted.length * frac), sorted.length - 1)]!;
}

function timestamp(): string {
  const now = new Date();
  const pad = (n: number) => n.toString().padStart(2, "0");
  return (
    `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}` +
    `T${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`
  );
}

export class BenchRecorder {
  readonly rows: BenchRow[] = [];
  duplicates = 0;
  decryptFail = 0;
  private gaps: number[] = [];
  private lastArrival = 0;
  private startTime = 0;
  private active: {
    fileId: bigint;
    total: number;
    totalFrags: number;
    resumeFrom: number;
  } | null = null;

  resetFile(
    fileId: bigint,
    total: number,
    totalFrags: number,
    resumeFrom: number,
  ): void {
    this.startTime = Date.now();
    this.gaps = [];
    this.duplicates = 0;
    this.decryptFail = 0;
    this.lastArrival = 0;
    this.active = { fileId, total, totalFrags, resumeFrom };
  }

  noteDataArrival(now = Date.now()): void {
    if (this.gaps.length >= 5000) return;
    const gap = this.lastArrival === 0 ? 0 : now - this.lastArrival;
    this.gaps.push(gap);
    this.lastArrival = now;
  }

  finalize(
    crcOk: boolean,
    total: number,
    mtu: number,
    fragSize: number,
    ingestStatus = "",
    vadStatus = "",
  ): BenchRow | null {
    const meta = this.active;
    if (!meta) return null;
    const elapsed = (Date.now() - this.startTime) / 1000;
    const goodput = elapsed > 0 ? meta.total / elapsed / 1024 : 0;
    const sorted = [...this.gaps].sort((a, b) => a - b);
    const row: BenchRow = {
      ts: timestamp(),
      fileId: fileIdHex(meta.fileId),
      totalBytes: total,
      totalFrags: meta.totalFrags,
      mtu,
      fragSize,
      goodputKBps: goodput.toFixed(2),
      medianRttMs: percentile(sorted, 0.5).toFixed(1),
      p95RttMs: percentile(sorted, 0.95).toFixed(1),
      duplicates: this.duplicates,
      retries: 0,
      decryptFail: this.decryptFail,
      crcOk,
      resumeFrom: meta.resumeFrom,
      elapsedS: elapsed.toFixed(2),
      ingestUploadId: "",
      ingestStatus,
      ingestError: "",
      vadStatus,
      vadSpeechS: "",
    };
    this.rows.push(row);
    this.active = null;
    return row;
  }

  updateIngest(
    fileIdHexValue: string,
    uploadId: string,
    status: string,
    error = "",
    vadStatus?: string,
    vadSpeechS?: string,
  ): void {
    const row = this.rows.find((r) => r.fileId === fileIdHexValue);
    if (!row) return;
    row.ingestUploadId = uploadId;
    row.ingestStatus = status;
    row.ingestError = error.slice(0, 200);
    if (vadStatus !== undefined) row.vadStatus = vadStatus;
    if (vadSpeechS !== undefined) row.vadSpeechS = vadSpeechS;
  }

  toCsv(): string {
    const cell = (value: string | number | boolean): string => {
      const text = typeof value === "boolean" ? (value ? "True" : "False") : String(value);
      return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
    };
    const lines = [BENCH_FIELDNAMES.join(",")];
    for (const row of this.rows) {
      lines.push(
        BENCH_FIELDNAMES.map((field) => cell(row[field as keyof BenchRow])).join(","),
      );
    }
    return lines.join("\n") + "\n";
  }
}
