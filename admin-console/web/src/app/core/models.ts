export type DeviceState = 'unowned' | 'owned' | 'reset_required';

export interface DeviceRecord {
  device_id: string;
  user_id: string | null;
  state: DeviceState | string;
  claimed_at: string | null;
  updated_at: string;
}

export type DeletionStatus = 'PENDING' | 'PROCESSING' | 'COMPLETE';

export interface DeletionRecord {
  user_id: string;
  status: DeletionStatus | string;
  attempt_count: number;
  last_error: string | null;
  next_attempt_at: string;
  created_at: string;
  updated_at: string;
  completed_at: string | null;
}

export interface Identity {
  id: string;
  state?: string;
  created_at?: string;
  traits: { email?: string; name?: string };
  verifiable_addresses?: { value: string; verified?: boolean; status?: string }[];
}

export interface Session {
  id: string;
  active?: boolean;
  issued_at?: string;
  expires_at?: string;
}

export interface ProvisionIdentity {
  deviceId: string;
  claimHash: string;
}

export interface ConsoleConfig {
  host: string;
  port: number;
  repoRoot: string;
  databaseUrl: string;
  kratosAdminUrl: string;
  kratosPublicUrl: string;
  ingestionUrl: string;
  arduinoCli: string;
  sketchDir: string;
  buildDir: string;
  fqbn: string;
  dockerHost: string;
}

export interface ComposeService {
  Service?: string;
  Name?: string;
  State?: string;
  Status?: string;
  Health?: string;
}

export interface InfraView {
  services: ComposeService[] | { error: string };
  health: { ingestion: boolean; kratos: boolean };
}

export interface TaskEvent {
  type: 'start' | 'command' | 'line' | 'exit' | 'failed';
  name?: string;
  command?: string;
  line?: string;
  code?: number;
  message?: string;
}

export interface SerialEvent {
  type: 'line' | 'sent' | 'status' | 'error';
  line?: string;
  open?: boolean;
  path?: string;
  message?: string;
}
