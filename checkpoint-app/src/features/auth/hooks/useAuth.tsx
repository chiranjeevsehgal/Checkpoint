import { useEffect, useSyncExternalStore, type PropsWithChildren } from 'react';
import { AppState } from 'react-native';

import {
  changePassword,
  confirmEmailVerification,
  confirmPasswordRecovery,
  getAuthState,
  hasPendingVerification,
  initializeAuth,
  reauthenticate,
  reconnectAuth,
  requestEmailVerification,
  requestPasswordRecovery,
  signIn,
  signOut,
  signOutEverywhere,
  signUp,
  subscribeAuth,
} from '@/features/auth/auth-store';

export function AuthProvider({ children }: PropsWithChildren) {
  const { status } = useAuth();

  useEffect(() => {
    void initializeAuth();
  }, []);

  // Retry the session check with backoff after an outage, and again whenever
  // the user returns to the app.
  useEffect(() => {
    if (status !== 'unavailable') return;
    void reconnectAuth();
    const subscription = AppState.addEventListener('change', (next) => {
      if (next === 'active') void reconnectAuth();
    });
    return () => subscription.remove();
  }, [status]);

  return children;
}

export function useAuth() {
  const state = useSyncExternalStore(subscribeAuth, getAuthState, getAuthState);
  return {
    ...state,
    signIn,
    signUp,
    signOut,
    signOutEverywhere,
    requestEmailVerification,
    confirmEmailVerification,
    hasPendingVerification,
    requestPasswordRecovery,
    confirmPasswordRecovery,
    changePassword,
    reauthenticate,
    refresh: initializeAuth,
  };
}
