import { Pressable, View } from 'react-native';

import { cn } from '@/lib/utils';

interface SwitchProps {
  value: boolean;
  onValueChange: (next: boolean) => void;
  disabled?: boolean;
  className?: string;
}

export function Switch({ value, onValueChange, disabled, className }: SwitchProps) {
  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityState={{ checked: value, disabled }}
      disabled={disabled}
      onPress={() => onValueChange(!value)}
      className={cn(
        'h-6 w-11 flex-none justify-center active:opacity-80',
        value ? 'items-end bg-primary' : 'items-start bg-border',
        disabled && 'opacity-45',
        className,
      )}
    >
      <View className="m-0.5 h-[18px] w-[18px] bg-switch-thumb" />
    </Pressable>
  );
}
