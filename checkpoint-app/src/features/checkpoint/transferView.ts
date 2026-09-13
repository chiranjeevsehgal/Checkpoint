import { formatBytes } from './parsers.ts';
import type { CheckpointStatus } from './status.ts';

export type TransferStage = 'receiving' | 'analyzing' | 'uploading' | 'done';

export type TransferOutcome =
  'pending' | 'uploaded' | 'filtered' | 'skipped' | 'failed' | 'disabled';

export interface TransferInput {
  fileId: string;
  totalBytes: number;
  received: number;
  totalFrags: number;
  vad: string;
  ingest: string;
}

export interface TransferView {
  filename: string;
  sizeLabel: string;
  pct: number;
  stage: TransferStage;
  outcome: TransferOutcome;
  vadLabel: string;
  ingestLabel: string;
  failed: boolean;
  status: CheckpointStatus;
  headline: string;
}

export function classifyOutcome(complete: boolean, ingest: string): TransferOutcome {
  if (!complete) return 'pending';
  if (ingest === 'skipped-no-speech') return 'filtered';
  if (ingest.startsWith('skipped')) return 'skipped';
  if (ingest === 'disabled') return 'disabled';
  if (ingest.startsWith('failed')) return 'failed';
  return 'uploaded';
}

const PENDING_VALUES = new Set(['', '—', 'pending']);

export function isPendingValue(value: string): boolean {
  return PENDING_VALUES.has(value);
}

function speechSeconds(vad: string): string {
  const [, seconds = ''] = vad.split(' ');
  return seconds.replace(/s$/, '');
}

function describeVad(vad: string, stage: TransferStage): string {
  if (stage === 'receiving' || stage === 'analyzing') {
    return vad === 'disabled' ? 'Voice gate off' : 'Checking for speech…';
  }
  if (vad.startsWith('speech')) {
    const seconds = speechSeconds(vad);
    return seconds ? `Speech · ${seconds}s` : 'Speech';
  }
  if (vad.startsWith('no-speech')) return 'No speech detected';
  if (vad.startsWith('disabled')) return 'Voice gate off';
  if (vad.startsWith('unavailable')) return "Couldn't analyze — uploaded anyway";
  return '—';
}

function describeIngest(ingest: string, stage: TransferStage): string {
  if (stage === 'receiving' || stage === 'analyzing') return 'Waiting…';
  if (isPendingValue(ingest)) return 'Uploading…';
  if (ingest === 'skipped-no-speech') return 'Filtered — silence';
  if (ingest === 'skipped-wav') return 'Skipped — not Ogg';
  if (ingest === 'skipped-too-large') return 'Skipped — too large';
  if (ingest === 'disabled') return 'Upload off';
  if (ingest.startsWith('failed')) {
    const reason = ingest.replace(/^failed:?\s*/i, '');
    return reason ? `Failed — ${reason}` : 'Failed';
  }
  const [status, uploadId] = ingest.split(' ');
  return uploadId ? `Uploaded · ${uploadId}` : (status ?? ingest);
}

function transferStatus(view: {
  stage: TransferStage;
  outcome: TransferOutcome;
  failed: boolean;
}): CheckpointStatus {
  if (view.failed) return 'failed';
  if (view.outcome === 'uploaded') return 'uploaded';
  if (view.outcome === 'filtered' || view.outcome === 'skipped' || view.outcome === 'disabled') {
    return 'filtered';
  }
  if (view.stage === 'receiving') return 'receiving';
  if (view.stage === 'analyzing') return 'analyzing';
  if (view.stage === 'uploading') return 'uploading';
  return 'ready';
}

function transferHeadline(view: TransferView): string {
  if (view.failed) return 'Upload failed';
  if (view.outcome === 'uploaded') return 'Uploaded successfully';
  if (view.outcome === 'filtered') return 'No speech detected';
  if (view.outcome === 'skipped' || view.outcome === 'disabled') return view.ingestLabel;
  if (view.stage === 'receiving') return 'Receiving';
  if (view.stage === 'analyzing') return 'Analyzing';
  if (view.stage === 'uploading') return 'Uploading…';
  return 'Ready';
}

export function formatTransferTime(at: number, now: number = Date.now()): string {
  const date = new Date(at);
  const today = new Date(now);
  const sameDay =
    date.getFullYear() === today.getFullYear() &&
    date.getMonth() === today.getMonth() &&
    date.getDate() === today.getDate();
  if (!sameDay) {
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
  }
  const hours24 = date.getHours();
  const hours = hours24 % 12 === 0 ? 12 : hours24 % 12;
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const suffix = hours24 < 12 ? 'AM' : 'PM';
  return `${hours}:${minutes} ${suffix}`;
}

export interface TransferDayGroup<T> {
  title: string;
  data: T[];
}

function startOfDay(at: number): number {
  const date = new Date(at);
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

function transferDayTitle(at: number, now: number): string {
  const daysAgo = Math.round((startOfDay(now) - startOfDay(at)) / 86_400_000);
  if (daysAgo <= 0) return 'Today';
  if (daysAgo === 1) return 'Yesterday';
  return new Date(at).toLocaleDateString([], { month: 'short', day: 'numeric' });
}

/** Groups newest-first records into consecutive day sections. */
export function groupTransfersByDay<T extends { createdAt: number }>(
  records: T[],
  now: number = Date.now(),
): TransferDayGroup<T>[] {
  const groups: TransferDayGroup<T>[] = [];
  for (const record of records) {
    const title = transferDayTitle(record.createdAt, now);
    const current = groups[groups.length - 1];
    if (current?.title === title) current.data.push(record);
    else groups.push({ title, data: [record] });
  }
  return groups;
}

export function transferView(input: TransferInput): TransferView {
  const pct = input.totalFrags > 0 ? input.received / input.totalFrags : 0;
  let stage: TransferStage = 'receiving';
  if (pct >= 1) {
    if (isPendingValue(input.vad)) stage = 'analyzing';
    else if (isPendingValue(input.ingest)) stage = 'uploading';
    else stage = 'done';
  }

  const outcome = classifyOutcome(pct >= 1 && !isPendingValue(input.ingest), input.ingest);
  const failed = input.ingest.startsWith('failed');
  const view: TransferView = {
    filename: `file_${input.fileId}.ogg`,
    sizeLabel: formatBytes(input.totalBytes),
    pct,
    stage,
    outcome,
    vadLabel: describeVad(input.vad, stage),
    ingestLabel: describeIngest(input.ingest, stage),
    failed,
    status: transferStatus({ stage, outcome, failed }),
    headline: '',
  };
  view.headline = transferHeadline(view);
  return view;
}
