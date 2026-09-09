import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function LoadingScreen({ message = 'Loading…' }: { message?: string }) {
  return (
    <View className="flex-1 items-center justify-center bg-background">
      <Text variant="muted">{message}</Text>
    </View>
  );
}
