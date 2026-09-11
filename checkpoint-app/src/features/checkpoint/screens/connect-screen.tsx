import { Bluetooth } from 'lucide-react-native';
import { useEffect, useMemo } from 'react';
import { Animated, ScrollView, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { BatteryOptimizationCard } from '@/components/shared/battery-optimization-card';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

import { LogView } from '../components/log-view.tsx';
import { Toggle } from '../components/toggle.tsx';
import { DEVICE_NAME } from '../config.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { linkView } from '../linkView.ts';

export function ConnectScreen() {
  const {
    connected,
    busy,
    linkState,
    deviceName,
    setDeviceName,
    claimText,
    setClaimText,
    logs,
    settings,
    connect,
    disconnect,
    updateSettings,
    clearLogs,
    needsSettings,
    openAppSettings,
  } = useCheckpoint();

  const view = linkView(linkState, deviceName.trim() || DEVICE_NAME);
  const pulse = useMemo(() => new Animated.Value(1), []);

  useEffect(() => {
    if (!busy) {
      pulse.setValue(1);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, { toValue: 0.45, duration: 550, useNativeDriver: true }),
        Animated.timing(pulse, { toValue: 1, duration: 550, useNativeDriver: true }),
      ])
    );
    loop.start();
    return () => loop.stop();
  }, [busy, pulse]);

  const heroActive = connected || busy;
  const showSettings = view.openSettings || needsSettings;

  return (
    <Screen>
      <AppHeader title="Connect" subtitle={view.label} />
      <ScrollView
        contentContainerStyle={{ gap: 16, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <Card>
          <CardKicker>Pendant</CardKicker>
          <View className="flex-row items-center gap-3.5">
            <Animated.View
              style={{ opacity: busy ? pulse : 1 }}
              className={cn(
                'h-[52px] w-[52px] flex-none items-center justify-center',
                heroActive ? 'bg-primary' : 'bg-input-bg'
              )}
            >
              <Icon
                as={Bluetooth}
                size={26}
                className={heroActive ? 'text-primary-foreground' : 'text-muted-foreground'}
              />
            </Animated.View>
            <View className="flex-1 gap-0.5">
              <Text className="font-display text-[17px]">{view.label}</Text>
              <Text variant="muted">{view.sub}</Text>
            </View>
          </View>

          <View className="bg-divider h-0.5" />

          <View className="gap-1">
            <Text className="text-[11px] opacity-65">Device name or address</Text>
            <Input
              value={deviceName}
              onChangeText={setDeviceName}
              editable={!connected && !busy}
              autoCapitalize="none"
              placeholder="Checkpoint"
            />
          </View>
          <View className="gap-1">
            <Text className="text-[11px] opacity-65">Claim key (enroll only, 64 hex or claim URI)</Text>
            <Input
              value={claimText}
              onChangeText={setClaimText}
              editable={!connected && !busy}
              autoCapitalize="none"
              autoCorrect={false}
              secureTextEntry
              placeholder="Hold the pendant button 5s to enroll"
            />
            <Text variant="muted" className="text-[11px]">
              Hold the pendant button 5s, then connect.
            </Text>
          </View>

          <View className="flex-row gap-2">
            {connected ? (
              <Button variant="outline" className="flex-1" onPress={() => void disconnect()}>
                <Text>Disconnect</Text>
              </Button>
            ) : (
              <Button className="flex-1" disabled={busy} onPress={() => void connect()}>
                <Text>{busy ? 'Connecting…' : 'Connect'}</Text>
              </Button>
            )}
            {showSettings ? (
              <Button variant="outline" className="flex-1" onPress={() => void openAppSettings()}>
                <Text>Open Settings</Text>
              </Button>
            ) : null}
          </View>
        </Card>

        <Card>
          <CardKicker>Options</CardKicker>
          <Toggle
            label="Upload to ingestion"
            description="Send speech clips to the server for transcription."
            value={settings.ingestEnabled}
            onChange={(next) => void updateSettings({ ...settings, ingestEnabled: next })}
          />
          <Toggle
            label="Voice-activity gate"
            description="Filter out silence before it's uploaded."
            value={settings.vadEnabled}
            onChange={(next) => void updateSettings({ ...settings, vadEnabled: next })}
          />
          <Toggle
            label="Keep files on device"
            description="Don't delete recordings from the pendant after upload."
            value={settings.keepFiles}
            onChange={(next) => void updateSettings({ ...settings, keepFiles: next })}
          />
        </Card>

        <BatteryOptimizationCard />

        <LogView logs={logs} onClear={clearLogs} />
      </ScrollView>
    </Screen>
  );
}
