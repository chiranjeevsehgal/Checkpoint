import { Directory, File, Paths } from 'expo-file-system';
import { useSyncExternalStore } from 'react';
import { AudioContext, decodeAudioData } from 'react-native-audio-api';

import { isOggOpus, repairOpusOgg } from './ogg.ts';

const PREVIEW_TTL_MS = 90_000;
const SLOW_PLAYBACK_RATE = 0.6;

type DecodedAudio = Awaited<ReturnType<typeof decodeAudioData>>;

export interface PlaybackSnapshot {
  uri: string | null;
  label: string | null;
  playing: boolean;
  paused: boolean;
}

const IDLE: PlaybackSnapshot = { uri: null, label: null, playing: false, paused: false };

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
  private buffer: DecodedAudio | null = null;
  private offsetSeconds = 0;
  private startedAt = 0;
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
    this.buffer = await decodeAudioData(toArrayBuffer(data));
    this.offsetSeconds = 0;
    this.startSource(0);
    this.setState({ uri: file.uri, label, playing: true, paused: false });
    this.scheduleDelete(file);
  };

  pause = (): void => {
    if (!this.source) return;
    const context = this.context;
    if (context) {
      this.offsetSeconds += (context.currentTime - this.startedAt) * SLOW_PLAYBACK_RATE;
    }
    this.stopSource();
    this.setState({ playing: false, paused: true });
  };

  resume = (): void => {
    if (!this.buffer || !this.snapshot.paused) return;
    const context = this.ensureContext();
    if (context.state === 'suspended') void context.resume();
    this.startSource(this.offsetSeconds);
    this.setState({ playing: true, paused: false });
  };

  stop = (): void => {
    if (this.deleteTimer) {
      clearTimeout(this.deleteTimer);
      this.deleteTimer = null;
    }
    this.stopSource();
    this.buffer = null;
    this.offsetSeconds = 0;
    this.startedAt = 0;
    this.setState(IDLE);
    clearPreviewDir();
  };

  private startSource(offset: number): void {
    const buffer = this.buffer;
    if (!buffer) return;
    const context = this.ensureContext();
    const source = context.createBufferSource({ pitchCorrection: true });
    source.buffer = buffer;
    source.connect(context.destination);
    source.playbackRate.value = SLOW_PLAYBACK_RATE;
    source.onEnded = () => this.handleEnded(source);
    source.start(0, offset);
    this.source = source;
    this.startedAt = context.currentTime;
  }

  private stopSource(): void {
    const source = this.source;
    this.source = null;
    if (!source) return;
    source.onEnded = null;
    try {
      source.stop();
    } catch {
      /* already stopped */
    }
  }

  private handleEnded(source: ReturnType<AudioContext['createBufferSource']>): void {
    if (this.source !== source) return;
    this.source = null;
    this.offsetSeconds = 0;
    this.setState({ playing: false, paused: false });
  }

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
