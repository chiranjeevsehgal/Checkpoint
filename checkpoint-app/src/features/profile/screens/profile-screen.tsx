import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function ProfileScreen({ id }: { id: string }) {
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text variant="h1">Profile</Text>
      <Text variant="muted" className="mt-2">
        User {id}
      </Text>
    </View>
  );
}
