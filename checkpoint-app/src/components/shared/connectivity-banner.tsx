import { ServerOff, TriangleAlert, WifiOff } from 'lucide-react-native';
import { View } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import { useNetworkStatus } from '@/features/checkpoint/hooks/useNetworkStatus.ts';
import { cn } from '@/lib/utils';

const BANNERS = {
  offline: {
    icon: WifiOff,
    label: 'Offline — no internet connection',
    className: 'bg-primary',
  },
  'server-unavailable': {
    icon: ServerOff,
    label: 'Server unavailable — audio saved, will sync later',
    className: 'bg-primary',
  },
  unstable: {
    icon: TriangleAlert,
    label: 'Unstable connection — retrying',
    className: 'bg-accent-600',
  },
} as const;

export function ConnectivityBanner() {
  const { state } = useNetworkStatus();
  if (state === 'online') return null;
  const banner = BANNERS[state];
  return (
    <View className={cn('mb-3 flex-row items-center justify-center gap-2 px-3 py-2', banner.className)}>
      <Icon as={banner.icon} size={14} className="text-primary-foreground" />
      <Text className="text-primary-foreground text-[12px]">{banner.label}</Text>
    </View>
  );
}
