import Constants from 'expo-constants';
import { openSettings } from 'expo-linking';
import { BatteryCharging } from 'lucide-react-native';
import { Linking, Platform } from 'react-native';

import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import { useBatteryOptimization } from '@/hooks/useBatteryOptimization';

function androidPackage(): string {
  return Constants.expoConfig?.android?.package ?? 'com.checkpoint.pendant';
}

async function openBatterySettings(): Promise<void> {
  if (Platform.OS === 'android') {
    try {
      await Linking.sendIntent('android.settings.IGNORE_BATTERY_OPTIMIZATION_SETTINGS');
      return;
    } catch {
      /* fall through to app details */
    }
    try {
      await Linking.sendIntent('android.settings.APPLICATION_DETAILS_SETTINGS', [
        { key: 'package', value: androidPackage() },
      ]);
      return;
    } catch {
      /* fall through to the generic settings screen */
    }
  }
  await openSettings();
}

export function BatteryOptimizationCard() {
  const restricted = useBatteryOptimization();
  if (Platform.OS !== 'android' || restricted !== true) return null;

  return (
    <Card>
      <CardKicker>Background sync</CardKicker>
      <Text className="text-[12px] text-muted-foreground">
        Android may pause Bluetooth and uploads when the screen is off. Allow Checkpoint to run
        without battery optimization so recordings keep syncing in the background.
      </Text>
      <Button variant="outline" onPress={() => void openBatterySettings()}>
        <Icon as={BatteryCharging} size={15} />
        <Text>Open battery settings</Text>
      </Button>
    </Card>
  );
}
