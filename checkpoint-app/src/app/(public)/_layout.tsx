import { Redirect, Stack } from 'expo-router';

import { useAuth } from '@/features/auth/hooks/useAuth';

export default function PublicLayout() {
  const { status } = useAuth();
  if (status === 'loading') return null;
  if (status === 'authenticated' || status === 'unavailable') {
    return <Redirect href="/(app)/(tabs)/connect" />;
  }
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="sign-in" options={{ title: 'Sign in' }} />
      <Stack.Screen name="sign-up" options={{ title: 'Sign up' }} />
      <Stack.Screen name="verify-email" options={{ title: 'Verify email' }} />
      <Stack.Screen name="recovery" options={{ title: 'Reset password' }} />
    </Stack>
  );
}
