import { Bluetooth, ScanLine } from 'lucide-react-native';
import { useCallback, useEffect, useMemo } from 'react';
import { Animated, ScrollView, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { BatteryOptimizationCard } from '@/components/shared/battery-optimization-card';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardKicker } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { useRefresh } from '@/lib/use-refresh';
import { useToast } from '@/providers/toast-provider';

import { parseClaimHex } from '../claim.ts';
import { LogView } from '../components/log-view.tsx';
import { Toggle } from '../components/toggle.tsx';
import { DEVICE_NAME } from '../config.ts';
import { connectionActivity, formatFingerprint } from '../connectionView.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { useClaimScanner } from '../hooks/useClaimScanner.ts';
import { linkView } from '../linkView.ts';
import { isInProgress } from '../transferStore.ts';

export function ConnectScreen() {
  const {
    connected,
    busy,
    linkState,
    deviceId,
    deviceName,
    setDeviceName,
    claimText,
    setClaimText,
    status,
    transfers,
    logs,
    settings,
    connect,
    disconnect,
    updateSettings,
    clearLogs,
    refreshStatus,
    refreshStorage,
    needsSettings,
    openAppSettings,
  } = useCheckpoint();
  const { showToast } = useToast();

  const view = linkView(linkState, deviceName.trim() || DEVICE_NAME);
  const pulse = useMemo(() => new Animated.Value(1), []);
  const livePulse = useMemo(() => new Animated.Value(1), []);

  const handleScannedClaim = useCallback(
    (data: string) => {
      const trimmed = data.trim();
      try {
        parseClaimHex(trimmed);
        setClaimText(trimmed);
        showToast('Claim key scanned.');
      } catch {
        showToast('Not a valid claim QR.');
      }
    },
    [setClaimText, showToast]
  );
  const { available: scannerAvailable, start: startScanner } = useClaimScanner(handleScannedClaim);

  const refresh = useCallback(async () => {
    if (!connected) return;
    await refreshStatus();
    await refreshStorage();
  }, [connected, refreshStatus, refreshStorage]);
  const { refreshing, onRefresh } = useRefresh(refresh);

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

  const syncing = transfers.filter(isInProgress).length;
  const activity = connectionActivity({
    connected,
    recording: status?.recording ?? false,
    vadActive: status?.vadActive ?? false,
    syncing,
  });
  const fingerprint = formatFingerprint(deviceId);

  useEffect(() => {
    if (activity.tone !== 'live') {
      livePulse.setValue(1);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(livePulse, { toValue: 0.25, duration: 700, useNativeDriver: true }),
        Animated.timing(livePulse, { toValue: 1, duration: 700, useNativeDriver: true }),
      ])
    );
    loop.start();
    return () => loop.stop();
  }, [activity.tone, livePulse]);

  const heroActive = connected || busy;
  const showSettings = view.openSettings || needsSettings;

  return (
    <Screen>
      <AppHeader title="Connect" subtitle={view.label} />
      <ScrollView
        className="flex-1"
        nestedScrollEnabled
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
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

          {connected ? (
            <View className="flex-row items-center justify-between gap-2">
              <Text className="font-mono text-primary text-[12px] tracking-[0.15em]">
                {fingerprint || '—'}
              </Text>
              <View className="flex-row items-center gap-1.5">
                <Animated.View
                  style={{ opacity: activity.tone === 'live' ? livePulse : 1 }}
                  className={cn(
                    'h-2 w-2',
                    activity.tone === 'live' ? 'bg-primary' : 'bg-muted-foreground'
                  )}
                />
                <Text variant="muted" className="text-[11px]">
                  {activity.label}
                </Text>
              </View>
            </View>
          ) : null}

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
            <View className="flex-row gap-2">
              <Input
                className="flex-1"
                value={claimText}
                onChangeText={setClaimText}
                editable={!connected && !busy}
                autoCapitalize="none"
                autoCorrect={false}
                secureTextEntry
                placeholder="Hold the pendant button 5s to enroll"
              />
              {scannerAvailable ? (
                <Button
                  variant="outline"
                  size="icon"
                  className="h-9 w-9"
                  disabled={connected || busy}
                  onPress={startScanner}
                  accessibilityLabel="Scan claim QR"
                >
                  <Icon as={ScanLine} size={16} />
                </Button>
              ) : null}
            </View>
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
