import { Asset } from 'expo-asset';
import { InferenceSession, Tensor } from 'onnxruntime-react-native';
import { decodeAudioData } from 'react-native-audio-api';

import {
  VAD_MIN_SILENCE_MS,
  VAD_MIN_SPEECH_MS,
  VAD_PAD_MS,
  VAD_SAMPLE_RATE,
  VAD_WINDOW_SAMPLES,
} from './config.ts';
import { repairOpusOgg } from './ogg.ts';
import { buildVadWindows, groupSpeechProbs, totalSpeechSeconds } from './vadTimestamps.ts';

export type VadStatus = 'speech' | 'no-speech' | 'disabled' | 'unavailable';

export interface VadVerdict {
  status: VadStatus;
  speechS: number;
  error?: string;
}

export interface VadOptions {
  threshold: number;
  minSpeechS: number;
}

export function shouldUpload(verdict: VadVerdict): boolean {
  return verdict.status !== 'no-speech';
}

export function vadSkipReason(verdict: VadVerdict, minSpeechS: number): string {
  return `no human speech (${verdict.speechS.toFixed(2)}s < ${minSpeechS}s)`;
}

// eslint-disable-next-line @typescript-eslint/no-require-imports
const modelAsset = require('../../../assets/models/silero_vad.onnx') as number;

let sessionPromise: Promise<InferenceSession> | null = null;

function ensureSession(): Promise<InferenceSession> {
  sessionPromise ??= (async () => {
    const asset = Asset.fromModule(modelAsset);
    await asset.downloadAsync();
    if (!asset.localUri) throw new Error('VAD model asset has no local URI');
    return InferenceSession.create(asset.localUri);
  })();
  return sessionPromise;
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.length);
  copy.set(bytes);
  return copy.buffer as ArrayBuffer;
}

async function decodeMono16k(repaired: Uint8Array): Promise<Float32Array> {
  const buffer = await decodeAudioData(toArrayBuffer(repaired), VAD_SAMPLE_RATE);
  const channels = buffer.numberOfChannels;
  if (channels < 1) throw new Error('Decoded audio has no channels');
  const first = buffer.getChannelData(0);
  if (channels === 1) return Float32Array.from(first);
  const mono = new Float32Array(first.length);
  for (let i = 0; i < mono.length; i++) mono[i] = first[i]! / channels;
  for (let ch = 1; ch < channels; ch++) {
    const data = buffer.getChannelData(ch);
    for (let i = 0; i < mono.length; i++) mono[i]! += data[i]! / channels;
  }
  return mono;
}

async function speechProbs(session: InferenceSession, pcm: Float32Array): Promise<number[]> {
  let state = new Float32Array(2 * 1 * 128);
  const sr = new Tensor('int64', BigInt64Array.of(BigInt(VAD_SAMPLE_RATE)), []);
  const probs: number[] = [];
  for (const window of buildVadWindows(pcm)) {
    const feeds = {
      input: new Tensor('float32', window, [1, window.length]),
      state: new Tensor('float32', state, [2, 1, 128]),
      sr,
    };
    const out = await session.run(feeds);
    const speech = out.output;
    const nextState = out.stateN;
    if (!speech || !nextState) throw new Error('VAD model returned no output');
    probs.push((speech.data as Float32Array)[0]!);
    state = Float32Array.from(nextState.data as Float32Array);
  }
  return probs;
}

export async function checkSpeech(audio: Uint8Array, options: VadOptions): Promise<VadVerdict> {
  try {
    const session = await ensureSession();
    const pcm = await decodeMono16k(repairOpusOgg(audio));
    if (pcm.length === 0) return { status: 'unavailable', speechS: 0 };
    const probs = await speechProbs(session, pcm);
    const spans = groupSpeechProbs(probs, {
      threshold: options.threshold,
      samplingRate: VAD_SAMPLE_RATE,
      windowSamples: VAD_WINDOW_SAMPLES,
      minSpeechMs: VAD_MIN_SPEECH_MS,
      minSilenceMs: VAD_MIN_SILENCE_MS,
      padMs: VAD_PAD_MS,
      audioLengthSamples: pcm.length,
    });
    const total = totalSpeechSeconds(spans);
    if (total >= options.minSpeechS) return { status: 'speech', speechS: total };
    return { status: 'no-speech', speechS: total };
  } catch (error) {
    // Fail-open by design, but keep the reason visible to the caller.
    return {
      status: 'unavailable',
      speechS: 0,
      error: error instanceof Error ? error.message : 'unknown',
    };
  }
}
