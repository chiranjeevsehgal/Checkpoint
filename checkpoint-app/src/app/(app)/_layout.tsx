import { Redirect, Stack } from 'expo-router';

import { LoadingScreen } from '@/components/shared/loading-screen';
import { useAuth } from '@/features/auth/hooks/useAuth';

export default function AppLayout() {
  const { status } = useAuth();
  if (status === 'loading') return <LoadingScreen message="Starting…" />;
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
      <Stack.Screen name="mcp-keys" options={{ title: 'MCP access' }} />
      <Stack.Screen name="languages" options={{ title: 'Languages' }} />
    </Stack>
  );
}
