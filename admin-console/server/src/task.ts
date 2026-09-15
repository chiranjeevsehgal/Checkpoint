import { spawn } from 'node:child_process';

import type { EventHub } from './events';

export class TaskBusyError extends Error {
  constructor(readonly current: string) {
    super(`Another task is running: ${current}`);
    this.name = 'TaskBusyError';
  }
}

export interface RunOptions {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
}

export class TaskRunner {
  private active: string | null = null;

  constructor(private readonly events: EventHub) {}

  get busy(): boolean {
    return this.active !== null;
  }

  get current(): string | null {
    return this.active;
  }

  // Fire-and-forget variant for long commands (compile/upload) whose output
  // streams over SSE. Throws TaskBusyError synchronously when already busy.
  start(name: string, commands: string[][], options: RunOptions = {}): void {
    if (this.active) throw new TaskBusyError(this.active);
    void this.runSequence(name, commands, options).catch((error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      this.events.publish('tasks', { type: 'failed', name, message });
    });
  }

  async runSequence(name: string, commands: string[][], options: RunOptions = {}): Promise<number> {
    if (this.active) throw new TaskBusyError(this.active);
    this.active = name;
    this.events.publish('tasks', { type: 'start', name });
    try {
      let code = 0;
      for (const command of commands) {
        this.events.publish('tasks', { type: 'command', name, command: command.join(' ') });
        code = await this.spawn(name, command, options);
        if (code !== 0) break;
      }
      this.events.publish('tasks', { type: 'exit', name, code });
      return code;
    } finally {
      this.active = null;
    }
  }

  run(name: string, command: string[], options: RunOptions = {}): Promise<number> {
    return this.runSequence(name, [command], options);
  }

  private spawn(name: string, command: string[], options: RunOptions): Promise<number> {
    return new Promise((resolve, reject) => {
      const child = spawn(command[0], command.slice(1), {
        cwd: options.cwd,
        env: options.env ?? process.env,
        windowsHide: true,
      });
      const forward = (chunk: Buffer): void => {
        for (const line of chunk.toString().split(/\r?\n/)) {
          if (line.length > 0) this.events.publish('tasks', { type: 'line', name, line });
        }
      };
      child.stdout.on('data', forward);
      child.stderr.on('data', forward);
      child.on('error', reject);
      child.on('close', (code) => resolve(code ?? -1));
    });
  }
}
