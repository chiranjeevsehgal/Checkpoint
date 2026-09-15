import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { formatTimestamp, shortId } from '../../core/format';
import type { DeletionRecord } from '../../core/models';

@Component({
  selector: 'ck-deletions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="page-header">
      <h1 class="page-title">Account deletions</h1>
      <button class="btn btn-ghost" type="button" (click)="load()">Refresh</button>
    </div>

    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      @if (deletions().length) {
        <table>
          <thead>
            <tr>
              <th>User</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Next attempt</th>
              <th>Completed</th>
              <th>Last error</th>
            </tr>
          </thead>
          <tbody>
            @for (item of deletions(); track item.user_id) {
              <tr>
                <td class="mono">{{ short(item.user_id) }}</td>
                <td>
                  <span
                    class="badge"
                    [class.badge-success]="item.status === 'COMPLETE'"
                    [class.badge-warning]="item.status === 'PENDING'"
                    [class.badge-primary]="item.status === 'PROCESSING'"
                  >
                    {{ item.status }}
                  </span>
                </td>
                <td>{{ item.attempt_count }}</td>
                <td>{{ format(item.next_attempt_at) }}</td>
                <td>{{ format(item.completed_at) }}</td>
                <td class="subtle">{{ item.last_error || '—' }}</td>
              </tr>
            }
          </tbody>
        </table>
      } @else {
        <p class="subtle">No deletions queued.</p>
      }
    </div>
  `,
})
export class Deletions {
  private readonly api = inject(ApiService);

  protected readonly deletions = signal<DeletionRecord[]>([]);
  protected readonly error = signal('');

  protected readonly format = formatTimestamp;
  protected readonly short = shortId;

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    try {
      this.deletions.set(await this.api.deletions());
      this.error.set('');
    } catch (error) {
      this.error.set(error instanceof Error ? error.message : 'Request failed');
    }
  }
}
