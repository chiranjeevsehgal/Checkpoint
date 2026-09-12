import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export function EmptyState({
  title = 'Nothing here yet',
  hint,
  action,
}: {
  title?: string;
  hint?: string;
  action?: React.ReactNode;
}) {
  return (
    <View className="items-center justify-center gap-1 px-4 py-12">
      <Text className="font-display text-[15px]">{title}</Text>
      {hint ? (
        <Text variant="muted" className="text-center">
          {hint}
        </Text>
      ) : null}
      {action ? <View className="mt-2">{action}</View> : null}
    </View>
  );
}
