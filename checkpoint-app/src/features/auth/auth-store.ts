import {
  KratosError,
  createFetchTransport,
  createLoginFlow,
  createRecoveryFlow,
  createRegistrationFlow,
  createSettingsFlow,
  createVerificationFlow,
  hasVerifiedEmail,
  identityEmail,
  logout,
  recoverySessionToken,
  submitLogin,
  submitPasswordChange,
  submitRecoveryCode,
  submitRecoveryEmail,
  submitRegistration,
  submitVerificationCode,
  submitVerificationEmail,
  whoami,
  type KratosFlow,
  type KratosIdentity,
  type KratosTransport,
} from './kratos-client';
import { initialAuthState, type AuthState } from './types';

import { revokeSessions } from '@/lib/api/account-api';
import { loadServerConfig, resolveKratosUrl } from '@/lib/server-config';
import {
  clearSession,
  getSessionToken,
  loadSession,
  saveSession,
  type Session,
} from '@/lib/session';

let state: AuthState = initialAuthState;
const listeners = new Set<() => void>();

let transport: KratosTransport = createFetchTransport(resolveKratosUrl());
let pendingVerification: KratosFlow | null = null;
let pendingRecovery: KratosFlow | null = null;

export function setAuthTransport(next: KratosTransport): void {
  transport = next;
}

/** Rebuilds the transport from the current (possibly dev-overridden) Kratos URL. */
export function applyServerConfig(): void {
  transport = createFetchTransport(resolveKratosUrl());
}

export function subscribeAuth(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function getAuthState(): AuthState {
  return state;
}

function setState(next: Partial<AuthState>): void {
  state = { ...state, ...next };
  for (const listener of listeners) listener();
}

function resetToAnonymous(): void {
  setState({ status: 'anonymous', identityId: null, email: null });
}

function isNetworkError(error: unknown): boolean {
  return !(error instanceof KratosError);
}

function locationRequired(error: unknown): boolean {
  return error instanceof KratosError && error.id === 'browser_location_change_required';
}

async function persistFromAuthResult(
  identity: KratosIdentity | undefined,
  token: string | undefined,
): Promise<void> {
  if (!token || !identity?.id) return;
  await saveSession({ token, identityId: identity.id });
}

export async function initializeAuth(): Promise<void> {
  setState({ status: 'loading' });
  let session: Session | null = null;
  try {
    await loadServerConfig();
    applyServerConfig();
    session = await loadSession();
    if (!session) {
      resetToAnonymous();
      return;
    }
    const result = await whoami(transport, session.token);
    const identity = result.identity;
    const email = identityEmail(identity);
    if (hasVerifiedEmail(identity)) {
      setState({ status: 'authenticated', identityId: identity?.id ?? session.identityId, email });
    } else {
      setState({ status: 'unverified', identityId: identity?.id ?? session.identityId, email });
    }
  } catch (error) {
    if (error instanceof KratosError && error.status === 401) {
      await clearSession();
      resetToAnonymous();
      return;
    }
    if (session) {
      // Transient outage: keep the token so the user is not silently logged out.
      setState({ status: 'unavailable', identityId: session.identityId, email: null });
    } else {
      resetToAnonymous();
    }
  }
}

export async function signUp(email: string, password: string): Promise<void> {
  const flow = await createRegistrationFlow(transport);
  const result = await submitRegistration(transport, flow, email, password);
  await persistFromAuthResult(result.identity ?? result.session?.identity, result.session_token);
  setState({ status: 'unverified', identityId: result.identity?.id ?? null, email });
}

export async function signIn(email: string, password: string): Promise<void> {
  const flow = await createLoginFlow(transport);
  const result = await submitLogin(transport, flow, email, password);
  const identity = result.session?.identity ?? result.identity;
  if (!result.session_token) {
    throw new Error('Sign in did not return a session.');
  }
  await saveSession({ token: result.session_token, identityId: identity?.id ?? '' });
  const verified = hasVerifiedEmail(identity);
  setState({
    status: verified ? 'authenticated' : 'unverified',
    identityId: identity?.id ?? null,
    email,
  });
}

export async function requestEmailVerification(email: string): Promise<void> {
  const flow = await createVerificationFlow(transport);
  pendingVerification = flow;
  await submitVerificationEmail(transport, flow, email);
}

export async function confirmEmailVerification(code: string): Promise<void> {
  if (!pendingVerification) throw new Error('Start email verification first.');
  await submitVerificationCode(transport, pendingVerification, code);
  pendingVerification = null;
  const token = getSessionToken();
  if (!token) {
    resetToAnonymous();
    return;
  }
  const result = await whoami(transport, token);
  const email = identityEmail(result.identity);
  setState({
    status: hasVerifiedEmail(result.identity) ? 'authenticated' : 'unverified',
    identityId: result.identity?.id ?? state.identityId,
    email,
  });
}

export async function requestPasswordRecovery(email: string): Promise<void> {
  const flow = await createRecoveryFlow(transport);
  pendingRecovery = flow;
  await submitRecoveryEmail(transport, flow, email);
}

export async function confirmPasswordRecovery(code: string, newPassword: string): Promise<void> {
  if (!pendingRecovery) throw new Error('Start password recovery first.');
  const result = await submitRecoveryCode(transport, pendingRecovery, code);
  pendingRecovery = null;
  const token = recoverySessionToken(result);
  const identity = result.session?.identity ?? result.identity;
  if (!token) throw new Error('Recovery did not return a session.');
  const settings = await createSettingsFlow(transport, token);
  await submitPasswordChange(transport, settings, {
    password: newPassword,
    email: identityEmail(identity),
    token,
  });
  try {
    await logout(transport, token);
  } catch {
    // The password is already changed; still drop the local session.
  }
  await clearSession();
  resetToAnonymous();
}

export async function changePassword(email: string | null, newPassword: string): Promise<void> {
  const token = getSessionToken();
  if (!token) throw new Error('Not signed in.');
  const flow = await createSettingsFlow(transport, token);
  await submitPasswordChange(transport, flow, { password: newPassword, email, token });
  await revokeSessions(token);
}

export async function signOut(): Promise<void> {
  const token = getSessionToken();
  if (token) {
    try {
      await logout(transport, token);
    } catch {
      // Local sign-out proceeds even when the server is unreachable.
    }
  }
  await clearSession();
  resetToAnonymous();
}

export async function signOutEverywhere(): Promise<void> {
  const token = getSessionToken();
  if (token) {
    try {
      await revokeSessions(token);
    } catch {
      // best effort
    }
  }
  await signOut();
}

export function setDeleting(): void {
  setState({ status: 'deleting', identityId: null, email: null });
}

export { isNetworkError, locationRequired };
