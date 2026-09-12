import { useRouter } from 'expo-router';
import { ArrowLeft, Terminal } from 'lucide-react-native';
import { Pressable, View } from 'react-native';

import { HeaderIconButton } from '@/components/shared/header-icon-button';
import { ThemeToggle } from '@/components/shared/theme-toggle';
import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';

interface AppHeaderProps {
  title: string;
  subtitle?: string;
  onBack?: () => void;
  showDebugLog?: boolean;
  action?: React.ReactNode;
}

export function AppHeader({
  title,
  subtitle,
  onBack,
  showDebugLog = true,
  action,
}: AppHeaderProps) {
  const router = useRouter();

  return (
    <View className="flex-row items-start justify-between gap-3 py-5">
      <View className="flex-1 flex-row items-start gap-2">
        {onBack ? (
          <Pressable
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Go back"
            className="active:opacity-70 pt-0.5"
          >
            <Icon as={ArrowLeft} size={22} />
          </Pressable>
        ) : null}
        <View className="flex-1 gap-0.5">
          <Text variant="h2">{title}</Text>
          {subtitle ? <Text variant="muted">{subtitle}</Text> : null}
        </View>
      </View>
      <View className="flex-row items-center gap-2">
        {action}
        {showDebugLog ? (
          <HeaderIconButton
            icon={Terminal}
            label="Open debug log"
            onPress={() => router.push('/debug-log')}
          />
        ) : null}
        <ThemeToggle />
      </View>
    </View>
  );
}
