import { View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Text } from '@/components/ui/text';

export function HomeScreen() {
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text variant="h1">Home</Text>
      <Text variant="muted" className="mt-2">
        Checkpoint production skeleton
      </Text>
      <Button className="mt-6">
        <Text>Get started</Text>
      </Button>
    </View>
  );
}
