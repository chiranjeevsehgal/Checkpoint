import { View } from 'react-native';

import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { useRecordingTimer } from '../hooks/useRecordingTimer.ts';
import type { DeviceStatus } from '../types.ts';

import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { Section } from '@/components/shared/section';
import { Card } from '@/components/ui/card';
import { ProgressBar } from '@/components/ui/progress-bar';
import { Text } from '@/components/ui/text';

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

export function RecordingSection() {
  const { connected, status, settings } = useCheckpoint();
  const { recording, levelPct, vadState } = recordingStatus(status);
  const timer = useRecordingTimer(recording);

  if (!connected) return null;

  return (
    <Section title="Recording">
      <Card>
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
        {settings.developerMode ? (
          <DeveloperDetails defaultExpanded>
            <DetailRow label="Recorder" value={recording ? 'Recording' : 'Idle'} />
            <DetailRow label="BLE" value="Connected" />
            <DetailRow label="VAD" value={vadState} />
            <DetailRow label="Sync" value={status?.sync ? 'On' : 'Off'} />
          </DeveloperDetails>
        ) : null}
      </Card>
    </Section>
  );
}
