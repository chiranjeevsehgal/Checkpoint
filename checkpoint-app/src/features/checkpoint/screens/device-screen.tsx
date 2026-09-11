import { Play, RefreshCw, Square } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { RangeSlider } from '@/components/ui/slider';
import { Text } from '@/components/ui/text';

import { Toggle } from '../components/toggle.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';

interface DeviceControls {
  session: boolean;
  muted?: boolean;
  brightness?: number;
}

function StatCell({ value, label }: { value: number; label: string }) {
  return (
    <View className="bg-input-bg flex-1 items-center p-2.5">
      <Text className="font-display text-lg">{value}</Text>
      <Text variant="muted" className="text-[10px]">
        {label}
      </Text>
    </View>
  );
}

export function DeviceScreen() {
  const { connected, status, toggleRec, refreshStatus, applyLed, applySync } = useCheckpoint();
  const [controls, setControls] = useState<DeviceControls>({ session: connected });
  const active = controls.session === connected ? controls : { session: connected };
  const muted = active.muted ?? status?.muted ?? false;
  const brightness = active.brightness ?? status?.brightness ?? 30;
  const sync = status?.sync ?? false;

  const updateControls = (patch: Partial<Omit<DeviceControls, 'session'>>) => {
    setControls({ ...active, ...patch, session: connected });
  };

  const vadState = status?.vadSpeech ? 'speech' : status?.vadActive ? 'active' : 'idle';
  const levelPct = status ? Math.max(0, Math.min(100, ((status.levelDbfs + 60) / 60) * 100)) : 0;

  return (
    <Screen>
      <AppHeader title="Device" subtitle="BLE remote control" />
      {!connected ? (
        <EmptyState title="Not connected" hint="Connect to a pendant to control it." />
      ) : (
        <ScrollView
          className="flex-1"
          contentContainerStyle={{ gap: 14, paddingBottom: 24 }}
          showsVerticalScrollIndicator={false}
        >
          <Card>
            <View className="flex-row items-center justify-between">
              <CardKicker>Recording</CardKicker>
              <Button variant="ghost" size="sm" onPress={() => void refreshStatus()}>
                <Icon as={RefreshCw} size={14} />
                <Text>Refresh</Text>
              </Button>
            </View>
            <ProgressBar value={levelPct / 100} className="h-1.5" />
            <Text variant="muted" className="text-[11px]">
              Voice level · {vadState}
            </Text>
            <View className="flex-row gap-2">
              <StatCell value={status?.pending ?? 0} label="Pending" />
              <StatCell value={status?.chunks ?? 0} label="Chunks" />
              <StatCell value={status?.utterances ?? 0} label="Utterances" />
            </View>
            <Button
              className={status?.recording ? 'bg-primary' : 'bg-foreground'}
              onPress={() => void toggleRec()}
            >
              <Icon as={status?.recording ? Square : Play} size={14} />
              <Text>{status?.recording ? 'Stop Rec' : 'Start Rec'}</Text>
            </Button>
          </Card>

          <Card>
            <CardKicker>LED</CardKicker>
            <Toggle label="LED muted" value={muted} onChange={(next) => updateControls({ muted: next })} />
            <View className="gap-1">
              <View className="flex-row justify-between">
                <Text className="text-[12px]">Brightness</Text>
                <Text className="font-mono text-[12px]">{brightness}</Text>
              </View>
              <RangeSlider
                min={5}
                max={255}
                step={1}
                value={brightness}
                onValueChange={(value) => updateControls({ brightness: value })}
              />
            </View>
            <Button variant="outline" onPress={() => void applyLed(muted, brightness)}>
              <Text>Apply LED</Text>
            </Button>
          </Card>

          <Card>
            <Toggle
              label="Auto-sync"
              description="Sync recordings automatically"
              value={sync}
              onChange={(next) => void applySync(next)}
            />
          </Card>
        </ScrollView>
      )}
    </Screen>
  );
}
