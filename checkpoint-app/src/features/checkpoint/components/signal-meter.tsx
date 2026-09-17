import { View } from 'react-native';

import { signalLabel, signalLevel, type SignalLevel } from '../connectionView.ts';

import { cn } from '@/lib/utils';

const BAR_HEIGHTS = [6, 9, 12, 15] as const;

function signalTone(level: SignalLevel): string {
  if (level >= 3) return 'bg-success';
  if (level === 2) return 'bg-warning';
  if (level === 1) return 'bg-destructive';
  return 'bg-border';
}

export function SignalMeter({ dbm }: { dbm: number }) {
  const level = signalLevel(dbm);
  return (
    <View
      className="flex-row items-end gap-0.5"
      accessible
      accessibilityLabel={`Bluetooth signal ${signalLabel(level)}`}
    >
      {BAR_HEIGHTS.map((height, index) => (
        <View
          key={height}
          style={{ height, width: 3 }}
          className={cn(index < level ? signalTone(level) : 'bg-border')}
        />
      ))}
    </View>
  );
}
