export function pickDefaultPort(preferred: string, ports: string[]): string {
  if (preferred && ports.includes(preferred)) return preferred;
  return ports[0] ?? '';
}
