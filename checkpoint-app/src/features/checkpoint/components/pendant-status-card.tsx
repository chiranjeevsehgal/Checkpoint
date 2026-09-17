import { Play, Square } from 'lucide-react-native';
import { useEffect, useMemo } from 'react';
import { Animated, Pressable, View } from 'react-native';

import { DEVICE_NAME } from '../config.ts';
import { connectionActivity, type ActivityTone } from '../connectionView.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { linkView } from '../linkView.ts';
import { isInProgress } from '../transferStore.ts';

import { SignalMeter } from './signal-meter.tsx';
import { Toggle } from './toggle.tsx';

import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { PendantLogo } from '@/components/shared/pendant-logo';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import { cn } from '@/lib/utils';

const ACTIVITY_DOT: Record<ActivityTone, string> = {
  recording: 'bg-primary',
  capturing: 'bg-primary',
  syncing: 'bg-warning',
  connected: 'bg-success',
  disconnected: 'bg-muted-foreground',
};

export function PendantStatusCard({ onManage }: { onManage: () => void }) {
  const {
    connected,
    busy,
    linkState,
    deviceId,
    enrolled,
    autoConnecting,
    deviceName,
    status,
    transfers,
    rssi,
    settings,
    toggleRec,
    stopConnection,
    updateSettings,
  } = useCheckpoint();
  const view = linkView(linkState, deviceName.trim() || DEVICE_NAME);
  const reduceMotion = useReducedMotion();
  const pulse = useMemo(() => new Animated.Value(1), []);
  const livePulse = useMemo(() => new Animated.Value(1), []);

  useEffect(() => {
    if (!busy || reduceMotion) {
      pulse.setValue(1);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, { toValue: 0.45, duration: 550, useNativeDriver: true }),
        Animated.timing(pulse, { toValue: 1, duration: 550, useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [busy, pulse, reduceMotion]);

  const syncing = transfers.filter(isInProgress).length;
  const activity = connectionActivity({
    connected,
    recording: status?.recording ?? false,
    vadActive: status?.vadActive ?? false,
    syncing,
  });
  const activityLive = activity.tone === 'recording' || activity.tone === 'capturing';

  useEffect(() => {
    if (!activityLive || reduceMotion) {
      livePulse.setValue(1);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(livePulse, { toValue: 0.25, duration: 700, useNativeDriver: true }),
        Animated.timing(livePulse, { toValue: 1, duration: 700, useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [activityLive, livePulse, reduceMotion]);

  const recording = status?.recording ?? false;
  const connecting = autoConnecting || linkState === 'reconnecting';

  return (
    <Section title="Pendant">
      <Card>
        <View className="flex-row items-center gap-3.5">
          <Animated.View
            style={{ opacity: busy ? pulse : 1 }}
            className="h-[52px] w-[52px] flex-none items-center justify-center bg-input-bg"
          >
            <PendantLogo height={30} dotColor={connected ? '#03fc84' : '#fc2003'} />
          </Animated.View>
          <View className="flex-1 gap-0.5">
            <Text className="font-display text-[17px]">{view.label}</Text>
            <Text variant="muted">{view.sub}</Text>
          </View>
        </View>

        {connected ? (
          <View className="flex-row items-center justify-between gap-2">
            <View className="flex-row items-center gap-1.5">
              <Animated.View
                style={{ opacity: activityLive ? livePulse : 1 }}
                className={cn('h-2 w-2', ACTIVITY_DOT[activity.tone])}
              />
              <Text variant="muted" className="text-[11px]">
                {activity.label}
              </Text>
            </View>
            {rssi !== null ? <SignalMeter dbm={rssi} /> : null}
          </View>
        ) : null}

        {connecting ? (
          <View className="flex-row items-center justify-between gap-2">
            <Text variant="muted" className="text-[12px]">
              {autoConnecting ? 'Auto-connecting…' : 'Reconnecting…'}
            </Text>
            <Pressable
              onPress={() => void stopConnection()}
              accessibilityRole="button"
              className="active:opacity-60"
            >
              <Text className="font-display text-[12px] text-primary-text">Stop</Text>
            </Pressable>
          </View>
        ) : null}

        {connected ? (
          <Button variant={recording ? 'destructive' : 'default'} onPress={() => void toggleRec()}>
            <Icon as={recording ? Square : Play} size={14} />
            <Text>{recording ? 'Stop recording' : 'Start recording'}</Text>
          </Button>
        ) : null}

        <Toggle
          label="Auto-connect & sync"
          description="Discover, connect and sync your pendant automatically."
          value={settings.autoSyncEnabled}
          onChange={(next) => void updateSettings({ ...settings, autoSyncEnabled: next })}
        />

        {enrolled ? (
          <Button variant="outline" onPress={onManage}>
            <Text>Manage device</Text>
          </Button>
        ) : null}

        {settings.developerMode ? (
          <DeveloperDetails defaultExpanded>
            <DetailRow label="Device ID" value={deviceId ?? '—'} />
            <DetailRow label="Claim" value={enrolled ? 'Linked' : 'Not linked'} />
            <DetailRow label="Link state" value={linkState} />
          </DeveloperDetails>
        ) : null}
      </Card>
    </Section>
  );
}
