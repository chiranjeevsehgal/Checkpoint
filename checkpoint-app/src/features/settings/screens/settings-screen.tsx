import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function SettingsScreen() {
  return (
    <View className="flex-1 items-center justify-center bg-background px-6">
      <Text variant="h1">Settings</Text>
      <Text variant="muted" className="mt-2">
        App settings go here.
      </Text>
    </View>
  );
}
