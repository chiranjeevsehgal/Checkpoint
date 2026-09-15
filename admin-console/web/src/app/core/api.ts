import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom, type Observable } from 'rxjs';

import type {
  ArduinoCandidate,
  BrowseResult,
  ConsoleConfig,
  DeletionRecord,
  DeviceRecord,
  Identity,
  InfraView,
  ProvisionIdentity,
  Session,
  SettingsView,
  TestResult,
} from './models';

interface OutputResponse {
  output: string;
}

@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);
  private readonly base = '/api';

  health(): Promise<{ status: string }> {
    return this.request(this.http.get<{ status: string }>(`${this.base}/health`));
  }

  config(): Promise<ConsoleConfig> {
    return this.request(this.http.get<ConsoleConfig>(`${this.base}/config`));
  }

  infra(): Promise<InfraView> {
    return this.request(this.http.get<InfraView>(`${this.base}/infra`));
  }

  serialPorts(): Promise<string[]> {
    return this.request(
      this.http.get<{ ports: string[] }>(`${this.base}/serial/ports`),
    ).then((response) => response.ports);
  }

  serialState(): Promise<boolean> {
    return this.request(
      this.http.get<{ open: boolean }>(`${this.base}/serial`),
    ).then((response) => response.open);
  }

  openSerial(path: string): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/serial/open`, { path }));
  }

  closeSerial(): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/serial/close`, {}));
  }

  sendSerial(command: string): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/serial/send`, { command }));
  }

  compile(): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/firmware/compile`, {}));
  }

  upload(port: string): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/firmware/upload`, { port }));
  }

  compileUpload(port: string): Promise<void> {
    return this.request(this.http.post<void>(`${this.base}/firmware/compile-upload`, { port }));
  }

  devices(): Promise<DeviceRecord[]> {
    return this.request(
      this.http.get<{ devices: DeviceRecord[] }>(`${this.base}/devices`),
    ).then((response) => response.devices);
  }

  deletions(): Promise<DeletionRecord[]> {
    return this.request(
      this.http.get<{ deletions: DeletionRecord[] }>(`${this.base}/deletions`),
    ).then((response) => response.deletions);
  }

  readIds(): Promise<ProvisionIdentity> {
    return this.request(this.http.post<ProvisionIdentity>(`${this.base}/devices/read-ids`, {}));
  }

  provision(deviceId: string, claimHash: string): Promise<string> {
    return this.request(
      this.http.post<OutputResponse>(`${this.base}/devices/provision`, { deviceId, claimHash }),
    ).then((response) => response.output);
  }

  deviceStatus(deviceId: string): Promise<string> {
    return this.request(
      this.http.post<OutputResponse>(`${this.base}/devices/status`, { deviceId }),
    ).then((response) => response.output);
  }

  unquarantine(deviceId: string): Promise<string> {
    return this.request(
      this.http.post<OutputResponse>(`${this.base}/devices/unquarantine`, { deviceId }),
    ).then((response) => response.output);
  }

  users(): Promise<Identity[]> {
    return this.request(
      this.http.get<{ users: Identity[] }>(`${this.base}/users`),
    ).then((response) => response.users);
  }

  sessions(identityId: string): Promise<Session[]> {
    return this.request(
      this.http.get<{ sessions: Session[] }>(`${this.base}/users/${identityId}/sessions`),
    ).then((response) => response.sessions);
  }

  revokeSessions(identityId: string): Promise<string[]> {
    return this.request(
      this.http.post<{ revoked: string[] }>(
        `${this.base}/users/${identityId}/sessions/revoke`,
        {},
      ),
    ).then((response) => response.revoked);
  }

  deleteAccount(identityId: string): Promise<string> {
    return this.request(
      this.http.post<OutputResponse>(`${this.base}/users/${identityId}/delete`, {}),
    ).then((response) => response.output);
  }

  settings(): Promise<SettingsView> {
    return this.request(this.http.get<SettingsView>(`${this.base}/settings`));
  }

  saveSettings(values: Record<string, string>): Promise<SettingsView> {
    return this.request(this.http.put<SettingsView>(`${this.base}/settings`, { values }));
  }

  browse(path: string): Promise<BrowseResult> {
    return this.request(
      this.http.get<BrowseResult>(`${this.base}/settings/browse`, { params: { path } }),
    );
  }

  detectArduino(): Promise<ArduinoCandidate[]> {
    return this.request(
      this.http.post<{ candidates: ArduinoCandidate[] }>(`${this.base}/settings/detect-arduino`, {}),
    ).then((response) => response.candidates);
  }

  testTarget(target: string, value?: string): Promise<TestResult> {
    return this.request(this.http.post<TestResult>(`${this.base}/settings/test`, { target, value }));
  }

  private async request<T>(source: Observable<T>): Promise<T> {
    try {
      return await firstValueFrom(source);
    } catch (error) {
      throw new Error(describeHttpError(error));
    }
  }
}

export function describeHttpError(error: unknown): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { error?: string } | null;
    if (body && typeof body.error === 'string') return body.error;
    if (error.status === 0) return 'Agent unreachable (is the server running?)';
    return error.statusText || `Request failed (${error.status})`;
  }
  return error instanceof Error ? error.message : 'Unexpected error';
}
