export const API_PORT = 8080;
export const KRATOS_PORT = 4433;
export const MCP_PORT = 1417;

const SCHEME_PATTERN = /^[a-z][a-z0-9+.-]*:\/\//i;
const PORT_PATTERN = /:\d+$/;

/** Accepts a bare host or a full URL and returns just the host, or null. */
export function normalizeHost(input: string | null | undefined): string | null {
  if (!input) return null;
  const withoutScheme = input.trim().replace(SCHEME_PATTERN, '');
  const host = (withoutScheme.split('/')[0] ?? '').replace(PORT_PATTERN, '');
  return host.length > 0 ? host : null;
}

export function apiUrlForHost(host: string): string {
  return `http://${host}:${API_PORT}`;
}

export function kratosUrlForHost(host: string): string {
  return `http://${host}:${KRATOS_PORT}`;
}

/** Full MCP endpoint as pasted into an MCP client. */
export function mcpUrlForHost(host: string): string {
  return `http://${host}:${MCP_PORT}/mcp`;
}
