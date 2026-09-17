import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { EventsService, isTaskBusy } from '../../core/events';
import type { ConsoleConfig, TaskEvent } from '../../core/models';
import { pickDefaultPort } from '../../core/serial';

@Component({
  selector: 'ck-firmware',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="page-actions">
      <button class="btn btn-ghost" type="button" (click)="events.clearTasks()">Clear log</button>
    </div>

    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      <h2>Build &amp; flash</h2>

      <div class="field">
        <label for="firmware-port">Serial port (required for upload)</label>
        <div class="control-row">
          <select id="firmware-port" [value]="port()" (change)="port.set(selectValue($event))">
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
          Upload build
        </button>
        <button class="btn btn-primary" type="button" [disabled]="busy() || !port()" (click)="compileUpload()">
          Compile &amp; upload
        </button>
      </div>

      @if (config(); as view) {
        <hr />
        <dl class="defs">
          <dt>arduino-cli</dt>
          <dd>{{ view.arduinoCli }}</dd>
          <dt>Sketch</dt>
          <dd>{{ view.sketchDir }}</dd>
          <dt>Build</dt>
          <dd>{{ view.buildDir }}</dd>
        </dl>
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
        @if (!log().length) {
          <span class="subtle">Nothing yet. Compile or upload to stream progress here.</span>
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
