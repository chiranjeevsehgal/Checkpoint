import { Pressable, View } from 'react-native';

import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

interface ToggleProps {
  label: string;
  description?: string;
  value: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
}

export function Toggle({ label, description, value, onChange, disabled }: ToggleProps) {
  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityState={{ checked: value, disabled }}
      disabled={disabled}
      onPress={() => onChange(!value)}
      className={cn('flex-row items-start justify-between gap-3', disabled && 'opacity-45')}
    >
      <View className="flex-1 gap-0.5">
        <Text className="font-label text-sm">{label}</Text>
        {description ? (
          <Text variant="muted" className="text-[11px]">
            {description}
          </Text>
        ) : null}
      </View>
      <View
        className={cn(
          'border-border h-6 w-11 flex-none justify-center border',
          value ? 'bg-primary items-end' : 'bg-input-bg items-start'
        )}
      >
        <View className="bg-background m-0.5 h-[18px] w-[18px]" />
      </View>
    </Pressable>
  );
}
