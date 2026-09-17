import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { EventsService } from '../../core/events';
import type { SerialEvent } from '../../core/models';
import { pickDefaultPort } from '../../core/serial';

@Component({
  selector: 'ck-serial',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="page-actions">
      <button class="btn btn-ghost" type="button" (click)="events.clearSerial()">Clear</button>
    </div>

    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      <h2>Connection</h2>
      <div class="field">
        <label for="serial-port">Port</label>
        <div class="control-row">
          <select id="serial-port" [value]="port()" (change)="port.set(selectValue($event))">
            <option value="">—</option>
            @for (item of ports(); track item) {
              <option [value]="item">{{ item }}</option>
            }
          </select>
          <button class="btn btn-ghost" type="button" (click)="refresh()">Refresh</button>
          <button class="btn" type="button" [class.btn-primary]="!open()" (click)="toggle()">
            {{ open() ? 'Close' : 'Open' }}
          </button>
          <span class="pill" [class.badge-success]="open()">
            {{ open() ? 'open @115200' : 'closed' }}
          </span>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>Commands</h2>
      <div class="toolbar">
        <div class="cluster">
          <span class="cluster-label">auth</span>
          @for (quick of quickCommands; track quick.command) {
            <button class="btn btn-ghost" type="button" [disabled]="!open()" (click)="send(quick.command)">
              {{ quick.label }}
            </button>
          }
        </div>
        <div class="cluster">
          <label class="cluster-label" for="serial-slot">Slot</label>
          <select id="serial-slot" [value]="slot()" (change)="slot.set(selectValue($event))">
            <option value="0">0</option>
            <option value="1">1</option>
          </select>
          <button class="btn btn-ghost" type="button" [disabled]="!open()" (click)="send('auth forget ' + slot())">
            Forget
          </button>
        </div>
        <div class="cluster">
          <span class="cluster-label">Power</span>
          <button class="btn" type="button" [disabled]="!open()" (click)="send('power sleep')">
            Sleep
          </button>
        </div>
      </div>

      <hr />

      <div class="field">
        <label for="serial-command">Command</label>
        <div class="control-row">
          <input
            id="serial-command"
            [value]="command()"
            (input)="command.set(inputValue($event))"
            (keyup.enter)="sendFree()"
            placeholder="auth reset"
          />
          <button class="btn" type="button" [disabled]="!open()" (click)="sendFree()">Send</button>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>Console</h2>
      <div class="log">
        @for (entry of log(); track $index) {
          <div
            class="log-line"
            [class.command]="entry.type === 'sent'"
            [class.error]="entry.type === 'error'"
          >
            {{ describe(entry) }}
          </div>
        }
        @if (!log().length) {
          <span class="subtle">Nothing yet. Open the port to start reading output.</span>
        }
      </div>
    </div>
  `,
})
export class Serial implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);
  protected readonly events = inject(EventsService);

  protected readonly quickCommands = [
    { command: 'auth list', label: 'List' },
    { command: 'auth export', label: 'Export' },
    { command: 'auth provision', label: 'Provision' },
    { command: 'auth reset', label: 'Reset' },
  ];

  protected readonly ports = signal<string[]>([]);
  protected readonly port = signal('');
  protected readonly slot = signal('0');
  protected readonly command = signal('');
  protected readonly open = signal(false);
  protected readonly error = signal('');

  protected readonly log = computed(() => this.events.serialEvents());

  ngOnInit(): void {
    this.events.connectSerial();
    void this.refresh();
  }

  ngOnDestroy(): void {
    this.events.disconnectSerial();
  }

  protected selectValue(event: Event): string {
    return (event.target as HTMLSelectElement).value;
  }

  protected inputValue(event: Event): string {
    return (event.target as HTMLInputElement).value;
  }

  protected describe(entry: SerialEvent): string {
    switch (entry.type) {
      case 'sent':
        return `> ${entry.line}`;
      case 'line':
        return entry.line ?? '';
      case 'error':
        return `! ${entry.message}`;
      default:
        return `[serial] ${entry.open ? `opened ${entry.path ?? ''}` : 'closed'}`;
    }
  }

  protected async refresh(): Promise<void> {
    try {
      const [ports, open, config] = await Promise.all([
        this.api.serialPorts(),
        this.api.serialState(),
        this.api.config(),
      ]);
      this.ports.set(ports);
      this.open.set(open);
      if (!this.port()) this.port.set(pickDefaultPort(config.defaultSerialPort, ports));
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async toggle(): Promise<void> {
    try {
      if (this.open()) {
        await this.api.closeSerial();
        this.open.set(false);
      } else {
        await this.api.openSerial(this.port());
        this.open.set(true);
      }
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected send(command: string): void {
    void this.dispatch(command);
  }

  protected sendFree(): void {
    const command = this.command().trim();
    if (!command) return;
    this.command.set('');
    void this.dispatch(command);
  }

  private async dispatch(command: string): Promise<void> {
    try {
      await this.api.sendSerial(command);
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }
}

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
