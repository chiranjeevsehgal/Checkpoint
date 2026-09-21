// User-facing error text for non-auth surfaces. Raw technical detail (HTTP
// statuses, host names, device codes) stays in the debug log; toasts,
// dialogs and inline errors only show the strings returned here.
type StatusError = { status: number; code?: string };

function isStatusError(error: unknown): error is StatusError {
  return error instanceof Error && typeof (error as { status?: unknown }).status === 'number';
}

function isNetworkError(error: unknown): boolean {
  if (error instanceof TypeError) return true;
  return error instanceof Error && error.name === 'AbortError';
}

export function describeUserError(error: unknown, fallback: string): string {
  if (isStatusError(error)) {
    if (error.status === 401) return 'Your session has expired. Please sign in again.';
    if (error.status >= 500) {
      return 'Checkpoint is having trouble right now. Please try again in a moment.';
    }
    return fallback;
  }
  if (isNetworkError(error)) {
    return "Can't reach Checkpoint. Check your connection and try again.";
  }
  return fallback;
}
