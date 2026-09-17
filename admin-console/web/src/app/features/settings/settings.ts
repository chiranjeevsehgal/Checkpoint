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
    <div class="page-actions">
      <button class="btn btn-ghost" type="button" [disabled]="saving()" (click)="load()">
        Reload
      </button>
      <button
        class="btn btn-primary"
        type="button"
        [disabled]="saving() || !dirty()"
        (click)="save()"
      >
        {{ saving() ? 'Saving…' : 'Save' }}
      </button>
    </div>

    @if (message()) {
      <p class="success">{{ message() }}</p>
    }
    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    @if (loading()) {
      <p class="state">Loading settings…</p>
    } @else {
      <div class="card">
        <h2>Connection</h2>
        <div class="field">
          <label for="ADMIN_DATABASE_URL">
            Database URL
            @if (sources()['ADMIN_DATABASE_URL']; as source) {
              <span class="chip">{{ source }}</span>
            }
          </label>
          <div class="control-row">
            <input
              id="ADMIN_DATABASE_URL"
              [value]="databaseUrlDraft()"
              (input)="databaseUrlDraft.set(inputValue($event))"
              [placeholder]="databaseUrlPlaceholder()"
            />
            <button class="btn btn-ghost" type="button" (click)="test('database')">Test</button>
          </div>
          @let databaseTest = tests()['database'];
          @if (databaseTest) {
            <p class="subtle" [class.success]="databaseTest.ok" [class.error]="!databaseTest.ok">
              {{ databaseTest.ok ? 'ok' : 'failed' }} — {{ databaseTest.detail }}
            </p>
          }
        </div>

        <div class="field-grid">
          @for (field of connectionFields; track field.key) {
            <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field }" />
          }
        </div>
      </div>

      <div class="card">
        <h2>Firmware</h2>
        <div class="field-grid">
          @for (field of firmwareFields; track field.key) {
            <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field }" />
          }
        </div>
        @if (candidates().length) {
          <hr />
          <p class="subtle">Detected arduino-cli installations</p>
          <div class="table-scroll">
            <table>
              <tbody>
                @for (candidate of candidates(); track candidate.path) {
                  <tr>
                    <td class="mono">{{ candidate.path }}</td>
                    <td class="subtle">{{ candidate.version || 'unknown version' }}</td>
                    <td class="actions">
                      <button class="btn btn-ghost" type="button" (click)="setValue('ADMIN_ARDUINO_CLI', candidate.path)">
                        Use
                      </button>
                    </td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
      </div>

      <div class="card">
        <h2>Device admin CLI</h2>
        <div class="field-grid">
          <div class="field">
            <label for="ADMIN_DEVICE_ADMIN_MODE">
              Execution mode
              @if (sources()['ADMIN_DEVICE_ADMIN_MODE']; as source) {
                <span class="chip">{{ source }}</span>
              }
            </label>
            <select id="ADMIN_DEVICE_ADMIN_MODE" [value]="mode()" (change)="setValue('ADMIN_DEVICE_ADMIN_MODE', $event)">
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
          }
        </div>
        @if (mode() === 'ssh') {
          <p class="subtle">The remote host runs ./device-admin with its own DATABASE_URL.</p>
        }
      </div>

      <div class="card">
        <h2>Serial</h2>
        <div class="field-grid">
          <ng-container [ngTemplateOutlet]="fieldRow" [ngTemplateOutletContext]="{ field: serialField }" />
        </div>
      </div>

      <div class="card">
        <h2>Agent</h2>
        <p class="subtle">Bind address and port come from the environment and need an agent restart.</p>
        <dl class="defs">
          <dt>Address</dt>
          <dd>{{ host() }}:{{ port() }}</dd>
        </dl>
      </div>
    }

    <ng-template #fieldRow let-field="field">
      <div class="field">
        <label [attr.for]="field.key">
          {{ field.label }}
          @if (sources()[field.key]; as source) {
            <span class="chip">{{ source }}</span>
          }
        </label>
        <div class="control-row">
          <input [id]="field.key" [value]="value(field.key)" (input)="setValue(field.key, $event)" />
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
            <p class="subtle" [class.success]="result.ok" [class.error]="!result.ok">
              {{ result.ok ? 'ok' : 'failed' }} — {{ result.detail }}
            </p>
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
          <div class="list-box">
            @for (entry of state.result.entries; track entry.path) {
              <button class="btn" type="button" (click)="browseTo(entry.path)">{{ entry.name }}</button>
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
  protected readonly loading = signal(true);
  protected readonly message = signal('');
  protected readonly error = signal('');

  private readonly baseline = signal('');

  protected readonly mode = computed(() => this.values()['ADMIN_DEVICE_ADMIN_MODE'] || 'go');

  protected readonly dirty = computed(() => JSON.stringify(this.payload()) !== this.baseline());

  private readonly payload = computed<Record<string, string>>(() => {
    const values = this.values();
    const payload: Record<string, string> = {};
    for (const key of EDITABLE_KEYS) payload[key] = values[key] ?? '';
    const draft = this.databaseUrlDraft().trim();
    if (draft) payload['ADMIN_DATABASE_URL'] = draft;
    return payload;
  });

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
    this.message.set('');
  }

  protected async save(): Promise<void> {
    this.saving.set(true);
    this.message.set('');
    this.error.set('');
    try {
      this.databaseUrlDraft.set('');
      this.apply(await this.api.saveSettings(this.payload()));
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

  protected async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.apply(await this.api.settings());
      this.message.set('');
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    } finally {
      this.loading.set(false);
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
    this.databaseUrlDraft.set('');
    this.baseline.set(JSON.stringify(this.payload()));
  }
}

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
