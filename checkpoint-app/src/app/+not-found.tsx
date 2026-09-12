import { Link, Stack } from 'expo-router';
import { View } from 'react-native';

import { Text } from '@/components/ui/text';

export default function NotFoundScreen() {
  return (
    <>
      <Stack.Screen options={{ title: 'Not found' }} />
      <View className="flex-1 items-center justify-center gap-4 bg-background px-6">
        <Text variant="h1">404</Text>
        <Text variant="muted">This screen does not exist.</Text>
        <Link href="/(app)/(tabs)/connect">
          <Text variant="large" className="text-primary">
            Go home
          </Text>
        </Link>
      </View>
    </>
  );
}
