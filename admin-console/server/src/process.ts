import { spawn } from 'node:child_process';

export interface CommandOutput {
  code: number;
  stdout: string;
  stderr: string;
}

export interface CaptureOptions {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
}

export function runCapture(command: string[], options: CaptureOptions = {}): Promise<CommandOutput> {
  return new Promise((resolve, reject) => {
    const child = spawn(command[0], command.slice(1), {
      cwd: options.cwd,
      env: options.env ?? process.env,
      windowsHide: true,
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString();
    });
    child.stderr.on('data', (chunk: Buffer) => {
      stderr += chunk.toString();
    });
    child.on('error', reject);
    child.on('close', (code) => resolve({ code: code ?? -1, stdout, stderr }));
  });
}

export function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
