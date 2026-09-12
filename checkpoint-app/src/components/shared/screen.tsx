import { KeyboardAvoidingView, Platform } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { cn } from '@/lib/utils';

export function Screen({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SafeAreaView>) {
  return (
    <KeyboardAvoidingView
      style={{ flex: 1 }}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <SafeAreaView className={cn('flex-1 bg-background px-4', className)} {...props}>
        {children}
      </SafeAreaView>
    </KeyboardAvoidingView>
  );
}
