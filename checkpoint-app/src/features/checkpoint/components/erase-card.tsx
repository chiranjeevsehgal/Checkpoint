import { ActivityIndicator } from 'react-native';

import { useCheckpoint } from '../hooks/useCheckpoint.tsx';

import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Text } from '@/components/ui/text';

export function EraseCard() {
  const { connected, deleting, erasing, requestErase } = useCheckpoint();

  return (
    <Card>
      <Text variant="muted" className="text-[12px]">
        Permanently delete all recordings stored on the pendant. This cannot be undone.
      </Text>
      <Button
        variant="outline"
        className="border-destructive"
        disabled={!connected || deleting !== null || erasing}
        onPress={requestErase}
      >
        {erasing ? (
          <ActivityIndicator size="small" />
        ) : (
          <Text className="text-destructive">Erase all recordings</Text>
        )}
      </Button>
    </Card>
  );
}
