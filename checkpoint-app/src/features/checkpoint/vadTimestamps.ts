import {
  VAD_CONTEXT_SAMPLES,
  VAD_MIN_SILENCE_MS,
  VAD_MIN_SPEECH_MS,
  VAD_PAD_MS,
  VAD_WINDOW_SAMPLES,
} from "./config.ts";

export interface SpeechSpan {
  start: number;
  end: number;
}

export interface GroupingOptions {
  threshold: number;
  samplingRate?: number;
  windowSamples?: number;
  minSpeechMs?: number;
  minSilenceMs?: number;
  padMs?: number;
  audioLengthSamples?: number;
}

/**
 * Frames PCM into Silero v5 model inputs: each window is the previous window's
 * trailing context followed by the next window of samples. The final short
 * window is zero-padded, matching the Silero reference.
 */
export function buildVadWindows(
  pcm: Float32Array,
  windowSamples = VAD_WINDOW_SAMPLES,
  contextSamples = VAD_CONTEXT_SAMPLES,
): Float32Array[] {
  const windows: Float32Array[] = [];
  let context = new Float32Array(contextSamples);
  for (let offset = 0; offset < pcm.length; offset += windowSamples) {
    const combined = new Float32Array(windowSamples + contextSamples);
    combined.set(context, 0);
    combined.set(pcm.subarray(offset, offset + windowSamples), contextSamples);
    windows.push(combined);
    context = combined.slice(windowSamples);
  }
  return windows;
}

/**
 * Port of silero-vad get_speech_timestamps grouping (utils_vad.py), with the
 * same defaults the Python client passes: infinite max speech duration, so
 * the max-speech cut branch (and its possible_ends bookkeeping, including
 * the 98 ms min_silence_at_max_speech default) is dead code here and
 * intentionally omitted. Returns spans in seconds, rounded like Python's
 * round(x, 1).
 */
export function groupSpeechProbs(
  probs: number[],
  opts: GroupingOptions,
): SpeechSpan[] {
  const threshold = opts.threshold;
  const samplingRate = opts.samplingRate ?? 16000;
  const windowSamples = opts.windowSamples ?? 512;
  const minSpeechSamples = ((samplingRate * (opts.minSpeechMs ?? VAD_MIN_SPEECH_MS)) / 1000);
  const padSamples = (samplingRate * (opts.padMs ?? VAD_PAD_MS)) / 1000;
  const minSilenceSamples =
    (samplingRate * (opts.minSilenceMs ?? VAD_MIN_SILENCE_MS)) / 1000;
  const audioLengthSamples = opts.audioLengthSamples ?? probs.length * windowSamples;

  const negThreshold = Math.max(threshold - 0.15, 0.01);
  const speeches: { start: number; end: number }[] = [];
  let triggered = false;
  let current: { start: number; end: number } | null = null;
  let tempEnd = 0;

  for (let i = 0; i < probs.length; i++) {
    const prob = probs[i]!;
    const cur = windowSamples * i;

    if (prob >= threshold && tempEnd) {
      tempEnd = 0;
    }
    if (prob >= threshold && !triggered) {
      triggered = true;
      current = { start: cur, end: 0 };
      continue;
    }
    if (prob < negThreshold && triggered && current) {
      if (!tempEnd) tempEnd = cur;
      if (cur - tempEnd < minSilenceSamples) continue;
      current.end = tempEnd;
      if (current.end - current.start > minSpeechSamples) {
        speeches.push(current);
      }
      current = null;
      tempEnd = 0;
      triggered = false;
    }
  }

  if (current && audioLengthSamples - current.start > minSpeechSamples) {
    current.end = audioLengthSamples;
    speeches.push(current);
  }

  for (let i = 0; i < speeches.length; i++) {
    const speech = speeches[i]!;
    if (i === 0) {
      speech.start = Math.max(0, speech.start - padSamples);
    }
    if (i !== speeches.length - 1) {
      const next = speeches[i + 1]!;
      const silence = next.start - speech.end;
      if (silence < 2 * padSamples) {
        speech.end += Math.floor(silence / 2);
        next.start = Math.max(0, next.start - silence / 2);
      } else {
        speech.end = Math.min(audioLengthSamples, speech.end + padSamples);
        next.start = Math.max(0, next.start - padSamples);
      }
    } else {
      speech.end = Math.min(audioLengthSamples, speech.end + padSamples);
    }
  }

  const toSeconds = (samples: number) =>
    Math.max(0, Math.round((samples / samplingRate) * 10) / 10);
  const audioSeconds = audioLengthSamples / samplingRate;
  return speeches.map((s) => ({
    start: toSeconds(s.start),
    end: Math.min(toSeconds(s.end), audioSeconds),
  }));
}

export function totalSpeechSeconds(spans: SpeechSpan[]): number {
  return spans.reduce((sum, s) => sum + (s.end - s.start), 0);
}
