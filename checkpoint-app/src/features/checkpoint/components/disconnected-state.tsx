import { useRouter } from 'expo-router';

import { EmptyState } from '@/components/shared/empty-state';
import { Button } from '@/components/ui/button';
import { Text } from '@/components/ui/text';

export function DisconnectedState() {
  const router = useRouter();
  return (
    <EmptyState
      title="Pendant not connected"
      hint="Connect to your pendant to view storage and transfers."
      action={
        <Button variant="outline" onPress={() => router.navigate('/(app)/(tabs)/connect')}>
          <Text>Go to Pendant</Text>
        </Button>
      }
    />
  );
}
