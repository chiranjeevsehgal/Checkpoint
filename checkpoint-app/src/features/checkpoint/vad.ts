export type VadStatus = "speech" | "no-speech" | "disabled" | "unavailable";

export interface VadVerdict {
  status: VadStatus;
  speechS: number;
}

export interface VadOptions {
  threshold: number;
  minSpeechS: number;
}

export function shouldUpload(verdict: VadVerdict): boolean {
  return verdict.status !== "no-speech";
}

export function vadSkipReason(verdict: VadVerdict, minSpeechS: number): string {
  return `no human speech (${verdict.speechS.toFixed(2)}s < ${minSpeechS}s)`;
}

export async function checkSpeech(
  _audio: Uint8Array,
  _options: VadOptions,
): Promise<VadVerdict> {
  return { status: "unavailable", speechS: 0 };
}
