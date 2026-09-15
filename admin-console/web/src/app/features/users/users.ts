import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { emailStatus, formatTimestamp, shortId } from '../../core/format';
import type { Identity } from '../../core/models';
import { ConfirmDestructive } from '../../shared/confirm-destructive';

@Component({
  selector: 'ck-users',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ConfirmDestructive],
  template: `
    <div class="page-header">
      <h1 class="page-title">Users</h1>
      <button class="btn btn-ghost" type="button" (click)="load()">Refresh</button>
    </div>

    @if (message()) {
      <p class="success">{{ message() }}</p>
    }
    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="card">
      @if (users().length) {
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th>Identity</th>
              <th>Created</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            @for (user of users(); track user.id) {
              @let email = emailOf(user);
              <tr>
                <td>{{ user.traits.name || '—' }}</td>
                <td>
                  {{ email.email }}
                  <span
                    class="badge"
                    [class.badge-success]="email.verified"
                    [class.badge-warning]="!email.verified"
                  >
                    {{ email.verified ? 'verified' : 'unverified' }}
                  </span>
                </td>
                <td class="mono">{{ short(user.id) }}</td>
                <td>{{ format(user.created_at) }}</td>
                <td>
                  <div class="toolbar">
                    <button class="btn btn-ghost" type="button" (click)="revoke(user.id)">
                      Revoke sessions
                    </button>
                    <button class="btn btn-danger" type="button" (click)="pendingDelete.set(user)">
                      Delete account
                    </button>
                  </div>
                </td>
              </tr>
            }
          </tbody>
        </table>
      } @else {
        <p class="subtle">No identities.</p>
      }
    </div>

    @if (pendingDelete(); as user) {
      <ck-confirm
        title="Delete account"
        [expected]="emailOf(user).email"
        confirmLabel="Queue deletion"
        [message]="
          'Queues an account-deletion tombstone for ' +
          user.id +
          '. The worker purges uploads, quarantines the pendant and removes the identity.'
        "
        (confirmed)="deleteAccount()"
        (cancelled)="pendingDelete.set(null)"
      />
    }
  `,
})
export class Users {
  private readonly api = inject(ApiService);

  protected readonly users = signal<Identity[]>([]);
  protected readonly pendingDelete = signal<Identity | null>(null);
  protected readonly message = signal('');
  protected readonly error = signal('');

  protected readonly format = formatTimestamp;
  protected readonly short = shortId;
  protected readonly emailOf = emailStatus;

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    try {
      this.users.set(await this.api.users());
      this.error.set('');
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async revoke(identityId: string): Promise<void> {
    this.message.set('');
    this.error.set('');
    try {
      const revoked = await this.api.revokeSessions(identityId);
      this.message.set(`Revoked ${revoked.length} session(s) for ${shortId(identityId)}`);
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }

  protected async deleteAccount(): Promise<void> {
    const user = this.pendingDelete();
    if (!user) return;
    this.pendingDelete.set(null);
    this.message.set('');
    this.error.set('');
    try {
      this.message.set(await this.api.deleteAccount(user.id));
    } catch (error) {
      this.error.set(asMessage(error));
    }
  }
}

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
