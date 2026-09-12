export interface Packet {
  version: number;
  type: number;
  seq: number;
  payload: Uint8Array;
}

export interface DeviceStatus {
  recording: boolean;
  vadActive: boolean;
  vadSpeech: boolean;
  muted: boolean;
  brightness: number;
  levelDbfs: number;
  pending: number;
  chunks: number;
  utterances: number;
  sync: boolean;
}

export interface StorageInfo {
  total: number;
  used: number;
  files: number;
  pending: number;
}

export interface DeviceFileEntry {
  name: string;
  size: number;
  flags: number;
}

export interface DeviceFileList {
  start: number;
  total: number;
  entries: DeviceFileEntry[];
}

export interface LedState {
  muted: boolean;
  brightness: number;
}

export type CheckpointEvent =
  | { type: "link"; state: "up" | "down" }
  | { type: "announce"; fileId: string; totalBytes: number; totalFrags: number }
  | { type: "progress"; fileId: string; received: number; totalFrags: number }
  | {
      type: "file_done";
      fileId: string;
      crcOk: boolean;
      totalBytes: number;
      ingestStatus: string;
      vadStatus: string;
    }
  | { type: "vad"; fileId: string; vadStatus: string; vadSpeechS: number }
  | {
      type: "ingest";
      fileId: string;
      uploadId: string;
      ingestStatus: string;
      ingestError: string;
      vadStatus?: string;
      vadSpeechS?: string;
    }
  | ({ type: "rec_status" } & DeviceStatus)
  | ({ type: "storage" } & StorageInfo)
  | ({ type: "file_list" } & DeviceFileList);
