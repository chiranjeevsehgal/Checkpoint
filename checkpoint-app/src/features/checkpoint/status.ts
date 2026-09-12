export type StatusTone = 'success' | 'primary' | 'warning' | 'danger' | 'muted';

export type CheckpointStatus =
  | 'ready'
  | 'connected'
  | 'disconnected'
  | 'recording'
  | 'receiving'
  | 'analyzing'
  | 'uploading'
  | 'uploaded'
  | 'filtered'
  | 'failed';

export interface StatusDescriptor {
  label: string;
  tone: StatusTone;
}

const STATUS: Record<CheckpointStatus, StatusDescriptor> = {
  ready: { label: 'Ready', tone: 'success' },
  connected: { label: 'Connected', tone: 'success' },
  disconnected: { label: 'Disconnected', tone: 'muted' },
  recording: { label: 'Recording', tone: 'primary' },
  receiving: { label: 'Receiving', tone: 'primary' },
  analyzing: { label: 'Analyzing', tone: 'warning' },
  uploading: { label: 'Uploading', tone: 'primary' },
  uploaded: { label: 'Uploaded', tone: 'success' },
  filtered: { label: 'Filtered', tone: 'muted' },
  failed: { label: 'Failed', tone: 'danger' },
};

export function statusDescriptor(status: CheckpointStatus): StatusDescriptor {
  return STATUS[status];
}

export const TONE_TEXT: Record<StatusTone, string> = {
  success: 'text-success',
  primary: 'text-primary',
  warning: 'text-warning',
  danger: 'text-destructive',
  muted: 'text-muted-foreground',
};

export const TONE_DOT: Record<StatusTone, string> = {
  success: 'bg-success',
  primary: 'bg-primary',
  warning: 'bg-warning',
  danger: 'bg-destructive',
  muted: 'bg-muted-foreground',
};
