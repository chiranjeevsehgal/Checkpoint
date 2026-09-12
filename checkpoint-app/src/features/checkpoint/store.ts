import { Directory, File, Paths } from 'expo-file-system';

import { partFileName } from './transfer.ts';
import type { SidecarState } from './transfer.ts';
import type { TransferRecord } from './transferStore.ts';

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

function checkpointDir(): Directory {
  return new Directory(Paths.document, 'checkpoint');
}

function receivedDir(): Directory {
  return new Directory(Paths.document, 'checkpoint', 'received');
}

export function ensureDirs(): void {
  for (const dir of [checkpointDir(), receivedDir()]) {
    if (!dir.exists) dir.create();
  }
}

export function partFile(fileIdHex: string): File {
  return new File(checkpointDir(), `${partFileName(BigInt(`0x${fileIdHex}`))}`);
}

export function sidecarFile(fileIdHex: string): File {
  return new File(checkpointDir(), `file_${fileIdHex}.part.json`);
}

export function receivedFile(fileIdHex: string, ext: string): File {
  return new File(receivedDir(), `file_${fileIdHex}${ext}`);
}

export function receivedMetaFile(fileIdHex: string): File {
  return new File(receivedDir(), `file_${fileIdHex}.json`);
}

export function transfersFile(): File {
  return new File(checkpointDir(), 'transfers.json');
}

export async function loadTransfers(): Promise<TransferRecord[]> {
  try {
    const file = transfersFile();
    if (!file.exists) return [];
    const parsed = JSON.parse(await file.text()) as unknown;
    return Array.isArray(parsed) ? (parsed as TransferRecord[]) : [];
  } catch {
    return [];
  }
}

export function saveTransfers(records: TransferRecord[]): void {
  try {
    ensureDirs();
    const file = transfersFile();
    if (file.exists) file.delete();
    file.create();
    file.write(textEncoder.encode(JSON.stringify(records)));
  } catch {
    /* transfer history is best-effort */
  }
}

export interface PartWriter {
  write(seq: number, plain: Uint8Array, fragSize: number): void;
  close(): void;
}

function openHandle(file: File) {
  return file.open();
}

export function openPart(fileIdHex: string, total: number): PartWriter | null {
  try {
    ensureDirs();
    const file = partFile(fileIdHex);
    if (!file.exists || file.size !== total) {
      if (file.exists) file.delete();
      file.create();
      file.write(new Uint8Array(total));
    }
    const handle = openHandle(file);
    return {
      write(seq, plain, fragSize) {
        try {
          handle.offset = seq * fragSize;
          handle.writeBytes(plain);
        } catch {
          try {
            handle.close();
          } catch {
            /* ignore */
          }
          throw new Error('part write failed');
        }
      },
      close() {
        try {
          handle.close();
        } catch {
          /* ignore */
        }
      },
    };
  } catch {
    return null;
  }
}

export async function readPartBytes(fileIdHex: string): Promise<Uint8Array | null> {
  try {
    const file = partFile(fileIdHex);
    if (!file.exists) return null;
    return await file.bytes();
  } catch {
    return null;
  }
}

export async function readSidecar(fileIdHex: string): Promise<SidecarState | null> {
  try {
    const file = sidecarFile(fileIdHex);
    if (!file.exists) return null;
    const raw = await file.text();
    const parsed = JSON.parse(raw) as Partial<SidecarState>;
    if (
      typeof parsed.crc !== 'string' ||
      typeof parsed.total !== 'number' ||
      typeof parsed.totalFrags !== 'number' ||
      typeof parsed.fragSize !== 'number' ||
      !Array.isArray(parsed.received)
    ) {
      return null;
    }
    return parsed as SidecarState;
  } catch {
    return null;
  }
}

export function writeSidecar(fileIdHex: string, state: SidecarState): void {
  try {
    ensureDirs();
    sidecarFile(fileIdHex).write(textEncoder.encode(JSON.stringify(state)));
  } catch {
    /* resume state is best-effort */
  }
}

export function deletePart(fileIdHex: string): void {
  for (const file of [partFile(fileIdHex), sidecarFile(fileIdHex)]) {
    try {
      if (file.exists) file.delete();
    } catch {
      /* ignore */
    }
  }
}

export function saveCompleted(
  fileIdHex: string,
  ext: string,
  data: Uint8Array,
  meta: Record<string, string | number>,
): string | null {
  try {
    ensureDirs();
    const out = receivedFile(fileIdHex, ext);
    if (out.exists) out.delete();
    out.create();
    out.write(data);
    const metaFile = receivedMetaFile(fileIdHex);
    if (metaFile.exists) metaFile.delete();
    metaFile.create();
    metaFile.write(textEncoder.encode(JSON.stringify(meta, null, 2)));
    return out.uri;
  } catch {
    return null;
  }
}

export async function readSavedBytes(uri: string): Promise<Uint8Array | null> {
  try {
    return await new File(uri).bytes();
  } catch {
    return null;
  }
}

export async function deleteSaved(fileIdHex: string): Promise<void> {
  for (const ext of ['.ogg', '.wav', '.json']) {
    try {
      const file = ext === '.json' ? receivedMetaFile(fileIdHex) : receivedFile(fileIdHex, ext);
      if (file.exists) file.delete();
    } catch {
      /* ignore */
    }
  }
}

export function decodeText(data: Uint8Array): string {
  return textDecoder.decode(data);
}
