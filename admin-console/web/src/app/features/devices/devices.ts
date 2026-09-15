import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { ApiService } from '../../core/api';
import { formatTimestamp, shortId } from '../../core/format';
import type { DeviceRecord } from '../../core/models';
import { ClaimQr } from '../../shared/claim-qr';
import { ConfirmDestructive } from '../../shared/confirm-destructive';

const DEVICE_ID = /^[0-9a-f]{32}$/;
const CLAIM_KEY = /^[0-9a-f]{64}$/;

@Component({
  selector: 'ck-devices',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ClaimQr, ConfirmDestructive],
  template: `
    <div class="page-actions">
      <button class="btn btn-ghost" type="button" (click)="load()">Refresh</button>
    </div>

    @if (message()) {
      <p class="success">{{ message() }}</p>
    }
    @if (error()) {
      <p class="error">{{ error() }}</p>
    }

    <div class="grid">
      <div class="card">
        <h2>Provision pendant</h2>
        <p class="subtle">Read IDs from the USB console, then provision the cloud claim hash.</p>
        <div class="field">
          <label for="provision-device-id">Device id</label>
          <input
            id="provision-device-id"
            [value]="deviceId()"
            (input)="deviceId.set(inputValue($event))"
          />
        </div>
        <div class="field">
          <label for="provision-claim-hash">Claim hash (cloud-sha256)</label>
          <input
            id="provision-claim-hash"
            [value]="claimHash()"
            (input)="claimHash.set(inputValue($event))"
          />
        </div>
        <div class="toolbar">
          <button class="btn btn-ghost" type="button" (click)="readIds()">Read IDs</button>
          <button class="btn btn-primary" type="button" (click)="provision()">Provision</button>
          <button class="btn" type="button" (click)="runStatus(deviceId())">Status</button>
        </div>
        @if (output()) {
          <pre class="log log-inline">{{ output() }}</pre>
        }
      </div>

      <div class="card">
        <h2>Enrollment QR</h2>
        <p class="subtle">
          Paste the BLE claim key printed by <span class="mono">auth export</span>; the device id
          comes from the pendant card.
        </p>
        <div class="field">
          <label for="qr-claim-key">BLE claim key</label>
          <input
            id="qr-claim-key"
            [value]="claimKey()"
            (input)="claimKey.set(inputValue($event))"
          />
        </div>
        @if (claimUri()) {
          <ck-claim-qr [value]="claimUri()" />
          <p class="mono">{{ claimUri() }}</p>
        } @else {
          <p class="state">Enter a 32-hex device id and 64-hex claim key.</p>
        }
      </div>
    </div>

    <div class="card">
      <h2>All devices</h2>
      @if (loading()) {
        <p class="state">Loading devices…</p>
      } @else if (devices().length) {
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Device</th>
                <th>State</th>
                <th>Owner</th>
                <th>Claimed</th>
                <th>Updated</th>
                <th><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              @for (device of devices(); track device.device_id) {
                <tr>
                  <td class="mono">{{ device.device_id }}</td>
                  <td>
                    <span
                      class="badge"
                      [class.badge-success]="device.state === 'owned'"
                      [class.badge-warning]="device.state === 'reset_required'"
                      [class.badge-muted]="device.state === 'unowned'"
                    >
                      {{ device.state }}
                    </span>
                  </td>
                  <td class="mono">{{ device.user_id ? short(device.user_id) : '—' }}</td>
                  <td>{{ format(device.claimed_at) }}</td>
                  <td>{{ format(device.updated_at) }}</td>
                  <td class="actions">
                    <div class="toolbar">
                      <button class="btn btn-ghost" type="button" (click)="runStatus(device.device_id)">
                        Status
                      </button>
                      @if (device.state === 'reset_required') {
                        <button
                          class="btn"
                          type="button"
                          (click)="pendingUnquarantine.set(device.device_id)"
                        >
                          Unquarantine
                        </button>
                      }
                    </div>
                  </td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      } @else {
        <p class="state">No devices.</p>
      }
    </div>

    @if (pendingUnquarantine(); as device) {
      <ck-confirm
        title="Unquarantine pendant"
        [expected]="device"
        confirmLabel="Unquarantine"
        [message]="
          'This returns ' +
          device +
          ' to the unowned pool, making it claimable by whoever holds it and its claim key.'
        "
        (confirmed)="unquarantine()"
        (cancelled)="pendingUnquarantine.set(null)"
      />
    }
  `,
})
export class Devices {
  private readonly api = inject(ApiService);

  protected readonly devices = signal<DeviceRecord[]>([]);
  protected readonly deviceId = signal('');
  protected readonly claimHash = signal('');
  protected readonly claimKey = signal('');
  protected readonly output = signal('');
  protected readonly error = signal('');
  protected readonly message = signal('');
  protected readonly loading = signal(true);
  protected readonly pendingUnquarantine = signal<string | null>(null);

  protected readonly format = formatTimestamp;
  protected readonly short = shortId;

  protected readonly claimUri = computed(() => {
    const device = this.deviceId().trim().toLowerCase();
    const key = this.claimKey().trim().toLowerCase();
    if (!DEVICE_ID.test(device) || !CLAIM_KEY.test(key)) return '';
    return `checkpoint://claim?device=${device}&key=${key}`;
  });

  constructor() {
    void this.load();
  }

  protected inputValue(event: Event): string {
    return (event.target as HTMLInputElement).value;
  }

  protected async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.devices.set(await this.api.devices());
      this.error.set('');
    } catch (error) {
      this.error.set(message(error));
    } finally {
      this.loading.set(false);
    }
  }

  protected async readIds(): Promise<void> {
    await this.guard(async () => {
      const identity = await this.api.readIds();
      this.deviceId.set(identity.deviceId);
      this.claimHash.set(identity.claimHash);
      this.message.set(`Read device ${identity.deviceId}`);
    });
  }

  protected async provision(): Promise<void> {
    await this.guard(async () => {
      this.output.set(await this.api.provision(this.deviceId().trim(), this.claimHash().trim()));
      this.message.set('Provisioned');
      await this.load();
    });
  }

  protected async runStatus(deviceId: string): Promise<void> {
    await this.guard(async () => {
      this.output.set(await this.api.deviceStatus(deviceId.trim()));
      this.message.set('');
    });
  }

  protected async unquarantine(): Promise<void> {
    const device = this.pendingUnquarantine();
    if (!device) return;
    this.pendingUnquarantine.set(null);
    await this.guard(async () => {
      this.output.set(await this.api.unquarantine(device));
      this.message.set(`Unquarantined ${device}`);
      await this.load();
    });
  }

  private async guard(action: () => Promise<void>): Promise<void> {
    this.error.set('');
    this.message.set('');
    try {
      await action();
    } catch (error) {
      this.error.set(message(error));
    }
  }
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : 'Request failed';
}
