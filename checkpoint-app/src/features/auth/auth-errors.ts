import { KratosError } from './kratos-client.ts';

// Matches ApiError (and any Error carrying an HTTP status) without importing
// lib/api, which keeps this module usable from node:test without path aliases.
type StatusError = { status: number; code?: string };

function isStatusError(error: unknown): error is StatusError {
  return error instanceof Error && typeof (error as { status?: unknown }).status === 'number';
}

function mapMessage(text: string): string | null {
  const length = text.match(/at least (\d+) characters/i);
  if (length) return `Use at least ${length[1]} characters.`;
  if (/been found in data breaches|data breach/i.test(text)) {
    return 'That password has appeared in a data breach. Choose a different one.';
  }
  if (/already.*(registered|exists)/i.test(text)) {
    return 'An account with this email already exists. Try signing in instead.';
  }
  if (/too similar|similar to (the )?identifier|similar to your (email|name)/i.test(text)) {
    return 'Choose a password that is not similar to your email or name.';
  }
  if (/different from the old password|different from your current password/i.test(text)) {
    return 'Your new password must be different from your current password.';
  }
  if (/invalid or already used|code.*(invalid|expired)/i.test(text)) {
    return 'That code is incorrect or has expired. Request a new one.';
  }
  if (/credentials are invalid|invalid credentials/i.test(text)) {
    return 'Incorrect email or password.';
  }
  return null;
}

function describeKratosError(error: KratosError, fallback: string): string {
  for (const text of error.messages) {
    const mapped = mapMessage(text);
    if (mapped) return mapped;
  }
  if (error.status === 401) return 'Your session has expired. Please sign in again.';
  if (error.status === 403) return 'Please sign in again to continue.';
  if (error.status === 404 || error.status === 410) {
    return 'That request has expired. Please start again.';
  }
  if (error.status >= 500) {
    return 'Checkpoint is having trouble right now. Please try again in a moment.';
  }
  return fallback;
}

function describeStatusError(error: StatusError, fallback: string): string {
  if (error.code === 'REAUTH_REQUIRED') {
    return 'That confirmation expired. Send a new code and try again.';
  }
  if (error.code === 'ACCOUNT_DELETING') return 'This account is already being deleted.';
  if (error.status === 401) return 'Your session has expired. Please sign in again.';
  if (error.status >= 500) {
    return 'Checkpoint is having trouble right now. Please try again in a moment.';
  }
  return fallback;
}

export function describeAuthError(error: unknown, fallback: string): string {
  if (error instanceof KratosError) return describeKratosError(error, fallback);
  if (isStatusError(error)) return describeStatusError(error, fallback);
  if (error instanceof TypeError) {
    return "Can't reach Checkpoint. Check your connection and try again.";
  }
  return fallback;
}
