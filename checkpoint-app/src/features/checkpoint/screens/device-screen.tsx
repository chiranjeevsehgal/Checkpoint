import { Play, Square } from 'lucide-react-native';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { Toggle } from '../components/toggle.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { useRecordingTimer } from '../hooks/useRecordingTimer.ts';
import type { DeviceStatus } from '../types.ts';

import { AppHeader } from '@/components/shared/app-header';
import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { EmptyState } from '@/components/shared/empty-state';
import { RefreshButton } from '@/components/shared/refresh-button';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { RangeSlider } from '@/components/ui/slider';
import { Text } from '@/components/ui/text';
import { useRefresh } from '@/lib/use-refresh';

interface DeviceControls {
  session: boolean;
  muted?: boolean;
  brightness?: number;
}

function StatCell({ value, label }: { value: number; label: string }) {
  return (
    <View className="flex-1 items-center bg-input-bg p-2.5">
      <Text className="font-display text-lg">{value}</Text>
      <Text variant="muted" className="text-[10px]">
        {label}
      </Text>
    </View>
  );
}

function RecordingDevDetails({
  recording,
  connected,
  vadState,
  sync,
}: {
  recording: boolean;
  connected: boolean;
  vadState: string;
  sync: boolean;
}) {
  return (
    <DeveloperDetails defaultExpanded>
      <DetailRow label="Recorder" value={recording ? 'Recording' : 'Idle'} />
      <DetailRow label="BLE" value={connected ? 'Connected' : 'Disconnected'} />
      <DetailRow label="VAD" value={vadState} />
      <DetailRow label="Sync" value={sync ? 'On' : 'Off'} />
    </DeveloperDetails>
  );
}

function recordingStatus(status: DeviceStatus | null): {
  recording: boolean;
  levelPct: number;
  vadState: string;
} {
  const recording = status?.recording ?? false;
  const levelPct = status ? Math.max(0, Math.min(100, ((status.levelDbfs + 60) / 60) * 100)) : 0;
  const vadState = status?.vadSpeech ? 'speech' : status?.vadActive ? 'active' : 'idle';
  return { recording, levelPct, vadState };
}

function RecordingCard({
  status,
  connected,
  developerMode,
  onToggle,
  onRefresh,
}: {
  status: DeviceStatus | null;
  connected: boolean;
  developerMode: boolean;
  onToggle: () => void;
  onRefresh: () => void;
}) {
  const { recording, levelPct, vadState } = recordingStatus(status);
  const timer = useRecordingTimer(recording);

  return (
    <Section title="Recording">
      <Card>
        <View className="flex-row items-center justify-between gap-2">
          <Text className="font-display text-[17px]">
            {recording ? 'Recording' : 'Ready to record'}
          </Text>
          <RefreshButton label="Refresh device state" onPress={onRefresh} />
        </View>
        {recording ? <Text className="font-mono text-[20px]">{timer}</Text> : null}
        <ProgressBar value={levelPct / 100} className="h-1.5" />
        <Text variant="muted" className="text-[11px]">
          Voice level · {vadState}
        </Text>
        <View className="flex-row gap-2">
          <StatCell value={status?.pending ?? 0} label="Pending" />
          <StatCell value={status?.chunks ?? 0} label="Chunks" />
          <StatCell value={status?.utterances ?? 0} label="Utterances" />
        </View>
        <Button variant={recording ? 'destructive' : 'default'} onPress={onToggle}>
          <Icon as={recording ? Square : Play} size={14} />
          <Text>{recording ? 'Stop recording' : 'Start recording'}</Text>
        </Button>
        {developerMode ? (
          <RecordingDevDetails
            recording={recording}
            connected={connected}
            vadState={vadState}
            sync={status?.sync ?? false}
          />
        ) : null}
      </Card>
    </Section>
  );
}

function LedCard({
  muted,
  brightness,
  onMutedChange,
  onBrightnessChange,
  onApply,
}: {
  muted: boolean;
  brightness: number;
  onMutedChange: (muted: boolean) => void;
  onBrightnessChange: (brightness: number) => void;
  onApply: () => void;
}) {
  return (
    <Section title="Status light">
      <Card>
        <Toggle
          label="Enabled"
          description="Light up the pendant status LED."
          value={!muted}
          onChange={(next) => onMutedChange(!next)}
        />
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
            onValueChange={onBrightnessChange}
          />
        </View>
        <Button variant="outline" onPress={onApply}>
          <Text>Apply light settings</Text>
        </Button>
      </Card>
    </Section>
  );
}

export function DeviceScreen() {
  const { connected, status, settings, toggleRec, refreshStatus, applyLed, applySync } =
    useCheckpoint();
  const [controls, setControls] = useState<DeviceControls>({ session: connected });
  const { refreshing, onRefresh } = useRefresh(refreshStatus);
  const active = controls.session === connected ? controls : { session: connected };
  const muted = active.muted ?? status?.muted ?? false;
  const brightness = active.brightness ?? status?.brightness ?? 30;
  const sync = status?.sync ?? false;

  const updateControls = (patch: Partial<Omit<DeviceControls, 'session'>>) => {
    setControls({ ...active, ...patch, session: connected });
  };

  if (!connected) {
    return (
      <CheckpointScreen>
        <AppHeader title="Device" subtitle="BLE remote control" />
        <EmptyState title="Not connected" hint="Connect to a pendant to control it." />
      </CheckpointScreen>
    );
  }

  return (
    <CheckpointScreen>
      <AppHeader title="Device" subtitle="BLE remote control" />
      <ScrollView
        className="flex-1"
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 24, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <RecordingCard
          status={status}
          connected={connected}
          developerMode={settings.developerMode}
          onToggle={() => void toggleRec()}
          onRefresh={() => void refreshStatus()}
        />
        <LedCard
          muted={muted}
          brightness={brightness}
          onMutedChange={(next) => updateControls({ muted: next })}
          onBrightnessChange={(value) => updateControls({ brightness: value })}
          onApply={() => void applyLed(muted, brightness)}
        />
        <Section title="Auto-sync">
          <Card>
            <Toggle
              label="Automatically transfer new recordings"
              description="Let the pendant send new files as soon as they are ready."
              value={sync}
              onChange={(next) => void applySync(next)}
            />
          </Card>
        </Section>
      </ScrollView>
    </CheckpointScreen>
  );
}
