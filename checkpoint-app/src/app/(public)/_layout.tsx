import { Redirect, Stack } from 'expo-router';

import { LoadingScreen } from '@/components/shared/loading-screen';
import { useAuth } from '@/features/auth/hooks/useAuth';

export default function PublicLayout() {
  const { status } = useAuth();
  if (status === 'loading') return <LoadingScreen message="Starting…" />;
  if (status === 'authenticated' || status === 'unavailable' || status === 'reconnecting') {
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
