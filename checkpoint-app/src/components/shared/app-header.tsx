import { useRouter } from 'expo-router';
import { ArrowLeft, CircleUser } from 'lucide-react-native';
import { Pressable, View } from 'react-native';

import { HeaderIconButton } from '@/components/shared/header-icon-button';
import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';

interface AppHeaderProps {
  title: string;
  subtitle?: string;
  onBack?: () => void;
  action?: React.ReactNode;
}

export function AppHeader({ title, subtitle, onBack, action }: AppHeaderProps) {
  const router = useRouter();

  return (
    <View className="flex-row items-start justify-between gap-3 py-5">
      <View className="flex-1 flex-row items-start gap-2">
        {onBack ? (
          <Pressable
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Go back"
            className="pt-0.5 active:opacity-70"
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
        {!onBack ? (
          <HeaderIconButton
            icon={CircleUser}
            label="Account"
            onPress={() => router.push('/account')}
          />
        ) : null}
      </View>
    </View>
  );
}
