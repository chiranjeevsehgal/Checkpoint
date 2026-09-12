import { BluetoothOff, TriangleAlert } from 'lucide-react-native';
import { View } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import { useBluetoothState } from '@/features/checkpoint/hooks/useBluetoothState.ts';
import { cn } from '@/lib/utils';

const BANNERS = {
  off: {
    icon: BluetoothOff,
    label: 'Bluetooth is off — turn it on to sync',
    className: 'bg-warning',
  },
  unauthorized: {
    icon: TriangleAlert,
    label: 'Bluetooth permission needed',
    className: 'bg-warning',
  },
  unsupported: {
    icon: BluetoothOff,
    label: 'Bluetooth is not supported on this device',
    className: 'bg-warning',
  },
} as const;

export function BluetoothBanner() {
  const bluetooth = useBluetoothState();
  if (bluetooth !== 'off' && bluetooth !== 'unauthorized' && bluetooth !== 'unsupported') {
    return null;
  }
  const banner = BANNERS[bluetooth];
  return (
    <View
      className={cn('mb-3 flex-row items-center justify-center gap-2 px-3 py-2', banner.className)}
    >
      <Icon as={banner.icon} size={14} className="text-background" />
      <Text className="text-[12px] text-background">{banner.label}</Text>
    </View>
  );
}
