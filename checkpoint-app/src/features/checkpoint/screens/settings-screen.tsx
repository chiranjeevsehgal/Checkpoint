import Constants from 'expo-constants';
import { useRouter } from 'expo-router';
import { useCallback, useMemo, useState } from 'react';
import { ScrollView, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { EraseCard } from '../components/erase-card.tsx';
import { Toggle } from '../components/toggle.tsx';
import { useBluetoothState } from '../hooks/useBluetoothState.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { classifyLog, formatLogTime, isErrorLog } from '../logFilter.ts';
import type { CheckpointSettings } from '../settings.ts';
import { transferView } from '../transferView.ts';

import { AppHeader } from '@/components/shared/app-header';
import { BatteryOptimizationCard } from '@/components/shared/battery-optimization-card';
import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { RangeSlider } from '@/components/ui/slider';
import { Text } from '@/components/ui/text';
import { env } from '@/lib/env';
import { cn } from '@/lib/utils';
import { useAppTheme } from '@/providers/theme-provider';

type Apply = (patch: Partial<CheckpointSettings>) => void;

interface ValueSliderProps {
  label: string;
  format: (value: number) => string;
  min: number;
  max: number;
  step: number;
  value: number;
  hint?: string;
  onChange: (value: number) => void;
}

function thresholdHint(value: number): string {
  if (value < 0.35) return 'More sensitive — catches quieter speech, more false positives.';
  if (value > 0.65) return 'Fewer false positives — may miss quiet speech.';
  return 'Balanced sensitivity.';
}

function ValueSlider({ label, format, min, max, step, value, hint, onChange }: ValueSliderProps) {
  const [draft, setDraft] = useState(value);
  const [trackedValue, setTrackedValue] = useState(value);

  if (value !== trackedValue) {
    setTrackedValue(value);
    setDraft(value);
  }

  return (
    <View className="gap-1">
      <View className="flex-row justify-between">
        <Text className="text-[12px]">{label}</Text>
        <Text className="font-mono text-[12px]">{format(draft)}</Text>
      </View>
      <RangeSlider
        min={min}
        max={max}
        step={step}
        value={draft}
        accessibilityLabel={label}
        onValueChange={setDraft}
        onSlidingComplete={onChange}
      />
      {hint ? (
        <Text variant="muted" className="text-[11px]">
          {hint}
        </Text>
      ) : null}
    </View>
  );
}

function SpeechSection({
  settings,
  apply,
  applySilent,
}: {
  settings: CheckpointSettings;
  apply: Apply;
  applySilent: Apply;
}) {
  return (
    <Section title="Speech">
      <Card>
        <Toggle
          label="Voice activity detection"
          description="Only process audio that contains speech."
          value={settings.vadEnabled}
          onChange={(next) => apply({ vadEnabled: next })}
        />
        <ValueSlider
          label="VAD threshold"
          format={(value) => value.toFixed(2)}
          min={0}
          max={1}
          step={0.01}
          value={settings.vadThreshold}
          hint={thresholdHint(settings.vadThreshold)}
          onChange={(value) => applySilent({ vadThreshold: Number(value.toFixed(2)) })}
        />
        <ValueSlider
          label="Minimum speech duration"
          format={(value) => `${value.toFixed(1)}s`}
          min={0.1}
          max={3}
          step={0.1}
          value={settings.minSpeechS}
          hint={`Clips shorter than ${settings.minSpeechS.toFixed(1)}s are dropped as noise.`}
          onChange={(value) => applySilent({ minSpeechS: Number(value.toFixed(1)) })}
        />
      </Card>
    </Section>
  );
}

function TransferSection({
  settings,
  apply,
  applySilent,
}: {
  settings: CheckpointSettings;
  apply: Apply;
  applySilent: Apply;
}) {
  return (
    <Section title="Sync">
      <Card>
        <Toggle
          label="Auto-connect & sync"
          description="Discover, connect and sync your pendant automatically."
          value={settings.autoSyncEnabled}
          onChange={(next) => apply({ autoSyncEnabled: next })}
        />
        <Toggle
          label="Upload recordings"
          description="Send speech clips to the server for transcription."
          value={settings.ingestEnabled}
          onChange={(next) => apply({ ingestEnabled: next })}
        />
        <ValueSlider
          label="Keep completed transfers"
          format={(value) => `${Math.round(value)}h`}
          min={1}
          max={168}
          step={1}
          value={settings.retentionHours}
          hint={`Completed transfers and their local audio are cleaned up after ${settings.retentionHours}h. Pending and failed audio is never deleted.`}
          onChange={(value) => applySilent({ retentionHours: Math.round(value) })}
        />
      </Card>
      <BatteryOptimizationCard />
    </Section>
  );
}

function BackendSection({
  settings,
  onSave,
  onTest,
  testing,
  probe,
}: {
  settings: CheckpointSettings;
  onSave: (next: { serverUrl: string }) => void;
  onTest: () => void;
  testing: boolean;
  probe: { ok: boolean; latencyMs: number; at: number } | null;
}) {
  const [serverUrl, setServerUrl] = useState(settings.serverUrl);

  const save = () => {
    onSave({ serverUrl: serverUrl.trim() });
  };

  return (
    <Section title="Connection">
      <Card>
        <View className="gap-1">
          <Text className="text-[11px] text-subtle-foreground">
            Server URL (LAN IP for on-device testing)
          </Text>
          <Input
            value={serverUrl}
            onChangeText={setServerUrl}
            autoCapitalize="none"
            autoCorrect={false}
            className="font-mono text-[13px]"
          />
        </View>
        <View className="flex-row gap-2">
          <Button variant="outline" className="flex-1" onPress={save}>
            <Text>Save server</Text>
          </Button>
          <Button variant="outline" className="flex-1" disabled={testing} onPress={onTest}>
            <Text>{testing ? 'Testing…' : 'Test connection'}</Text>
          </Button>
        </View>
        {probe ? (
          <View className="gap-0.5">
            <Text className={cn('text-[12px]', probe.ok ? 'text-success' : 'text-destructive')}>
              {probe.ok ? `✓ Server reachable · ${probe.latencyMs} ms` : '✕ Connection failed'}
            </Text>
            <Text variant="muted" className="text-[11px]">
              Last checked {formatLogTime(probe.at)}
            </Text>
          </View>
        ) : null}
      </Card>
    </Section>
  );
}

interface Diagnostics {
  appVersion: string;
  bluetooth: string;
  pendantId: string;
  claim: string;
  lastServerError: string;
  lastTransferError: string;
}

function DeveloperSection({
  settings,
  apply,
  diagnostics,
  onOpenLog,
}: {
  settings: CheckpointSettings;
  apply: Apply;
  diagnostics: Diagnostics;
  onOpenLog: () => void;
}) {
  return (
    <Section title="Developer">
      <Card>
        <Toggle
          label="Developer information"
          description="Show raw IDs, filenames and pipeline details across the app."
          value={settings.developerMode}
          onChange={(next) => apply({ developerMode: next })}
        />
        <DeveloperDetails defaultExpanded>
          <DetailRow label="App version" value={diagnostics.appVersion} />
          <DetailRow label="Bluetooth" value={diagnostics.bluetooth} />
          <DetailRow label="Pendant ID" value={diagnostics.pendantId} />
          <DetailRow label="Claim" value={diagnostics.claim} />
          <DetailRow label="Last server error" value={diagnostics.lastServerError} />
          <DetailRow label="Last transfer error" value={diagnostics.lastTransferError} />
        </DeveloperDetails>
        <Button variant="outline" onPress={onOpenLog}>
          <Text>Open debug log</Text>
        </Button>
      </Card>
    </Section>
  );
}

function AppearanceSection() {
  const { scheme, setTheme } = useAppTheme();

  return (
    <Section title="Appearance">
      <Card>
        <Toggle
          label="Dark mode"
          description="Use the dark theme across the app."
          value={scheme === 'dark'}
          onChange={(next) => setTheme(next ? 'dark' : 'light')}
        />
      </Card>
    </Section>
  );
}

export function CheckpointSettingsScreen() {
  const { settings, updateSettings, testConnection, logs, transfers, deviceId, enrolled } =
    useCheckpoint();
  const bluetooth = useBluetoothState();
  const router = useRouter();
  const [probe, setProbe] = useState<{ ok: boolean; latencyMs: number; at: number } | null>(null);
  const [testing, setTesting] = useState(false);

  const apply = useCallback<Apply>(
    (patch) => {
      void updateSettings({ ...settings, ...patch });
    },
    [settings, updateSettings],
  );

  const applySilent = useCallback<Apply>(
    (patch) => {
      void updateSettings({ ...settings, ...patch }, { silent: true });
    },
    [settings, updateSettings],
  );

  const runTest = useCallback(async () => {
    setTesting(true);
    try {
      setProbe(await testConnection());
    } finally {
      setTesting(false);
    }
  }, [testConnection]);

  const onSaveServer = useCallback(
    (next: { serverUrl: string }) => {
      void updateSettings({ ...settings, ...next });
    },
    [settings, updateSettings],
  );

  const diagnostics = useMemo<Diagnostics>(() => {
    const lastServer = [...logs]
      .reverse()
      .find((entry) => classifyLog(entry.text) === 'server' && isErrorLog(entry.text));
    const lastTransfer = transfers.find((record) => record.outcome === 'failed');
    return {
      appVersion: Constants.expoConfig?.version ?? '—',
      bluetooth: bluetooth ?? '—',
      pendantId: deviceId ?? '—',
      claim: enrolled ? 'Linked' : 'Not linked',
      lastServerError: lastServer?.text ?? '—',
      lastTransferError: lastTransfer ? transferView(lastTransfer).ingestLabel : '—',
    };
  }, [bluetooth, deviceId, enrolled, logs, transfers]);

  return (
    <CheckpointScreen>
      <AppHeader title="Settings" subtitle="Speech, transfer and server" />
      <ScrollView
        className="flex-1"
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ gap: 24, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <SpeechSection settings={settings} apply={apply} applySilent={applySilent} />
        <TransferSection settings={settings} apply={apply} applySilent={applySilent} />
        <BackendSection
          settings={settings}
          onSave={onSaveServer}
          onTest={() => void runTest()}
          testing={testing}
          probe={probe}
        />
        <AppearanceSection />
        <Section title="Account">
          <Card>
            <Button variant="outline" onPress={() => router.push('/account')}>
              <Text>Account & security</Text>
            </Button>
          </Card>
        </Section>
        <Section title="Danger zone">
          <EraseCard />
        </Section>
        {env.devBuild ? (
          <DeveloperSection
            settings={settings}
            apply={apply}
            diagnostics={diagnostics}
            onOpenLog={() => router.push('/debug-log')}
          />
        ) : null}
      </ScrollView>
    </CheckpointScreen>
  );
}
