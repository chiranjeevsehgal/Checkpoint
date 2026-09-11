import * as Network from "expo-network";

import {
  HEALTH_PATH,
  HEALTH_POLL_DOWN_MS,
  HEALTH_POLL_OK_MS,
  HEALTH_TIMEOUT_MS,
} from "./config.ts";
import {
  classifyConnectivity,
  type Connectivity,
  type HealthProbe,
  type NetworkReachability,
} from "./networkStatus.ts";

export interface NetworkSnapshot {
  state: Connectivity;
  latencyMs: number | null;
  lastCheckedAt: number | null;
}

const INITIAL_SNAPSHOT: NetworkSnapshot = {
  state: "online",
  latencyMs: null,
  lastCheckedAt: null,
};

class NetworkMonitor {
  private reachability: NetworkReachability = {};
  private probes: HealthProbe[] = [];
  private snapshot: NetworkSnapshot = INITIAL_SNAPSHOT;
  private listeners = new Set<() => void>();
  private subscription: { remove: () => void } | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private baseUrl = "http://localhost:8080";
  private started = false;
  private probing = false;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): NetworkSnapshot => this.snapshot;

  configure(baseUrl: string): void {
    this.baseUrl = baseUrl.trim().replace(/\/+$/, "");
  }

  async start(): Promise<void> {
    if (this.started) return;
    this.started = true;
    try {
      this.reachability = await Network.getNetworkStateAsync();
    } catch {
      this.reachability = {};
    }
    if (!this.started) return;
    this.subscription = Network.addNetworkStateListener((state) => {
      this.reachability = state;
      if (state.isInternetReachable === false || state.isConnected === false) {
        this.recompute();
        return;
      }
      void this.probeNow();
    });
    await this.probeNow();
  }

  stop(): void {
    this.started = false;
    this.subscription?.remove();
    this.subscription = null;
    this.clearTimer();
  }

  async probeNow(): Promise<HealthProbe | null> {
    if (this.probing) return null;
    if (this.reachability.isInternetReachable === false) {
      this.recompute();
      this.scheduleNext();
      return null;
    }
    this.probing = true;
    const startedAt = Date.now();
    let probe: HealthProbe;
    try {
      const response = await fetch(`${this.baseUrl}${HEALTH_PATH}`, {
        signal: AbortSignal.timeout(HEALTH_TIMEOUT_MS),
      });
      probe = { ok: response.ok, latencyMs: Date.now() - startedAt, at: Date.now() };
    } catch {
      probe = { ok: false, latencyMs: Date.now() - startedAt, at: Date.now() };
    } finally {
      this.probing = false;
    }
    this.probes.push(probe);
    if (this.probes.length > 20) this.probes.shift();
    this.recompute();
    this.scheduleNext();
    return probe;
  }

  private recompute(): void {
    const state = classifyConnectivity(this.reachability, this.probes, Date.now());
    const last = this.probes[this.probes.length - 1];
    this.snapshot = {
      state,
      latencyMs: last?.latencyMs ?? null,
      lastCheckedAt: last?.at ?? null,
    };
    for (const listener of this.listeners) listener();
  }

  private scheduleNext(): void {
    this.clearTimer();
    const delay = this.snapshot.state === "online" ? HEALTH_POLL_OK_MS : HEALTH_POLL_DOWN_MS;
    this.timer = setTimeout(() => void this.probeNow(), delay);
  }

  private clearTimer(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }
}

export const networkMonitor = new NetworkMonitor();
