import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { BatteryOptimizationCard } from '@/components/shared/battery-optimization-card';
import { Collapsible } from '@/components/shared/collapsible';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { RangeSlider } from '@/components/ui/slider';
import { Text } from '@/components/ui/text';

import { Toggle } from '../components/toggle.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { isValidUserId } from '../parsers.ts';
import type { CheckpointSettings } from '../settings.ts';

function thresholdHint(value: number): string {
  if (value < 0.35) return 'More sensitive — catches quieter speech, more false positives.';
  if (value > 0.65) return 'Fewer false positives — may miss quiet speech.';
  return 'Balanced sensitivity.';
}

export function CheckpointSettingsScreen() {
  const { settings, updateSettings, testConnection } = useCheckpoint();
  const [draft, setDraft] = useState<CheckpointSettings>(settings);
  const [userIdError, setUserIdError] = useState<string | null>(null);

  const set = <K extends keyof CheckpointSettings>(key: K, value: CheckpointSettings[K]) => {
    if (key === 'userId') setUserIdError(null);
    setDraft((prev) => ({ ...prev, [key]: value }));
  };

  const save = () => {
    if (!isValidUserId(draft.userId)) {
      setUserIdError('Enter a valid user ID (UUID like aaaaaaaa-…).');
      return;
    }
    setUserIdError(null);
    void updateSettings(draft).catch(() => setUserIdError('Save failed — try again.'));
  };

  return (
    <Screen>
      <AppHeader title="Settings" subtitle="Server, identity and VAD" />
      <ScrollView
        contentContainerStyle={{ gap: 14, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <Card>
          <CardKicker>Voice gate</CardKicker>
          <View className="gap-1">
            <View className="flex-row justify-between">
              <Text className="text-[12px]">VAD threshold</Text>
              <Text className="font-mono text-[12px]">{draft.vadThreshold.toFixed(2)}</Text>
            </View>
            <RangeSlider
              min={0}
              max={1}
              step={0.01}
              value={draft.vadThreshold}
              onValueChange={(value) => set('vadThreshold', Number(value.toFixed(2)))}
            />
            <Text variant="muted" className="text-[11px]">
              {thresholdHint(draft.vadThreshold)}
            </Text>
          </View>
          <View className="gap-1">
            <View className="flex-row justify-between">
              <Text className="text-[12px]">Minimum speech</Text>
              <Text className="font-mono text-[12px]">{draft.minSpeechS.toFixed(1)}s</Text>
            </View>
            <RangeSlider
              min={0.1}
              max={3}
              step={0.1}
              value={draft.minSpeechS}
              onValueChange={(value) => set('minSpeechS', Number(value.toFixed(1)))}
            />
            <Text variant="muted" className="text-[11px]">
              Clips shorter than {draft.minSpeechS.toFixed(1)}s are dropped as noise.
            </Text>
          </View>
        </Card>

        <Card>
          <CardKicker>Synchronization</CardKicker>
          <Toggle
            label="Sync automatically"
            description="Connect and sync recordings without opening the app."
            value={draft.autoSyncEnabled}
            onChange={(next) => set('autoSyncEnabled', next)}
          />
          <View className="gap-1">
            <View className="flex-row justify-between">
              <Text className="text-[12px]">Keep completed transfers</Text>
              <Text className="font-mono text-[12px]">{draft.retentionHours}h</Text>
            </View>
            <RangeSlider
              min={1}
              max={168}
              step={1}
              value={draft.retentionHours}
              onValueChange={(value) => set('retentionHours', Math.round(value))}
            />
            <Text variant="muted" className="text-[11px]">
              Completed transfers and their local audio are cleaned up after {draft.retentionHours}
              h. Pending and failed audio is never deleted.
            </Text>
          </View>
        </Card>

        <BatteryOptimizationCard />

        <Button variant="outline" onPress={() => void testConnection()}>
          <Text>Test server connection</Text>
        </Button>

        <Collapsible title="Advanced — developer">
          <View className="gap-1">
            <Text className="text-[11px] opacity-65">Server URL (LAN IP for on-device testing)</Text>
            <Input
              value={draft.serverUrl}
              onChangeText={(text) => set('serverUrl', text)}
              autoCapitalize="none"
              autoCorrect={false}
              className="font-mono text-[13px]"
            />
          </View>
          <View className="gap-1">
            <Text className="text-[11px] opacity-65">User ID (Bearer token)</Text>
            <Input
              value={draft.userId}
              onChangeText={(text) => set('userId', text)}
              autoCapitalize="none"
              autoCorrect={false}
              placeholder="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
              className="font-mono text-[13px]"
            />
            {userIdError ? <Text className="text-primary text-[11px]">{userIdError}</Text> : null}
          </View>
        </Collapsible>

        <Button onPress={save} className="h-11">
          <Text>Save settings</Text>
        </Button>
      </ScrollView>
    </Screen>
  );
}
