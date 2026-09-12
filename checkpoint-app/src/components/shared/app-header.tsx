import { View } from 'react-native';

import { ThemeToggle } from '@/components/shared/theme-toggle';
import { Text } from '@/components/ui/text';

export function AppHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <View className="flex-row items-start justify-between gap-4 py-5">
      <View className="flex-1 gap-0.5">
        <Text variant="h2">{title}</Text>
        {subtitle ? <Text variant="muted">{subtitle}</Text> : null}
      </View>
      <ThemeToggle />
    </View>
  );
}
