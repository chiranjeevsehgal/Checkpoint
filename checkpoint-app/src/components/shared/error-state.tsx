import { View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Text } from '@/components/ui/text';

export function ErrorState({
  message = 'Something went wrong.',
  onRetry,
}: {
  message?: string;
  onRetry?: () => void;
}) {
  return (
    <View className="flex-1 items-center justify-center gap-4 bg-background px-6">
      <Text variant="h3">Error</Text>
      <Text variant="muted">{message}</Text>
      {onRetry ? (
        <Button onPress={onRetry}>
          <Text>Retry</Text>
        </Button>
      ) : null}
    </View>
  );
}
