import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { EventsService, isTaskBusy } from '../../core/events';
import type { ConsoleConfig, TaskEvent } from '../../core/models';
import { pickDefaultPort } from '../../core/serial';

@Component({
  selector: 'ck-firmware',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="page-header">
      <h1 class="page-title">Firmware</h1>
      <button class="btn btn-ghost" type="button" (click)="events.clearTasks()">Clear log</button>
    </div>

    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      <div class="row between">
        <h2>Build &amp; flash</h2>
        <span class="pill" [class.badge-primary]="busy()">{{ busy() ? 'working' : 'idle' }}</span>
      </div>

      <div class="field">
        <label>Serial port (required for upload)</label>
        <div class="row">
          <select [value]="port()" (change)="port.set(selectValue($event))">
            <option value="">—</option>
            @for (item of ports(); track item) {
              <option [value]="item">{{ item }}</option>
            }
          </select>
          <button class="btn btn-ghost" type="button" (click)="loadPorts()">Refresh ports</button>
        </div>
      </div>

      <div class="toolbar">
        <button class="btn" type="button" [disabled]="busy()" (click)="compile()">Compile</button>
        <button class="btn" type="button" [disabled]="busy() || !port()" (click)="upload()">
          Upload build dir
        </button>
        <button class="btn btn-primary" type="button" [disabled]="busy() || !port()" (click)="compileUpload()">
          Compile + upload
        </button>
      </div>

      @if (config(); as view) {
        <p class="subtle mono">{{ view.arduinoCli }}</p>
        <p class="subtle mono">sketch {{ view.sketchDir }}</p>
        <p class="subtle mono">build {{ view.buildDir }}</p>
      }
    </div>

    <div class="card">
      <h2>Output</h2>
      <div class="log">
        @for (entry of log(); track $index) {
          <div
            class="log-line"
            [class.command]="entry.type === 'command'"
            [class.error]="entry.type === 'failed'"
          >
            {{ describe(entry) }}
          </div>
        }
      </div>
    </div>
  `,
})
export class Firmware {
  private readonly api = inject(ApiService);
  protected readonly events = inject(EventsService);

  protected readonly config = signal<ConsoleConfig | null>(null);
  protected readonly ports = signal<string[]>([]);
  protected readonly port = signal('');
  protected readonly error = signal('');

  protected readonly log = computed(() => this.events.taskEvents());
  protected readonly busy = computed(() => isTaskBusy(this.log()));

  constructor() {
    this.events.connectTasks();
    void this.init();
  }

  protected selectValue(event: Event): string {
    return (event.target as HTMLSelectElement).value;
  }

  protected describe(entry: TaskEvent): string {
    switch (entry.type) {
      case 'command':
        return `$ ${entry.command}`;
      case 'line':
        return entry.line ?? '';
      case 'exit':
        return `— ${entry.name} exited with ${entry.code}`;
      case 'failed':
        return `! ${entry.name}: ${entry.message}`;
      default:
        return `▶ ${entry.name}`;
    }
  }

  protected async loadPorts(): Promise<void> {
    try {
      this.ports.set(await this.api.serialPorts());
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected compile(): void {
    void this.start(() => this.api.compile());
  }

  protected upload(): void {
    void this.start(() => this.api.upload(this.port()));
  }

  protected compileUpload(): void {
    void this.start(() => this.api.compileUpload(this.port()));
  }

  private async init(): Promise<void> {
    try {
      const config = await this.api.config();
      this.config.set(config);
      const ports = await this.api.serialPorts();
      this.ports.set(ports);
      if (!this.port()) this.port.set(pickDefaultPort(config.defaultSerialPort, ports));
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  private async start(action: () => Promise<void>): Promise<void> {
    this.error.set('');
    try {
      await action();
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }
}

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
