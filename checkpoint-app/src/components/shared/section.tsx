import { View } from 'react-native';

import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

interface SectionProps {
  title?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

export function Section({ title, action, children, className }: SectionProps) {
  return (
    <View className={cn('gap-3', className)}>
      {title || action ? (
        <View className="flex-row items-center justify-between gap-2">
          {title ? (
            <Text variant="kicker" className="flex-1">
              {title}
            </Text>
          ) : (
            <View className="flex-1" />
          )}
          {action}
        </View>
      ) : null}
      {children}
    </View>
  );
}
