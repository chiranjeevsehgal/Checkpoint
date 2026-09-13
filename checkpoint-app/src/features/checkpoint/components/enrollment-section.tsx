import { ScanLine } from 'lucide-react-native';
import { useCallback, useMemo } from 'react';
import { View } from 'react-native';

import { parseClaimHex } from '../claim.ts';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { useClaimScanner } from '../hooks/useClaimScanner.ts';

import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { useToast } from '@/providers/toast-provider';

export function EnrollmentSection({ forceOpen }: { forceOpen: boolean }) {
  const {
    connected,
    busy,
    enrolled,
    autoConnecting,
    deviceName,
    settings,
    claimText,
    setClaimText,
    connect,
    stopConnection,
  } = useCheckpoint();
  const { showToast } = useToast();

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

  const claimValid = useMemo(() => {
    try {
      parseClaimHex(claimText.trim());
      return true;
    } catch {
      return false;
    }
  }, [claimText]);

  if (!forceOpen && enrolled) return null;

  return (
    <Section title="Set up pendant">
      <Card>
        <Text variant="muted" className="text-[12px]">
          Hold the pendant button for 5 seconds to enter pairing mode.
        </Text>
        <View className="gap-1">
          <Text className="text-[11px] text-subtle-foreground">Claim key / claim URI (64 hex)</Text>
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
  );
}
