export type AuthStatus =
  | 'loading'
  | 'anonymous'
  | 'unverified'
  | 'authenticated'
  | 'unavailable'
  | 'reconnecting'
  | 'deleting';

export interface AuthState {
  status: AuthStatus;
  identityId: string | null;
  email: string | null;
  name: string | null;
}

export const initialAuthState: AuthState = {
  status: 'loading',
  identityId: null,
  email: null,
  name: null,
};
