import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function SearchScreen() {
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text variant="h1">Search</Text>
      <Text variant="muted" className="mt-2">
        Search goes here.
      </Text>
    </View>
  );
}
