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
    <div class="page-header">
      <h1 class="page-title">Devices</h1>
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
          <label>Device id</label>
          <input [value]="deviceId()" (input)="deviceId.set(inputValue($event))" />
        </div>
        <div class="field">
          <label>Claim hash (cloud-sha256)</label>
          <input [value]="claimHash()" (input)="claimHash.set(inputValue($event))" />
        </div>
        <div class="toolbar">
          <button class="btn btn-ghost" type="button" (click)="readIds()">Read IDs</button>
          <button class="btn btn-primary" type="button" (click)="provision()">Provision</button>
          <button class="btn" type="button" (click)="runStatus(deviceId())">Status</button>
        </div>
        @if (output()) {
          <pre class="log">{{ output() }}</pre>
        }
      </div>

      <div class="card">
        <h2>Enrollment QR</h2>
        <p class="subtle">Paste the BLE claim key printed by <span class="mono">auth export</span>.</p>
        <div class="field">
          <label>Device id</label>
          <input [value]="qrDeviceId()" (input)="qrDeviceId.set(inputValue($event))" />
        </div>
        <div class="field">
          <label>BLE claim key</label>
          <input [value]="claimKey()" (input)="claimKey.set(inputValue($event))" />
        </div>
        @if (claimUri()) {
          <ck-claim-qr [value]="claimUri()" />
          <pre class="log">{{ claimUri() }}</pre>
        } @else {
          <p class="subtle">Enter a 32-hex device id and 64-hex claim key.</p>
        }
      </div>
    </div>

    <div class="card">
      <h2>All devices</h2>
      @if (devices().length) {
        <table>
          <thead>
            <tr>
              <th>Device</th>
              <th>State</th>
              <th>Owner</th>
              <th>Claimed</th>
              <th>Updated</th>
              <th></th>
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
                <td>
                  <div class="toolbar">
                    <button class="btn btn-ghost" type="button" (click)="runStatus(device.device_id)">
                      Status
                    </button>
                    @if (device.state === 'reset_required') {
                      <button class="btn btn-danger" type="button" (click)="pendingUnquarantine.set(device.device_id)">
                        Unquarantine
                      </button>
                    }
                  </div>
                </td>
              </tr>
            }
          </tbody>
        </table>
      } @else {
        <p class="subtle">No devices.</p>
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
  protected readonly qrDeviceId = signal('');
  protected readonly claimKey = signal('');
  protected readonly output = signal('');
  protected readonly error = signal('');
  protected readonly message = signal('');
  protected readonly pendingUnquarantine = signal<string | null>(null);

  protected readonly format = formatTimestamp;
  protected readonly short = shortId;

  protected readonly claimUri = computed(() => {
    const device = this.qrDeviceId().trim().toLowerCase();
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
    try {
      this.devices.set(await this.api.devices());
      this.error.set('');
    } catch (error) {
      this.error.set(message(error));
    }
  }

  protected async readIds(): Promise<void> {
    await this.guard(async () => {
      const identity = await this.api.readIds();
      this.deviceId.set(identity.deviceId);
      this.claimHash.set(identity.claimHash);
      this.qrDeviceId.set(identity.deviceId);
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
