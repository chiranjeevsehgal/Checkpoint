import { Moon, Sun } from 'lucide-react-native';
import { Pressable } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { useAppTheme } from '@/providers/theme-provider';

export function ThemeToggle() {
  const { scheme, toggleTheme } = useAppTheme();

  return (
    <Pressable
      onPress={toggleTheme}
      accessibilityRole="button"
      accessibilityLabel="Toggle theme"
      className="border-border active:bg-foreground/10 h-9 w-9 flex-none items-center justify-center border"
    >
      <Icon as={scheme === 'dark' ? Sun : Moon} size={18} />
    </Pressable>
  );
}
