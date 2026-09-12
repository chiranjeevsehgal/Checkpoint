import { useLocalSearchParams } from 'expo-router';

import { ProfileScreen } from '@/features/profile/screens/profile-screen';

export default function ProfileRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <ProfileScreen id={Array.isArray(id) ? (id[0] ?? '') : (id ?? '')} />;
}
