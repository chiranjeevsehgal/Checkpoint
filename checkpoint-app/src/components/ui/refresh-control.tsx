import { useColorScheme } from 'nativewind';
import { RefreshControl, type RefreshControlProps } from 'react-native';

import { PALETTE } from '@/lib/theme';

export function AppRefreshControl({ refreshing, onRefresh, ...props }: RefreshControlProps) {
  const { colorScheme } = useColorScheme();
  const palette = colorScheme === 'dark' ? PALETTE.dark : PALETTE.light;

  return (
    <RefreshControl
      refreshing={refreshing}
      onRefresh={onRefresh}
      tintColor={palette.primary}
      colors={[palette.primary]}
      progressBackgroundColor={palette.surface}
      {...props}
    />
  );
}
