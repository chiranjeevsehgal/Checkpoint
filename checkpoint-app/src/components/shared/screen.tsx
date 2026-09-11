import { SafeAreaView } from 'react-native-safe-area-context';

import { cn } from '@/lib/utils';

export function Screen({
  className,
  ...props
}: React.ComponentProps<typeof SafeAreaView>) {
  return (
    <SafeAreaView
      className={cn('bg-background flex-1 px-5', className)}
      {...props}
    />
  );
}
