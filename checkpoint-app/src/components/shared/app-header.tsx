import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function AppHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <View className="gap-1 py-4">
      <Text variant="h2">{title}</Text>
      {subtitle ? <Text variant="muted">{subtitle}</Text> : null}
    </View>
  );
}
