import type { LucideIcon } from 'lucide-react-native';
import { View } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';

export function EmptyState({
  icon,
  title = 'Nothing here yet',
  hint,
  action,
}: {
  icon?: LucideIcon;
  title?: string;
  hint?: string;
  action?: React.ReactNode;
}) {
  return (
    <View className="items-center justify-center gap-1 px-4 py-12">
      {icon ? <Icon as={icon} size={28} className="mb-2 text-muted-foreground" /> : null}
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
