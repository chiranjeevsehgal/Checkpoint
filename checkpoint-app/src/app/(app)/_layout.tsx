import { Stack } from 'expo-router';

// TODO: add auth guard — redirect to /(public)/sign-in when no session.
export default function AppLayout() {
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="(tabs)" />
      <Stack.Screen name="debug-log" options={{ title: 'Debug Log' }} />
      <Stack.Screen name="profile/[id]" options={{ title: 'Profile' }} />
    </Stack>
  );
}
