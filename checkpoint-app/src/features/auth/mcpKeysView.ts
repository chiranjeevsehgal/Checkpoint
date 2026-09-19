const MAX_NAME_LENGTH = 100;

/** Device-friendly default; modelName is nullable on web and emulators. */
export function defaultKeyName(modelName: string | null | undefined, platform: string): string {
  const model = modelName?.trim();
  if (model) return model;
  return platform === 'ios' ? 'iOS' : platform.charAt(0).toUpperCase() + platform.slice(1);
}

/** Trimmed name when it satisfies the server's 1-100 rule, otherwise null. */
export function normalizeKeyName(name: string): string | null {
  const trimmed = name.trim();
  if (trimmed.length === 0 || trimmed.length > MAX_NAME_LENGTH) return null;
  return trimmed;
}

export function claudeCodeCommand(endpoint: string, key: string): string {
  return `claude mcp add --transport http checkpoint ${endpoint} --header "Authorization: Bearer ${key}"`;
}

export function mcpJsonSnippet(endpoint: string, key: string): string {
  return JSON.stringify(
    { mcpServers: { checkpoint: { url: endpoint, headers: { Authorization: `Bearer ${key}` } } } },
    null,
    2,
  );
}

export function formatDateTime(iso: string | undefined): string {
  if (!iso) return 'Never';
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}
