import type { LucideIcon } from 'lucide-react-native';
import { Pressable } from 'react-native';

import { Icon } from '@/components/ui/icon';

interface HeaderIconButtonProps {
  icon: LucideIcon;
  label: string;
  onPress: () => void;
}

export function HeaderIconButton({ icon, label, onPress }: HeaderIconButtonProps) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      hitSlop={8}
      className="active:bg-foreground/10 h-9 w-9 flex-none items-center justify-center border border-border"
    >
      <Icon as={icon} size={18} />
    </Pressable>
  );
}
