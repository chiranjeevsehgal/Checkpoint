import { useEffect, useSyncExternalStore, type PropsWithChildren } from 'react';
import { AppState } from 'react-native';

import {
  changePassword,
  confirmEmailVerification,
  confirmPasswordRecovery,
  getAuthState,
  initializeAuth,
  reauthenticate,
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

  // Retry the session check when the user returns to the app after an outage.
  useEffect(() => {
    if (status !== 'unavailable') return;
    const subscription = AppState.addEventListener('change', (next) => {
      if (next === 'active') void initializeAuth();
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
    requestPasswordRecovery,
    confirmPasswordRecovery,
    changePassword,
    reauthenticate,
    refresh: initializeAuth,
  };
}
