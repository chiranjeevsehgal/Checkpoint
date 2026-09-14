import { Redirect, Stack } from 'expo-router';

import { useAuth } from '@/features/auth/hooks/useAuth';

export default function AppLayout() {
  const { status } = useAuth();
  if (status === 'loading') return null;
  if (status === 'anonymous' || status === 'deleting') {
    return <Redirect href="/(public)/sign-in" />;
  }
  if (status === 'unverified') {
    return <Redirect href="/(public)/verify-email" />;
  }
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="(tabs)" />
      <Stack.Screen name="debug-log" options={{ title: 'Debug Log' }} />
      <Stack.Screen name="account" options={{ title: 'Account' }} />
    </Stack>
  );
}
