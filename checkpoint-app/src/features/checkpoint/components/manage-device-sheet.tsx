import { X } from 'lucide-react-native';
import { useState } from 'react';
import { KeyboardAvoidingView, Modal, Pressable, View } from 'react-native';

import { useCheckpoint } from '../hooks/useCheckpoint.tsx';

import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { HeaderIconButton } from '@/components/shared/header-icon-button';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { useToast } from '@/providers/toast-provider';

interface ManageDeviceSheetProps {
  visible: boolean;
  onClose: () => void;
  onSetup: () => void;
}

export function ManageDeviceSheet({ visible, onClose, onSetup }: ManageDeviceSheetProps) {
  const {
    connected,
    busy,
    settings,
    deviceName,
    setDeviceName,
    deviceId,
    enrolled,
    linkState,
    reconnect,
    disconnect,
    stopConnection,
    requestForget,
    requestRelease,
  } = useCheckpoint();
  const { showToast } = useToast();
  const [name, setName] = useState(deviceName);

  const saveName = () => {
    const trimmed = name.trim();
    if (trimmed === '' || trimmed === deviceName) return;
    setDeviceName(trimmed);
    showToast('Device name saved.');
  };

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
        <View className="flex-1 justify-end bg-black/50">
          <Pressable
            className="flex-1"
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Close manage device"
          />
          <View className="gap-3 bg-popover p-4 pb-8">
            <View className="flex-row items-center justify-between gap-2">
              <Text className="font-display text-[17px]">Manage device</Text>
              <HeaderIconButton icon={X} label="Close" onPress={onClose} />
            </View>

            <View className="gap-1">
              <Text className="text-[11px] text-subtle-foreground">Device name or address</Text>
              <View className="flex-row gap-2">
                <Input
                  className="flex-1"
                  value={name}
                  onChangeText={setName}
                  editable={!connected && !busy}
                  autoCapitalize="none"
                  placeholder="Checkpoint"
                />
                <Button
                  variant="outline"
                  disabled={connected || busy || name.trim() === deviceName}
                  onPress={saveName}
                >
                  <Text>Save</Text>
                </Button>
              </View>
            </View>

            <View className="gap-2">
              {connected ? (
                <View className="flex-row gap-2">
                  <Button
                    variant="outline"
                    className="flex-1"
                    disabled={busy}
                    onPress={() => void reconnect()}
                  >
                    <Text>Reconnect</Text>
                  </Button>
                  <Button
                    variant="outline"
                    className="flex-1"
                    disabled={busy}
                    onPress={() => void disconnect()}
                  >
                    <Text>Disconnect</Text>
                  </Button>
                </View>
              ) : linkState === 'reconnecting' ? (
                <Button variant="outline" onPress={() => void stopConnection()}>
                  <Text>Stop reconnecting</Text>
                </Button>
              ) : enrolled ? (
                <Button disabled={busy} onPress={() => void reconnect()}>
                  <Text>Connect</Text>
                </Button>
              ) : (
                <Button
                  variant="outline"
                  onPress={() => {
                    onSetup();
                    onClose();
                  }}
                >
                  <Text>Set up pendant</Text>
                </Button>
              )}
              <Button
                variant="outline"
                disabled={busy || !enrolled}
                onPress={() => {
                  onClose();
                  requestRelease();
                }}
              >
                <Text className="text-destructive">Release pendant</Text>
              </Button>
              <Button
                variant="outline"
                disabled={busy || !enrolled}
                onPress={() => {
                  onClose();
                  requestForget();
                }}
              >
                <Text className="text-destructive">Forget pendant</Text>
              </Button>
            </View>

            {settings.developerMode ? (
              <DeveloperDetails defaultExpanded>
                <DetailRow label="Device ID" value={deviceId ?? '—'} />
                <DetailRow label="Claim" value={enrolled ? 'Linked' : 'Not linked'} />
                <DetailRow label="Link state" value={linkState} />
              </DeveloperDetails>
            ) : null}
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}
