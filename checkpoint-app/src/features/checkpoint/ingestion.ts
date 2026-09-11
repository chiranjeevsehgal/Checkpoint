import { sha256 } from "@noble/hashes/sha2.js";

import { apiFetch, apiPutBytes } from "@/lib/api/api-client";

import {
  INGEST_MAX_BYTES,
  INGEST_POLL_INTERVAL_S,
  INGEST_POLL_TIMEOUT_S,
  INGEST_TIMEOUT_S,
} from "./config.ts";
import { bytesToHex } from "./crypto.ts";
import { isValidUserId } from "./parsers.ts";

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function describeError(error: unknown): string {
  if (!(error instanceof Error)) return String(error);
  const cause = error.cause;
  const causeText = cause instanceof Error ? cause.message : cause ? String(cause) : "";
  if (causeText && !error.message.includes(causeText)) {
    return `${error.message} (cause: ${causeText})`;
  }
  return error.message;
}

function safeHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return "unknown-host";
  }
}

export interface CompletedUpload {
  uploadId: string;
  status: string;
}

interface CreateResponse {
  upload_id: string;
  upload: { url: string };
}

interface StatusResponse {
  status: string;
}

export class IngestionUploader {
  private readonly timeoutMs: number;
  private readonly pollIntervalMs: number;
  private readonly pollTimeoutMs: number;

  constructor(
    private readonly baseUrl: string,
    private readonly userId: string,
    timeoutS = INGEST_TIMEOUT_S,
    private readonly pollEnabled = true,
    pollTimeoutS = INGEST_POLL_TIMEOUT_S,
    pollIntervalS = INGEST_POLL_INTERVAL_S,
  ) {
    this.timeoutMs = Math.max(1, timeoutS) * 1000;
    this.pollIntervalMs = Math.max(0.1, pollIntervalS) * 1000;
    this.pollTimeoutMs = Math.max(0, pollTimeoutS) * 1000;
  }

  private signal(): AbortSignal | undefined {
    if (typeof AbortSignal.timeout === "function") {
      return AbortSignal.timeout(this.timeoutMs);
    }
    return undefined;
  }

  async getStatus(uploadId: string): Promise<string> {
    const body = await apiFetch<StatusResponse>(
      `${this.baseUrl}/v1/uploads/${uploadId}`,
      { signal: this.signal() },
      this.userId,
    );
    if (!body || typeof body.status !== "string" || body.status === "") {
      throw new Error(`get status: unexpected response for ${uploadId}`);
    }
    return body.status;
  }

  async waitSubmitted(uploadId: string): Promise<string> {
    let last = "READY";
    const deadline = Date.now() + this.pollTimeoutMs;
    while (Date.now() < deadline) {
      await sleep(this.pollIntervalMs);
      try {
        last = (await this.getStatus(uploadId)) || last;
      } catch {
        continue;
      }
      if (last === "SUBMITTED") break;
      if (last !== "READY" && last !== "UPLOADING") break;
    }
    return last;
  }

  async upload(
    data: Uint8Array,
    filename: string,
    contentType = "audio/ogg",
    idempotencyKey?: string,
  ): Promise<CompletedUpload> {
    if (data.length > INGEST_MAX_BYTES) {
      throw new Error(`too-large: ${data.length} > ${INGEST_MAX_BYTES}`);
    }
    if (!isValidUserId(this.userId)) {
      throw new Error("ingest user ID is not a valid UUID — check Settings > User ID");
    }
    const headers: Record<string, string> = {};
    if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey.slice(0, 128);
    let created: CreateResponse;
    try {
      created = await apiFetch<CreateResponse>(
        `${this.baseUrl}/v1/uploads`,
        {
          method: "POST",
          body: JSON.stringify({
            filename,
            content_type: contentType,
            size_bytes: data.length,
          }),
          signal: this.signal(),
          headers,
        },
        this.userId,
      );
    } catch (error) {
      throw new Error(`create failed: ${describeError(error)}`);
    }
    if (!created?.upload_id || !created?.upload?.url) {
      throw new Error("create: unexpected response");
    }
    try {
      await apiPutBytes(created.upload.url, data, contentType);
    } catch (error) {
      throw new Error(`put failed (${safeHost(created.upload.url)}): ${describeError(error)}`);
    }
    const checksum = bytesToHex(sha256(data));
    let completed: StatusResponse;
    try {
      completed = await apiFetch<StatusResponse>(
        `${this.baseUrl}/v1/uploads/${created.upload_id}/complete`,
        {
          method: "POST",
          body: JSON.stringify({ size_bytes: data.length, checksum_sha256: checksum }),
          signal: this.signal(),
        },
        this.userId,
      );
    } catch (error) {
      throw new Error(`complete failed: ${describeError(error)}`);
    }
    const doneStatus = completed?.status ?? "";
    if (doneStatus !== "READY" && doneStatus !== "SUBMITTED") {
      throw new Error(`complete: unexpected status ${doneStatus}`);
    }
    if (doneStatus === "SUBMITTED" || !this.pollEnabled) {
      return { uploadId: created.upload_id, status: doneStatus };
    }
    const final = await this.waitSubmitted(created.upload_id);
    return { uploadId: created.upload_id, status: final };
  }
}
