import { SafeAreaView } from 'react-native-safe-area-context';

import { BluetoothBanner } from '@/components/shared/bluetooth-banner';
import { ConnectivityBanner } from '@/components/shared/connectivity-banner';
import { cn } from '@/lib/utils';

export function Screen({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SafeAreaView>) {
  return (
    <SafeAreaView className={cn('flex-1 bg-background px-5', className)} {...props}>
      <ConnectivityBanner />
      <BluetoothBanner />
      {children}
    </SafeAreaView>
  );
}
