import { Directory, File, Paths } from 'expo-file-system';
import { useSyncExternalStore } from 'react';
import { AudioContext, decodeAudioData } from 'react-native-audio-api';

import { isOggOpus, repairOpusOgg } from './ogg.ts';

const PREVIEW_TTL_MS = 90_000;
const SLOW_PLAYBACK_RATE = 0.7;

export interface PlaybackSnapshot {
  uri: string | null;
  label: string | null;
  playing: boolean;
}

const IDLE: PlaybackSnapshot = { uri: null, label: null, playing: false };

function previewDir(): Directory {
  return new Directory(Paths.cache, 'checkpoint', 'preview');
}

function clearPreviewDir(): void {
  const dir = previewDir();
  if (!dir.exists) return;
  for (const entry of dir.list()) {
    try {
      entry.delete();
    } catch {
      /* stale temp files are best-effort */
    }
  }
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.length);
  copy.set(bytes);
  return copy.buffer as ArrayBuffer;
}

class PlaybackController {
  private context: AudioContext | null = null;
  private source: ReturnType<AudioContext['createBufferSource']> | null = null;
  private deleteTimer: ReturnType<typeof setTimeout> | null = null;
  private listeners = new Set<() => void>();
  private snapshot: PlaybackSnapshot = IDLE;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): PlaybackSnapshot => this.snapshot;

  play = async (uri: string, label: string): Promise<void> => {
    const file = new File(uri);
    if (!file.exists) throw new Error('Recording is no longer on this device.');
    const bytes = await file.bytes();
    await this.playBytes(bytes, label);
  };

  playBytes = async (bytes: Uint8Array, label: string): Promise<void> => {
    this.stop();
    const data = isOggOpus(bytes) ? repairOpusOgg(bytes) : bytes;
    const file = this.writePreview(data);
    const context = this.ensureContext();
    if (context.state === 'suspended') await context.resume();
    const source = context.createBufferSource({ pitchCorrection: true });
    source.buffer = await decodeAudioData(toArrayBuffer(data));
    source.connect(context.destination);
    source.playbackRate.value = SLOW_PLAYBACK_RATE;
    source.onEnded = () => {
      if (this.source === source) this.setState({ playing: false });
    };
    source.start();
    this.source = source;
    this.setState({ uri: file.uri, label, playing: true });
    this.scheduleDelete(file);
  };

  stop = (): void => {
    if (this.deleteTimer) {
      clearTimeout(this.deleteTimer);
      this.deleteTimer = null;
    }
    const source = this.source;
    this.source = null;
    if (source) {
      try {
        source.stop();
      } catch {
        /* already stopped */
      }
    }
    this.setState(IDLE);
    clearPreviewDir();
  };

  private ensureContext(): AudioContext {
    this.context ??= new AudioContext();
    return this.context;
  }

  private writePreview(data: Uint8Array): File {
    const dir = previewDir();
    dir.create({ intermediates: true, idempotent: true });
    clearPreviewDir();
    const file = new File(dir, `preview_${Date.now()}${isOggOpus(data) ? '.ogg' : '.wav'}`);
    file.create();
    file.write(data);
    return file;
  }

  private scheduleDelete(file: File): void {
    this.deleteTimer = setTimeout(() => {
      this.deleteTimer = null;
      try {
        if (file.exists) file.delete();
      } catch {
        /* best-effort */
      }
    }, PREVIEW_TTL_MS);
  }

  private setState(patch: Partial<PlaybackSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) listener();
  }
}

export const playback = new PlaybackController();

export function usePlayback(): PlaybackSnapshot {
  return useSyncExternalStore(playback.subscribe, playback.getSnapshot);
}
