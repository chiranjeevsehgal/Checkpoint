import { fetchWithTimeout } from '../../lib/http.ts';

export interface KratosFlow {
  id: string;
  ui: { action: string; method: string };
}

export interface KratosIdentity {
  id: string;
  traits?: { email?: string };
  verifiable_addresses?: { via?: string; verified?: boolean; status?: string }[];
}

export interface KratosContinueWith {
  action: string;
  ory_session_token?: string;
  flow?: { id?: string; url?: string };
}

export interface KratosAuthResult {
  session_token?: string;
  session?: { identity?: KratosIdentity; authenticated_at?: string };
  identity?: KratosIdentity;
  continue_with?: KratosContinueWith[];
}

export interface KratosWhoami {
  active?: boolean;
  identity?: KratosIdentity;
}

export class KratosError extends Error {
  status: number;
  id?: string;
  messages: string[];

  constructor(status: number, id: string | undefined, messages: string[]) {
    super(messages[0] ?? `Kratos request failed (${status})`);
    this.status = status;
    this.id = id;
    this.messages = messages;
  }
}

export interface KratosTransport {
  request<T>(method: string, path: string, body?: unknown, token?: string): Promise<T>;
}

function messagesFrom(body: unknown): string[] {
  const data = body as {
    error?: { message?: unknown };
    ui?: { messages?: { text?: unknown }[] };
  };
  const texts: string[] = [];
  if (typeof data?.error?.message === 'string') texts.push(data.error.message);
  for (const message of data?.ui?.messages ?? []) {
    if (typeof message.text === 'string') texts.push(message.text);
  }
  return texts;
}

/**
 * Resolves a request path against the transport origin. Kratos returns flow
 * actions as absolute URLs built from its own public base URL, which may differ
 * from the address the client actually reaches (e.g. localhost on a phone).
 * Absolute paths are re-pointed at the transport origin; relative paths are
 * appended.
 */
export function resolveRequestUrl(baseUrl: string, path: string): string {
  if (!/^https?:\/\//i.test(path)) return `${baseUrl}${path}`;
  const base = new URL(baseUrl);
  const target = new URL(path);
  target.protocol = base.protocol;
  target.host = base.host;
  return target.toString();
}

export function createFetchTransport(baseUrl: string): KratosTransport {
  return {
    async request<T>(method: string, path: string, body?: unknown, token?: string): Promise<T> {
      const res = await fetchWithTimeout(resolveRequestUrl(baseUrl, path), {
        method,
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
          ...(token ? { 'X-Session-Token': token } : {}),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const text = await res.text();
      let data: unknown;
      try {
        data = text ? JSON.parse(text) : undefined;
      } catch {
        data = undefined;
      }
      if (!res.ok) {
        const id = (data as { error?: { id?: unknown } } | undefined)?.error?.id;
        throw new KratosError(
          res.status,
          typeof id === 'string' ? id : undefined,
          messagesFrom(data),
        );
      }
      return data as T;
    },
  };
}

function extractEmail(identity: KratosIdentity | undefined): string | null {
  return identity?.traits?.email ?? null;
}

export function hasVerifiedEmail(identity: KratosIdentity | undefined): boolean {
  return (
    identity?.verifiable_addresses?.some(
      (address) =>
        address.via === 'email' && address.verified === true && address.status === 'completed',
    ) ?? false
  );
}

export function identityEmail(identity: KratosIdentity | undefined): string | null {
  return extractEmail(identity);
}

/**
 * Kratos cannot finish recovery for API/native clients, so with
 * `use_continue_with_transitions` enabled it returns the session token via
 * `continue_with` instead of a top-level `session_token`.
 */
export function recoverySessionToken(result: KratosAuthResult): string | undefined {
  if (result.session_token) return result.session_token;
  return result.continue_with?.find((entry) => entry.action === 'set_ory_session_token')
    ?.ory_session_token;
}

export function createRegistrationFlow(t: KratosTransport): Promise<KratosFlow> {
  return t.request<KratosFlow>('GET', '/self-service/registration/api');
}

export function submitRegistration(
  t: KratosTransport,
  flow: KratosFlow,
  email: string,
  password: string,
): Promise<KratosAuthResult> {
  return t.request<KratosAuthResult>('POST', flow.ui.action, {
    method: 'password',
    traits: { email },
    password,
  });
}

export function createLoginFlow(t: KratosTransport): Promise<KratosFlow> {
  return t.request<KratosFlow>('GET', '/self-service/login/api');
}

export function submitLogin(
  t: KratosTransport,
  flow: KratosFlow,
  email: string,
  password: string,
): Promise<KratosAuthResult> {
  return t.request<KratosAuthResult>('POST', flow.ui.action, {
    method: 'password',
    identifier: email,
    password,
  });
}

export function createVerificationFlow(t: KratosTransport): Promise<KratosFlow> {
  return t.request<KratosFlow>('GET', '/self-service/verification/api');
}

export function submitVerificationEmail(
  t: KratosTransport,
  flow: KratosFlow,
  email: string,
): Promise<unknown> {
  return t.request<unknown>('POST', flow.ui.action, { method: 'code', email });
}

export function submitVerificationCode(
  t: KratosTransport,
  flow: KratosFlow,
  code: string,
): Promise<unknown> {
  return t.request<unknown>('POST', flow.ui.action, { method: 'code', code });
}

export function createRecoveryFlow(t: KratosTransport): Promise<KratosFlow> {
  return t.request<KratosFlow>('GET', '/self-service/recovery/api');
}

export function submitRecoveryEmail(
  t: KratosTransport,
  flow: KratosFlow,
  email: string,
): Promise<unknown> {
  return t.request<unknown>('POST', flow.ui.action, { method: 'code', email });
}

export function submitRecoveryCode(
  t: KratosTransport,
  flow: KratosFlow,
  code: string,
): Promise<KratosAuthResult> {
  return t.request<KratosAuthResult>('POST', flow.ui.action, { method: 'code', code });
}

export function whoami(t: KratosTransport, token: string): Promise<KratosWhoami> {
  return t.request<KratosWhoami>('GET', '/sessions/whoami', undefined, token);
}

export function logout(t: KratosTransport, token: string): Promise<void> {
  return t.request<void>('DELETE', '/self-service/logout/api', { session_token: token });
}

export function createSettingsFlow(t: KratosTransport, token: string): Promise<KratosFlow> {
  return t.request<KratosFlow>('GET', '/self-service/settings/api', undefined, token);
}

export function submitPasswordChange(
  t: KratosTransport,
  flow: KratosFlow,
  options: { password: string; email: string | null; token: string },
): Promise<unknown> {
  return t.request<unknown>(
    'POST',
    flow.ui.action,
    {
      method: 'password',
      password: options.password,
      ...(options.email ? { traits: { email: options.email } } : {}),
    },
    options.token,
  );
}
