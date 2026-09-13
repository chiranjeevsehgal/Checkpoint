export interface SyncServiceGate {
  connected: boolean;
  autoSyncEnabled: boolean;
}

export function shouldRunSyncService({ connected, autoSyncEnabled }: SyncServiceGate): boolean {
  return connected || autoSyncEnabled;
}
