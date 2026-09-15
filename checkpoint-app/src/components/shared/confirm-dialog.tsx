import { useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Modal, View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { PasswordInput } from '@/components/ui/password-input';
import { Text } from '@/components/ui/text';

interface ConfirmDialogProps {
  visible: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  requireText?: string;
  requirePassword?: boolean;
  onCancel: () => void;
  onConfirm: (password?: string) => void | Promise<void>;
}

type DialogBodyProps = Omit<ConfirmDialogProps, 'visible'>;

function DialogBody({
  title,
  body,
  confirmLabel,
  requireText,
  requirePassword,
  onCancel,
  onConfirm,
}: DialogBodyProps) {
  const [typed, setTyped] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const textReady = !requireText || typed.trim().toUpperCase() === requireText.toUpperCase();
  const passwordReady = !requirePassword || password !== '';
  const ready = textReady && passwordReady && !busy;

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      await onConfirm(requirePassword ? password : undefined);
    } catch (err) {
      setPassword('');
      setError(err instanceof Error ? err.message : 'Something went wrong.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <View className="w-full gap-3 bg-popover p-4">
      <Text className="font-display text-[17px] leading-tight">{title}</Text>
      <Text className="text-[13px] text-muted-foreground">{body}</Text>
      {requireText ? (
        <Input
          value={typed}
          onChangeText={setTyped}
          autoCapitalize="characters"
          autoCorrect={false}
          editable={!busy}
          placeholder={requireText}
        />
      ) : null}
      {requirePassword ? (
        <PasswordInput
          placeholder="Account password"
          autoComplete="current-password"
          value={password}
          onChangeText={setPassword}
          editable={!busy}
        />
      ) : null}
      {error ? <Text className="text-[12px] text-destructive">{error}</Text> : null}
      <View className="mt-1 flex-row justify-end gap-2">
        <Button variant="outline" size="sm" disabled={busy} onPress={onCancel}>
          <Text>Cancel</Text>
        </Button>
        <Button variant="destructive" size="sm" disabled={!ready} onPress={() => void confirm()}>
          {busy ? <ActivityIndicator size="small" /> : <Text>{confirmLabel}</Text>}
        </Button>
      </View>
    </View>
  );
}

export function ConfirmDialog({ visible, onCancel, ...rest }: ConfirmDialogProps) {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel}>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
        <View className="flex-1 items-center justify-center bg-black/50 p-6">
          {visible ? <DialogBody {...rest} onCancel={onCancel} /> : null}
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}
