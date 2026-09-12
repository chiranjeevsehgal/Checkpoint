import type { ComponentProps } from 'react';

import { BluetoothBanner } from './bluetooth-banner.tsx';
import { ConnectivityBanner } from './connectivity-banner.tsx';

import { Screen } from '@/components/shared/screen';

export function CheckpointScreen({ children, ...props }: ComponentProps<typeof Screen>) {
  return (
    <Screen {...props}>
      <ConnectivityBanner />
      <BluetoothBanner />
      {children}
    </Screen>
  );
}
