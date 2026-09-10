import { hexToBytes } from "./crypto.ts";

export function parseClaimHex(claimText: string): Uint8Array {
  let text = claimText.trim();
  const uriIndex = text.toLowerCase().indexOf("checkpoint://claim?");
  if (uriIndex >= 0) {
    const query = text.slice(uriIndex).split("?", 2)[1] ?? "";
    const fields: Record<string, string> = {};
    for (const part of query.split("&")) {
      const eq = part.indexOf("=");
      if (eq >= 0) fields[part.slice(0, eq)] = part.slice(eq + 1);
    }
    text = fields["key"] ?? "";
  }
  let claimKey: Uint8Array;
  try {
    claimKey = hexToBytes(text.trim());
  } catch {
    throw new Error("--claim must be 64 hex chars (32 bytes)");
  }
  if (claimKey.length === 16) {
    throw new Error(
      "That looks like the device id (16 bytes), not the claim key. " +
        "Paste the `claim` line (64 hex chars) or the full checkpoint://claim?... URI.",
    );
  }
  if (claimKey.length !== 32) {
    throw new Error("--claim must be 64 hex chars (32 bytes)");
  }
  return claimKey;
}
