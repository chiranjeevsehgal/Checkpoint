export type AuthStatus =
  'loading' | 'anonymous' | 'unverified' | 'authenticated' | 'unavailable' | 'deleting';

export interface AuthState {
  status: AuthStatus;
  identityId: string | null;
  email: string | null;
}

export const initialAuthState: AuthState = {
  status: 'loading',
  identityId: null,
  email: null,
};
