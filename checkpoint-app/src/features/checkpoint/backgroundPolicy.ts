export interface SyncServiceGate {
  connected: boolean;
  autoSyncEnabled: boolean;
  remindersEnabled: boolean;
}

export function shouldRunSyncService({
  connected,
  autoSyncEnabled,
  remindersEnabled,
}: SyncServiceGate): boolean {
  return connected || autoSyncEnabled || remindersEnabled;
}
