import { useEffect, useSyncExternalStore, type PropsWithChildren } from 'react';

import {
  changePassword,
  confirmEmailVerification,
  confirmPasswordRecovery,
  getAuthState,
  initializeAuth,
  requestEmailVerification,
  requestPasswordRecovery,
  signIn,
  signOut,
  signOutEverywhere,
  signUp,
  subscribeAuth,
} from '@/features/auth/auth-store';

export function AuthProvider({ children }: PropsWithChildren) {
  useEffect(() => {
    void initializeAuth();
  }, []);
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
    refresh: initializeAuth,
  };
}
