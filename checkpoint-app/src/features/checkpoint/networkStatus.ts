import {
  HEALTH_SLOW_MS,
  HEALTH_UNSTABLE_FAILS,
  HEALTH_UNSTABLE_WINDOW_MS,
} from "./config.ts";

export type Connectivity = "online" | "offline" | "unstable" | "server-unavailable";

export interface NetworkReachability {
  isConnected?: boolean;
  isInternetReachable?: boolean;
}

export interface HealthProbe {
  ok: boolean;
  latencyMs: number;
  at: number;
}

function consecutiveFailures(probes: HealthProbe[]): number {
  let count = 0;
  for (let i = probes.length - 1; i >= 0 && !probes[i]!.ok; i--) count += 1;
  return count;
}

export function classifyConnectivity(
  network: NetworkReachability,
  probes: HealthProbe[],
  now: number,
): Connectivity {
  if (network.isInternetReachable === false || network.isConnected === false) {
    return "offline";
  }
  const recent = probes.filter((probe) => now - probe.at <= HEALTH_UNSTABLE_WINDOW_MS);
  if (recent.length === 0) return "online";
  const last = recent[recent.length - 1]!;
  if (last.ok) {
    return last.latencyMs > HEALTH_SLOW_MS ? "unstable" : "online";
  }
  return consecutiveFailures(recent) >= HEALTH_UNSTABLE_FAILS
    ? "unstable"
    : "server-unavailable";
}
