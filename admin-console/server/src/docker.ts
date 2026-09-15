import { runCapture } from './process';
import type { AppConfig } from './config';

export function parseComposeOutput(output: string): unknown[] {
  const trimmed = output.trim();
  if (!trimmed) return [];
  if (trimmed.startsWith('[')) return JSON.parse(trimmed) as unknown[];
  return trimmed
    .split(/\r?\n/)
    .filter((line) => line.length > 0)
    .map((line) => JSON.parse(line) as unknown);
}

export async function composeServices(config: AppConfig): Promise<unknown[]> {
  const env: NodeJS.ProcessEnv = { ...process.env };
  if (config.dockerHost) env.DOCKER_HOST = config.dockerHost;
  const result = await runCapture(['docker', 'compose', 'ps', '--format', 'json'], {
    cwd: config.repoRoot,
    env,
  });
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `docker compose ps exited ${result.code}`);
  }
  return parseComposeOutput(result.stdout);
}

export async function probe(url: string, timeoutMs = 3000): Promise<boolean> {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(timeoutMs) });
    return response.ok;
  } catch {
    return false;
  }
}
