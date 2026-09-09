import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function EmptyState({
  title = 'Nothing here yet',
  hint,
}: {
  title?: string;
  hint?: string;
}) {
  return (
    <View className="flex-1 items-center justify-center gap-2 bg-background px-6">
      <Text variant="h3">{title}</Text>
      {hint ? <Text variant="muted">{hint}</Text> : null}
    </View>
  );
}
