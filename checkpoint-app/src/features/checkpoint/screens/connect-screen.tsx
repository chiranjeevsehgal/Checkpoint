import { ScanLine } from 'lucide-react-native';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Animated, Pressable, ScrollView, View } from 'react-native';

import { parseClaimHex } from '../claim.ts';
import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { ManageDeviceSheet } from '../components/manage-device-sheet.tsx';
import { DEVICE_NAME } from '../config.ts';
import { connectionActivity, type ActivityTone } from '../connectionView.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { useClaimScanner } from '../hooks/useClaimScanner.ts';
import { linkView } from '../linkView.ts';
import { isInProgress } from '../transferStore.ts';

import { AppHeader } from '@/components/shared/app-header';
import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { PendantLogo } from '@/components/shared/pendant-logo';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import { useRefresh } from '@/lib/use-refresh';
import { cn } from '@/lib/utils';
import { useToast } from '@/providers/toast-provider';

const ACTIVITY_DOT: Record<ActivityTone, string> = {
  recording: 'bg-primary',
  capturing: 'bg-primary',
  syncing: 'bg-warning',
  connected: 'bg-success',
  disconnected: 'bg-muted-foreground',
};

export function ConnectScreen() {
  const {
    connected,
    busy,
    linkState,
    deviceId,
    enrolled,
    autoConnecting,
    deviceName,
    claimText,
    setClaimText,
    status,
    transfers,
    settings,
    connect,
    disconnect,
    stopConnection,
    refreshStatus,
    refreshStorage,
    needsSettings,
    openAppSettings,
  } = useCheckpoint();
  const { showToast } = useToast();
  const [setupOpen, setSetupOpen] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);

  const view = linkView(linkState, deviceName.trim() || DEVICE_NAME);
  const reduceMotion = useReducedMotion();
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
    [setClaimText, showToast],
  );
  const handleScanError = useCallback(() => {
    showToast('Could not open the scanner.');
  }, [showToast]);
  const { available: scannerAvailable, start: startScanner } = useClaimScanner(
    handleScannedClaim,
    handleScanError,
  );

  const refresh = useCallback(async () => {
    if (!connected) return;
    await refreshStatus();
    await refreshStorage();
  }, [connected, refreshStatus, refreshStorage]);
  const { refreshing, onRefresh } = useRefresh(refresh);

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

  const showSettings = view.openSettings || needsSettings;
  const showEnrollment = setupOpen || !enrolled;
  const settingsLabel = linkState === 'needs permission' ? 'Allow Bluetooth' : 'Open Settings';
  const claimValid = useMemo(() => {
    try {
      parseClaimHex(claimText.trim());
      return true;
    } catch {
      return false;
    }
  }, [claimText]);

  return (
    <CheckpointScreen>
      <AppHeader title="Connect" subtitle={view.label} />
      <ScrollView
        className="flex-1"
        nestedScrollEnabled
        keyboardShouldPersistTaps="handled"
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 24, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
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
              <View className="flex-row items-center gap-1.5">
                <Animated.View
                  style={{ opacity: activityLive ? livePulse : 1 }}
                  className={cn('h-2 w-2', ACTIVITY_DOT[activity.tone])}
                />
                <Text variant="muted" className="text-[11px]">
                  {activity.label}
                </Text>
              </View>
            ) : null}

            {autoConnecting ? (
              <View className="flex-row items-center justify-between gap-2">
                <Text variant="muted" className="text-[12px]">
                  Auto-connecting…
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

            {settings.developerMode ? (
              <DeveloperDetails defaultExpanded>
                <DetailRow label="Device ID" value={deviceId ?? '—'} />
                <DetailRow label="Claim" value={enrolled ? 'Linked' : 'Not linked'} />
                <DetailRow label="Link state" value={linkState} />
              </DeveloperDetails>
            ) : null}

            <View className="flex-row gap-2">
              {connected ? (
                <>
                  <Button variant="ghost" className="flex-1" onPress={() => void disconnect()}>
                    <Text>Disconnect</Text>
                  </Button>
                  <Button variant="outline" className="flex-1" onPress={() => setSheetOpen(true)}>
                    <Text>Manage device</Text>
                  </Button>
                </>
              ) : enrolled ? (
                <Button className="flex-1" disabled={busy} onPress={() => void connect()}>
                  <Text>{busy ? 'Connecting…' : 'Connect'}</Text>
                </Button>
              ) : null}
              {showSettings ? (
                <Button variant="outline" className="flex-1" onPress={() => void openAppSettings()}>
                  <Text>{settingsLabel}</Text>
                </Button>
              ) : null}
            </View>
          </Card>
        </Section>

        {showEnrollment ? (
          <Section title="Set up pendant">
            <Card>
              <Text variant="muted" className="text-[12px]">
                Hold the pendant button for 5 seconds to enter pairing mode.
              </Text>
              <View className="gap-1">
                <Text className="text-[11px] text-subtle-foreground">
                  Claim key / claim URI (64 hex)
                </Text>
                <View className="flex-row gap-2">
                  <Input
                    className="flex-1"
                    value={claimText}
                    onChangeText={(text) => {
                      void stopConnection();
                      setClaimText(text);
                    }}
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
                      onPress={() => {
                        void stopConnection();
                        startScanner();
                      }}
                      accessibilityLabel="Scan claim QR"
                    >
                      <Icon as={ScanLine} size={16} />
                    </Button>
                  ) : null}
                </View>
              </View>
              <Button disabled={busy || !claimValid} onPress={() => void connect()}>
                <Text>{busy ? 'Linking…' : 'Find & link pendant'}</Text>
              </Button>
              {settings.developerMode ? (
                <DeveloperDetails defaultExpanded>
                  <DetailRow label="BLE discovery" value={autoConnecting ? 'Active' : 'Idle'} />
                  <DetailRow label="Claim status" value={enrolled ? 'Linked' : 'Waiting'} />
                  <DetailRow label="Device" value={deviceName} />
                </DeveloperDetails>
              ) : null}
            </Card>
          </Section>
        ) : null}
      </ScrollView>

      <ManageDeviceSheet
        visible={sheetOpen}
        onClose={() => setSheetOpen(false)}
        onSetup={() => setSetupOpen(true)}
      />
    </CheckpointScreen>
  );
}
