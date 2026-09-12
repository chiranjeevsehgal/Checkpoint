import { ChevronDown } from 'lucide-react-native';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

interface DeveloperDetailsProps {
  children: React.ReactNode;
  defaultExpanded?: boolean;
  className?: string;
}

export function DeveloperDetails({
  children,
  defaultExpanded = false,
  className,
}: DeveloperDetailsProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  return (
    <View className={cn('border-t border-divider pt-2', className)}>
      <Pressable
        onPress={() => setExpanded((open) => !open)}
        accessibilityRole="button"
        accessibilityState={{ expanded }}
        className="flex-row items-center justify-between gap-2 active:opacity-70"
      >
        <Text variant="kicker">Developer details</Text>
        <View style={{ transform: [{ rotate: expanded ? '180deg' : '0deg' }] }}>
          <Icon as={ChevronDown} size={14} />
        </View>
      </Pressable>
      {expanded ? <View className="gap-1 pt-2">{children}</View> : null}
    </View>
  );
}

export function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <View className="flex-row items-baseline justify-between gap-3">
      <Text variant="muted" className="text-[11px]">
        {label}
      </Text>
      <Text className="flex-1 text-right font-mono text-[11px] text-subtle-foreground">
        {value}
      </Text>
    </View>
  );
}
