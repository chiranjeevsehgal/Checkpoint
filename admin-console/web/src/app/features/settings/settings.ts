import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import type { ArduinoCandidate, BrowseResult, SettingsView, TestResult } from '../../core/models';

interface FieldDef {
  key: string;
  label: string;
  test?: string;
  browse?: boolean;
  detect?: boolean;
}

const EDITABLE_KEYS = [
  'ADMIN_KRATOS_ADMIN_URL',
  'ADMIN_KRATOS_PUBLIC_URL',
  'ADMIN_INGESTION_URL',
  'ADMIN_DOCKER_HOST',
  'ADMIN_ARDUINO_CLI',
  'ADMIN_SKETCH_DIR',
  'ADMIN_BUILD_DIR',
  'ADMIN_FQBN',
  'ADMIN_DEVICE_ADMIN_MODE',
  'ADMIN_DEVICE_ADMIN_BINARY',
  'ADMIN_DEVICE_ADMIN_SSH_HOST',
  'ADMIN_DEVICE_ADMIN_SSH_DIR',
  'ADMIN_DEFAULT_SERIAL_PORT',
];

@Component({
  selector: 'ck-settings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NgTemplateOutlet],
  template: `
    <div class="page-header">
      <h1 class="page-title">Settings</h1>
      <button class="btn btn-primary" type="button" [disabled]="saving()" (click)="save()">Save</button>
    </div>

    @if (message()) {
      <p class="success">{{ message() }}</p>
    }
    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      <h2>Connection</h2>
      <div class="field">
        <label>Database URL <span class="subtle">{{ sources()['ADMIN_DATABASE_URL'] }}</span></label>
        <div class="row">
          <input
            [value]="databaseUrlDraft()"
            (input)="databaseUrlDraft.set(inputValue($event))"
            [placeholder]="databaseUrlPlaceholder()"
          />
          <button class="btn btn-ghost" type="button" (click)="test('database')">Test</button>
        </div>
        @let databaseTest = tests()['database'];
        @if (databaseTest) {
          <span [class]="databaseTest.ok ? 'subtle success' : 'subtle error'">
            {{ databaseTest.ok ? 'ok' : 'failed' }} — {{ databaseTest.detail }}
          </span>
        }
      </div>

      @for (field of connectionFields; track field.key) {
        <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field }" />
      }
    </div>

    <div class="card">
      <h2>Firmware</h2>
      @for (field of firmwareFields; track field.key) {
        <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field }" />
      }
      @if (candidates().length) {
        <hr />
        <p class="subtle">Detected arduino-cli installations</p>
        <table>
          <tbody>
            @for (candidate of candidates(); track candidate.path) {
              <tr>
                <td class="mono">{{ candidate.path }}</td>
                <td class="subtle">{{ candidate.version || 'unknown version' }}</td>
                <td>
                  <button class="btn btn-ghost" type="button" (click)="setValue('ADMIN_ARDUINO_CLI', candidate.path)">
                    Use
                  </button>
                </td>
              </tr>
            }
          </tbody>
        </table>
      }
    </div>

    <div class="card">
      <h2>Device admin CLI</h2>
      <div class="field">
        <label>Execution mode <span class="subtle">{{ sources()['ADMIN_DEVICE_ADMIN_MODE'] }}</span></label>
        <select [value]="mode()" (change)="setValue('ADMIN_DEVICE_ADMIN_MODE', $event)">
          <option value="go">go run (local repo + Go)</option>
          <option value="binary">Prebuilt binary</option>
          <option value="ssh">SSH to remote host</option>
        </select>
      </div>
      @if (mode() === 'binary') {
        <ng-container
          [ngTemplateOutlet]="fieldRow"
          [ngTemplateOutletContext]="{ field: binaryField }"
        />
      }
      @if (mode() === 'ssh') {
        @for (field of sshFields; track field.key) {
          <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field }" />
        }
        <p class="subtle">The remote host runs ./device-admin with its own DATABASE_URL.</p>
      }
    </div>

    <div class="card">
      <h2>Serial</h2>
      <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field: serialField }" />
    </div>

    <div class="card">
      <h2>Agent</h2>
      <p class="subtle">Bind address and port come from the environment and need an agent restart.</p>
      <p class="mono">{{ host() }}:{{ port() }}</p>
    </div>

    <ng-template #fieldRow let-field="field">
      <div class="field">
        <label>{{ field.label }} <span class="subtle">{{ sources()[field.key] }}</span></label>
        <div class="row">
          <input [value]="value(field.key)" (input)="setValue(field.key, $event)" />
          @if (field.browse) {
            <button class="btn btn-ghost" type="button" (click)="openBrowser(field.key)">Browse…</button>
          }
          @if (field.detect) {
            <button class="btn btn-ghost" type="button" (click)="detect()">Detect</button>
          }
          @if (field.test) {
            <button class="btn btn-ghost" type="button" (click)="test(field.test, field.key)">Test</button>
          }
        </div>
        @if (field.test) {
          @let result = tests()[field.test];
          @if (result) {
            <span [class]="result.ok ? 'subtle success' : 'subtle error'">
              {{ result.ok ? 'ok' : 'failed' }} — {{ result.detail }}
            </span>
          }
        }
      </div>
    </ng-template>

    @if (browser(); as state) {
      <div class="modal-backdrop">
        <div class="card modal modal-wide">
          <h2>Select folder</h2>
          <p class="mono subtle">{{ state.result.path || '(drives)' }}</p>
          <div class="toolbar">
            @if (state.result.parent) {
              <button class="btn btn-ghost" type="button" (click)="browseTo(state.result.parent)">Up</button>
            }
          </div>
          <div class="log log-sm">
            @for (entry of state.result.entries; track entry.path) {
              <div>
                <button class="btn btn-ghost" type="button" (click)="browseTo(entry.path)">
                  {{ entry.name }}
                </button>
              </div>
            }
          </div>
          <div class="row between">
            <button class="btn btn-ghost" type="button" (click)="browser.set(null)">Cancel</button>
            <button class="btn btn-primary" type="button" [disabled]="!state.result.path" (click)="chooseFolder()">
              Use this folder
            </button>
          </div>
        </div>
      </div>
    }
  `,
})
export class Settings {
  private readonly api = inject(ApiService);

  protected readonly values = signal<Record<string, string>>({});
  protected readonly sources = signal<Record<string, string>>({});
  protected readonly tests = signal<Record<string, TestResult>>({});
  protected readonly candidates = signal<ArduinoCandidate[]>([]);
  protected readonly browser = signal<{ key: string; result: BrowseResult } | null>(null);
  protected readonly databaseUrlPlaceholder = signal('');
  protected readonly databaseUrlDraft = signal('');
  protected readonly host = signal('');
  protected readonly port = signal(0);
  protected readonly saving = signal(false);
  protected readonly message = signal('');
  protected readonly error = signal('');

  protected readonly mode = computed(() => this.values()['ADMIN_DEVICE_ADMIN_MODE'] || 'go');

  protected readonly connectionFields: FieldDef[] = [
    { key: 'ADMIN_KRATOS_ADMIN_URL', label: 'Kratos admin URL', test: 'kratos' },
    { key: 'ADMIN_KRATOS_PUBLIC_URL', label: 'Kratos public URL' },
    { key: 'ADMIN_INGESTION_URL', label: 'Ingestion API URL', test: 'ingestion' },
    { key: 'ADMIN_DOCKER_HOST', label: 'Docker host (infra view)', test: 'docker' },
  ];

  protected readonly firmwareFields: FieldDef[] = [
    { key: 'ADMIN_ARDUINO_CLI', label: 'arduino-cli path', detect: true },
    { key: 'ADMIN_SKETCH_DIR', label: 'Sketch directory', browse: true },
    { key: 'ADMIN_BUILD_DIR', label: 'Build directory', browse: true },
    { key: 'ADMIN_FQBN', label: 'FQBN' },
  ];

  protected readonly binaryField: FieldDef = { key: 'ADMIN_DEVICE_ADMIN_BINARY', label: 'Binary path' };
  protected readonly sshFields: FieldDef[] = [
    { key: 'ADMIN_DEVICE_ADMIN_SSH_HOST', label: 'SSH host (user@vps)' },
    { key: 'ADMIN_DEVICE_ADMIN_SSH_DIR', label: 'Remote directory' },
  ];
  protected readonly serialField: FieldDef = {
    key: 'ADMIN_DEFAULT_SERIAL_PORT',
    label: 'Default serial port',
  };

  constructor() {
    void this.load();
  }

  protected value(key: string): string {
    return this.values()[key] ?? '';
  }

  protected inputValue(event: Event): string {
    return (event.target as HTMLInputElement).value;
  }

  protected setValue(key: string, source: Event | string): void {
    const value = typeof source === 'string' ? source : (source.target as HTMLInputElement).value;
    this.values.update((current) => ({ ...current, [key]: value }));
  }

  protected async save(): Promise<void> {
    this.saving.set(true);
    this.message.set('');
    this.error.set('');
    try {
      const payload: Record<string, string> = {};
      for (const key of EDITABLE_KEYS) payload[key] = this.value(key);
      const draft = this.databaseUrlDraft().trim();
      if (draft) payload['ADMIN_DATABASE_URL'] = draft;
      this.apply(await this.api.saveSettings(payload));
      this.databaseUrlDraft.set('');
      this.message.set('Settings saved');
    } catch (error) {
      this.error.set(asMessage(error));
    } finally {
      this.saving.set(false);
    }
  }

  protected async test(target: string, key?: string): Promise<void> {
    this.error.set('');
    try {
      const value = target === 'database' ? this.databaseUrlDraft().trim() : key ? this.value(key) : undefined;
      const result = await this.api.testTarget(target, value || undefined);
      this.tests.update((current) => ({ ...current, [target]: result }));
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async detect(): Promise<void> {
    this.error.set('');
    try {
      this.candidates.set(await this.api.detectArduino());
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async openBrowser(key: string): Promise<void> {
    this.error.set('');
    try {
      const result = await this.api.browse(this.value(key));
      this.browser.set({ key, result });
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async browseTo(path: string): Promise<void> {
    try {
      const current = this.browser();
      if (current) this.browser.set({ key: current.key, result: await this.api.browse(path) });
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected chooseFolder(): void {
    const state = this.browser();
    if (state?.result.path) this.setValue(state.key, state.result.path);
    this.browser.set(null);
  }

  private async load(): Promise<void> {
    try {
      this.apply(await this.api.settings());
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  private apply(view: SettingsView): void {
    const values = { ...view.values };
    this.databaseUrlPlaceholder.set(values['ADMIN_DATABASE_URL'] ?? '');
    delete values['ADMIN_DATABASE_URL'];
    for (const key of EDITABLE_KEYS) values[key] = values[key] ?? '';
    values['ADMIN_DEVICE_ADMIN_MODE'] = values['ADMIN_DEVICE_ADMIN_MODE'] || 'go';
    this.values.set(values);
    this.sources.set(view.sources);
    this.host.set(view.host);
    this.port.set(view.port);
  }
}

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
