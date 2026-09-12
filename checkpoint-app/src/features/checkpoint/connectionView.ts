export type ActivityTone = "recording" | "capturing" | "syncing" | "connected" | "disconnected";

export interface ConnectionActivity {
  label: string;
  tone: ActivityTone;
}

export function formatFingerprint(deviceIdHex: string | null): string {
  if (!deviceIdHex || deviceIdHex.length < 8) return "";
  const hex = deviceIdHex.toUpperCase();
  return `${hex.slice(0, 4)}-${hex.slice(4, 8)}`;
}

export function connectionActivity(input: {
  connected: boolean;
  recording: boolean;
  vadActive: boolean;
  syncing: number;
}): ConnectionActivity {
  if (!input.connected) return { label: "Not connected", tone: "disconnected" };
  if (input.recording) return { label: "Recording now", tone: "recording" };
  if (input.vadActive) return { label: "Capturing speech", tone: "capturing" };
  if (input.syncing > 0) return { label: `Syncing ${input.syncing}`, tone: "syncing" };
  return { label: "Idle · listening", tone: "connected" };
}
