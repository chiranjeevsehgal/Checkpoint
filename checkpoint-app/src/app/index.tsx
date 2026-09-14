import { Redirect } from 'expo-router';

import { useAuth } from '@/features/auth/hooks/useAuth';

export default function Index() {
  const { status } = useAuth();
  if (status === 'loading') return null;
  if (status === 'unverified') return <Redirect href="/(public)/verify-email" />;
  if (status === 'authenticated' || status === 'unavailable') {
    return <Redirect href="/(app)/(tabs)/connect" />;
  }
  return <Redirect href="/(public)/sign-in" />;
}
