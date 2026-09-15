import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';

import { ApiService } from '../../core/api';
import type { ComposeService, ConsoleConfig, InfraView } from '../../core/models';

@Component({
  selector: 'ck-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    <div class="page-header">
      <h1 class="page-title">Dashboard</h1>
      <button class="btn btn-ghost" type="button" (click)="load()">Refresh</button>
    </div>

    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="grid">
      <div class="card">
        <div class="row between">
          <span class="subtle">Ingestion API</span>
          <span class="badge" [class.badge-success]="health().ingestion" [class.badge-danger]="!health().ingestion">
            {{ health().ingestion ? 'up' : 'down' }}
          </span>
        </div>
        <p class="mono">{{ config()?.ingestionUrl }}</p>
      </div>
      <div class="card">
        <div class="row between">
          <span class="subtle">Kratos</span>
          <span class="badge" [class.badge-success]="health().kratos" [class.badge-danger]="!health().kratos">
            {{ health().kratos ? 'up' : 'down' }}
          </span>
        </div>
        <p class="mono">{{ config()?.kratosPublicUrl }}</p>
      </div>
    </div>

    <div class="card">
      <div class="row between">
        <h2>Compose services</h2>
        @if (servicesError()) {
          <span class="subtle">{{ servicesError() }}</span>
        }
      </div>
      @if (services().length) {
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Service</th>
              <th>State</th>
              <th>Health</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            @for (service of services(); track service.Name) {
              <tr>
                <td class="mono">{{ service.Name || '—' }}</td>
                <td>{{ service.Service || '—' }}</td>
                <td>{{ service.State || '—' }}</td>
                <td>{{ service.Health || '—' }}</td>
                <td class="subtle">{{ service.Status || '—' }}</td>
              </tr>
            }
          </tbody>
        </table>
      } @else if (!servicesError()) {
        <p class="subtle">No services reported.</p>
      }
    </div>

    <div class="card">
      <div class="row between">
        <h2>Agent configuration</h2>
        <a class="btn btn-ghost" routerLink="/settings">Settings</a>
      </div>
      @if (config(); as view) {
        <table>
          <tbody>
            <tr><td class="subtle">Repository</td><td class="mono">{{ view.repoRoot }}</td></tr>
            <tr><td class="subtle">Database</td><td class="mono">{{ view.databaseUrl }}</td></tr>
            <tr><td class="subtle">Kratos admin</td><td class="mono">{{ view.kratosAdminUrl }}</td></tr>
            <tr><td class="subtle">arduino-cli</td><td class="mono">{{ view.arduinoCli }}</td></tr>
            <tr><td class="subtle">Sketch</td><td class="mono">{{ view.sketchDir }}</td></tr>
            <tr><td class="subtle">Build</td><td class="mono">{{ view.buildDir }}</td></tr>
            <tr><td class="subtle">FQBN</td><td class="mono">{{ view.fqbn }}</td></tr>
          </tbody>
        </table>
      }
    </div>
  `,
})
export class Dashboard {
  private readonly api = inject(ApiService);

  protected readonly config = signal<ConsoleConfig | null>(null);
  protected readonly infraView = signal<InfraView | null>(null);
  protected readonly error = signal('');

  protected readonly services = computed<ComposeService[]>(() => {
    const view = this.infraView();
    return view && Array.isArray(view.services) ? view.services : [];
  });

  protected readonly servicesError = computed(() => {
    const view = this.infraView();
    return view && !Array.isArray(view.services) ? view.services.error : '';
  });

  protected readonly health = computed(
    () => this.infraView()?.health ?? { ingestion: false, kratos: false },
  );

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.error.set('');
    try {
      const [config, infra] = await Promise.all([this.api.config(), this.api.infra()]);
      this.config.set(config);
      this.infraView.set(infra);
    } catch (error) {
      this.error.set(error instanceof Error ? error.message : 'Failed to load');
    }
  }
}
