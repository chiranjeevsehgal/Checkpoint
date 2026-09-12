import type { LogEntry } from './types.ts';

export type LogCategory = 'ble' | 'recording' | 'vad' | 'transfer' | 'server' | 'sync' | 'other';

export type LogFilter = LogCategory | 'all';

export const LOG_FILTERS: { id: LogFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'ble', label: 'BLE' },
  { id: 'recording', label: 'Recording' },
  { id: 'vad', label: 'VAD' },
  { id: 'transfer', label: 'Transfer' },
  { id: 'server', label: 'Server' },
  { id: 'sync', label: 'Sync' },
  { id: 'other', label: 'Other' },
];

function isTransfer(text: string): boolean {
  return (
    text.includes('[ingest]') ||
    text.includes('[preview]') ||
    text.includes('[resume]') ||
    text.includes('FILE_ANNOUNCE') ||
    text.includes('FILE_DONE') ||
    text.includes('READY_ACK') ||
    text.includes('[!] ingest')
  );
}

export function classifyLog(text: string): LogCategory {
  if (text.includes('rec-start') || text.includes('rec-stop')) return 'recording';
  if (text.startsWith('[ble]') || text.startsWith('Found:') || text.startsWith('Connected to')) {
    return 'ble';
  }
  if (text.includes('[vad]')) return 'vad';
  if (isTransfer(text)) return 'transfer';
  if (text.startsWith('[net]')) return 'server';
  if (text.startsWith('[sync]')) return 'sync';
  return 'other';
}

export function filterLogs(logs: LogEntry[], filter: LogFilter): LogEntry[] {
  if (filter === 'all') return logs;
  return logs.filter((entry) => classifyLog(entry.text) === filter);
}

export function isErrorLog(text: string): boolean {
  return text.includes('[!]') || /\b(error|failed|rejected|timeout|denied)\b/i.test(text);
}

export function formatLogTime(at: number): string {
  const date = new Date(at);
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');
  return `${hours}:${minutes}:${seconds}`;
}
