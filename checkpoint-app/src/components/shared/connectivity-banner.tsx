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
    className: 'bg-destructive',
  },
  'server-unavailable': {
    icon: ServerOff,
    label: 'Server unavailable — audio saved, will sync later',
    className: 'bg-destructive',
  },
  unstable: {
    icon: TriangleAlert,
    label: 'Unstable connection — retrying',
    className: 'bg-warning',
  },
} as const;

export function ConnectivityBanner() {
  const { state } = useNetworkStatus();
  if (state === 'online') return null;
  const banner = BANNERS[state];
  return (
    <View
      className={cn('mb-3 flex-row items-center justify-center gap-2 px-3 py-2', banner.className)}
    >
      <Icon as={banner.icon} size={14} className="text-background" />
      <Text className="text-[12px] text-background">{banner.label}</Text>
    </View>
  );
}
