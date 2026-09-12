export const routes = {
  connect: '/(app)/(tabs)/connect',
  transfers: '/(app)/(tabs)/transfers',
  device: '/(app)/(tabs)/device',
  storage: '/(app)/(tabs)/storage',
  settings: '/(app)/(tabs)/settings',
  signIn: '/(public)/sign-in',
  signUp: '/(public)/sign-up',
  profile: (id: string) => `/(app)/profile/${id}`,
} as const;
