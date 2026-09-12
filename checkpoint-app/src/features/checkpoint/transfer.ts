import { BLE_FRAG_SIZE, BLE_WINDOW } from './config.ts';
import { bytesToHex } from './crypto.ts';

export function fileIdHex(fileId: bigint): string {
  return fileId.toString(16).padStart(16, '0');
}

export function contigOf(received: Set<number>): number {
  let contig = -1;
  while (received.has(contig + 1)) contig += 1;
  return contig;
}

export class IncomingFile {
  readonly buffer: Uint8Array;
  readonly received = new Set<number>();
  contigSeq = -1;
  readonly fileId: bigint;
  readonly totalBytes: number;
  readonly totalFrags: number;
  readonly expectedCrc: number;
  readonly key: Uint8Array | null;
  readonly sessionId: number;
  readonly fragSize: number;

  constructor(
    fileId: bigint,
    totalBytes: number,
    totalFrags: number,
    expectedCrc: number,
    key: Uint8Array | null,
    sessionId: number,
    fragSize: number = BLE_FRAG_SIZE,
  ) {
    this.fileId = fileId;
    this.totalBytes = totalBytes;
    this.totalFrags = totalFrags;
    this.expectedCrc = expectedCrc;
    this.key = key;
    this.sessionId = sessionId;
    this.fragSize = fragSize;
    this.buffer = new Uint8Array(totalBytes);
  }

  addFragment(seq: number, data: Uint8Array): boolean {
    const offset = seq * this.fragSize;
    if (offset < 0 || offset + data.length > this.buffer.length) return false;
    this.buffer.set(data, offset);
    this.received.add(seq);
    this.contigSeq = contigOf(this.received);
    return true;
  }

  isComplete(): boolean {
    return this.received.size === this.totalFrags && this.contigSeq === this.totalFrags - 1;
  }

  data(): Uint8Array {
    return this.buffer.slice(0, this.totalBytes);
  }
}

export interface SidecarState {
  crc: string;
  total: number;
  totalFrags: number;
  fragSize: number;
  received: number[];
}

export function validateSidecar(
  sidecar: SidecarState | null,
  fileCrcHex: string,
  total: number,
  totalFrags: number,
  fragSize: number,
  partBytes: number,
): number[] | null {
  if (!sidecar) return null;
  if (
    sidecar.crc !== fileCrcHex ||
    sidecar.total !== total ||
    sidecar.totalFrags !== totalFrags ||
    sidecar.fragSize !== fragSize
  ) {
    return null;
  }
  const available = Math.floor(partBytes / fragSize);
  const seen = new Set<number>();
  for (const seq of sidecar.received) {
    if (Number.isInteger(seq) && seq >= 0 && seq < totalFrags && seq < available) {
      seen.add(seq);
    }
  }
  return [...seen].sort((a, b) => a - b);
}

export function resumeFrom(received: number[], totalFrags: number): number {
  const resume = contigOf(new Set(received)) + 1;
  if (resume <= 0 || resume >= totalFrags) return 0;
  return resume;
}

export function shouldSendAck(receivedCount: number, contig: number, totalFrags: number): boolean {
  return (
    contig >= 0 &&
    (receivedCount % BLE_WINDOW === 0 ||
      (contig + 1) % BLE_WINDOW === 0 ||
      receivedCount === totalFrags)
  );
}

export function crcHex(crc: number): string {
  return (crc >>> 0).toString(16).padStart(8, '0');
}

export function partFileName(fileId: bigint): string {
  return `file_${fileIdHex(fileId)}.part`;
}

export function deviceIdLabel(deviceId: Uint8Array): string {
  return bytesToHex(deviceId).slice(0, 8);
}
