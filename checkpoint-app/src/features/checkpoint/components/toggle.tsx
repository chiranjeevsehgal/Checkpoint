import { Pressable, View } from 'react-native';

import { Switch } from '@/components/ui/switch';
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
      className={cn(
        'flex-row items-start justify-between gap-3 active:opacity-80',
        disabled && 'opacity-45',
      )}
    >
      <View className="flex-1 gap-0.5">
        <Text className="font-label text-sm">{label}</Text>
        {description ? (
          <Text variant="muted" className="text-[11px]">
            {description}
          </Text>
        ) : null}
      </View>
      <Switch value={value} onValueChange={onChange} disabled={disabled} />
    </Pressable>
  );
}
