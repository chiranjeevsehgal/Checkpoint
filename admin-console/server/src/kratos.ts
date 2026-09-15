export interface KratosVerifiableAddress {
  value: string;
  verified?: boolean;
  status?: string;
}

export interface KratosIdentity {
  id: string;
  state?: string;
  created_at?: string;
  traits: { email?: string; name?: string };
  verifiable_addresses?: KratosVerifiableAddress[];
}

export interface KratosSession {
  id: string;
  active?: boolean;
  issued_at?: string;
  expires_at?: string;
}

// nextPageToken reads the page_token from a Link header entry with rel="next".
export function nextPageToken(header: string | null): string {
  if (!header) return '';
  for (const part of header.split(',')) {
    if (!part.includes('rel="next"')) continue;
    const match = /<([^>]+)>/.exec(part);
    if (!match) continue;
    return new URL(match[1]).searchParams.get('page_token') ?? '';
  }
  return '';
}

export class KratosAdminClient {
  constructor(readonly baseUrl: string) {}

  async listIdentities(): Promise<KratosIdentity[]> {
    const identities: KratosIdentity[] = [];
    let token = '';
    for (let page = 0; page < 100; page += 1) {
      const url = new URL('/admin/identities', this.baseUrl);
      url.searchParams.set('page_size', '250');
      if (token) url.searchParams.set('page_token', token);
      const response = await fetch(url, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`kratos admin returned ${response.status}`);
      identities.push(...((await response.json()) as KratosIdentity[]));
      token = nextPageToken(response.headers.get('link'));
      if (!token) break;
    }
    return identities;
  }

  async listSessions(identityId: string): Promise<KratosSession[]> {
    const response = await fetch(new URL(`/admin/identities/${identityId}/sessions`, this.baseUrl), {
      headers: { Accept: 'application/json' },
    });
    if (response.status === 404) return [];
    if (!response.ok) throw new Error(`kratos admin returned ${response.status}`);
    return (await response.json()) as KratosSession[];
  }

  async revokeSessions(identityId: string): Promise<string[]> {
    const sessions = await this.listSessions(identityId);
    const revoked: string[] = [];
    for (const session of sessions) {
      const response = await fetch(new URL(`/admin/sessions/${session.id}`, this.baseUrl), {
        method: 'DELETE',
      });
      if (!response.ok && response.status !== 404) {
        throw new Error(`kratos admin returned ${response.status}`);
      }
      revoked.push(session.id);
    }
    return revoked;
  }
}
