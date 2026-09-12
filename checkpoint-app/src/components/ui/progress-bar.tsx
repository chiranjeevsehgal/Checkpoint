import { View } from 'react-native';

import { cn } from '@/lib/utils';

interface ProgressBarProps {
  value: number;
  className?: string;
}

export function ProgressBar({ value, className }: ProgressBarProps) {
  const pct = Math.max(0, Math.min(1, value)) * 100;

  return (
    <View className={cn('bg-divider w-full overflow-hidden', className)}>
      <View className="bg-muted-foreground h-full" style={{ width: `${pct}%` }} />
    </View>
  );
}
