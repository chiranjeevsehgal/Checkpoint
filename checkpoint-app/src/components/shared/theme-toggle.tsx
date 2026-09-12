import { Moon, Sun } from 'lucide-react-native';

import { HeaderIconButton } from '@/components/shared/header-icon-button';
import { useAppTheme } from '@/providers/theme-provider';

export function ThemeToggle() {
  const { scheme, toggleTheme } = useAppTheme();

  return (
    <HeaderIconButton
      icon={scheme === 'dark' ? Sun : Moon}
      label="Toggle theme"
      onPress={toggleTheme}
    />
  );
}
