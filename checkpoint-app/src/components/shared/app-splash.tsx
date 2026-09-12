import { View } from 'react-native';

import { PendantLogo } from '@/components/shared/pendant-logo';
import { Text } from '@/components/ui/text';

export function AppSplash() {
  return (
    <View className="flex-1 items-center justify-center gap-8 bg-background">
      <PendantLogo size={92} />
      <Text className="font-display text-[13px] tracking-[0.35em]">A CLEARER TOMORROW</Text>
    </View>
  );
}
