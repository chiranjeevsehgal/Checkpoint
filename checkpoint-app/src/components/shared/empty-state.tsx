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
    <View className="items-center justify-center gap-1 px-4 py-12">
      <Text className="font-display text-[15px]">{title}</Text>
      {hint ? (
        <Text variant="muted" className="text-center">
          {hint}
        </Text>
      ) : null}
    </View>
  );
}
