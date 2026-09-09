import { SafeAreaView } from 'react-native-safe-area-context';

import { cn } from '@/lib/utils';

export function Screen({
  className,
  ...props
}: React.ComponentProps<typeof SafeAreaView>) {
  return (
    <SafeAreaView
      className={cn('flex-1 bg-background px-6', className)}
      {...props}
    />
  );
}
