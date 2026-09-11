import { formatBytes } from "./parsers.ts";

export type TransferStage = "receiving" | "analyzing" | "uploading" | "done";

export type TransferOutcome =
  | "pending"
  | "uploaded"
  | "filtered"
  | "skipped"
  | "failed"
  | "disabled";

export interface TransferInput {
  fileId: string;
  totalBytes: number;
  received: number;
  totalFrags: number;
  vad: string;
  ingest: string;
}

export interface TransferView {
  filename: string;
  sizeLabel: string;
  pct: number;
  stage: TransferStage;
  outcome: TransferOutcome;
  vadLabel: string;
  ingestLabel: string;
  failed: boolean;
}

function outcomeOf(stage: TransferStage, ingest: string): TransferOutcome {
  if (stage !== "done") return "pending";
  if (ingest === "skipped-no-speech") return "filtered";
  if (ingest.startsWith("skipped")) return "skipped";
  if (ingest === "disabled") return "disabled";
  if (ingest.startsWith("failed")) return "failed";
  return "uploaded";
}

const PENDING = new Set(["", "—", "pending"]);

function isPending(value: string): boolean {
  return PENDING.has(value);
}

function speechSeconds(vad: string): string {
  const [, seconds = ""] = vad.split(" ");
  return seconds.replace(/s$/, "");
}

function describeVad(vad: string, stage: TransferStage): string {
  if (stage === "receiving" || stage === "analyzing") {
    return vad === "disabled" ? "Voice gate off" : "Checking for speech…";
  }
  if (vad.startsWith("speech")) {
    const seconds = speechSeconds(vad);
    return seconds ? `Speech · ${seconds}s` : "Speech";
  }
  if (vad.startsWith("no-speech")) return "No speech detected";
  if (vad.startsWith("disabled")) return "Voice gate off";
  if (vad.startsWith("unavailable")) return "Couldn't analyze — uploaded anyway";
  return "—";
}

function describeIngest(ingest: string, stage: TransferStage): string {
  if (stage === "receiving" || stage === "analyzing") return "Waiting…";
  if (isPending(ingest)) return "Uploading…";
  if (ingest === "skipped-no-speech") return "Filtered — silence";
  if (ingest === "skipped-wav") return "Skipped — not Ogg";
  if (ingest === "skipped-too-large") return "Skipped — too large";
  if (ingest === "disabled") return "Upload off";
  if (ingest.startsWith("failed")) {
    const reason = ingest.replace(/^failed:?\s*/i, "");
    return reason ? `Failed — ${reason}` : "Failed";
  }
  const [status, uploadId] = ingest.split(" ");
  return uploadId ? `Uploaded · ${uploadId}` : status ?? ingest;
}

export function transferView(input: TransferInput): TransferView {
  const pct = input.totalFrags > 0 ? input.received / input.totalFrags : 0;
  let stage: TransferStage = "receiving";
  if (pct >= 1) {
    if (isPending(input.vad)) stage = "analyzing";
    else if (isPending(input.ingest)) stage = "uploading";
    else stage = "done";
  }

  return {
    filename: `file_${input.fileId}.ogg`,
    sizeLabel: formatBytes(input.totalBytes),
    pct,
    stage,
    outcome: outcomeOf(stage, input.ingest),
    vadLabel: describeVad(input.vad, stage),
    ingestLabel: describeIngest(input.ingest, stage),
    failed: input.ingest.startsWith("failed"),
  };
}
