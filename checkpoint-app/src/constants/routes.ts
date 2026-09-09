export const routes = {
  home: '/(app)/(tabs)/',
  search: '/(app)/(tabs)/search',
  settings: '/(app)/(tabs)/settings',
  signIn: '/(public)/sign-in',
  signUp: '/(public)/sign-up',
  profile: (id: string) => `/(app)/profile/${id}`,
} as const;
