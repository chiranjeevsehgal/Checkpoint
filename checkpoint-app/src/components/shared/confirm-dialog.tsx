import { useState } from 'react';
import { Modal, View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';

interface ConfirmDialogProps {
  visible: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  requireText?: string;
  onCancel: () => void;
  onConfirm: () => void;
}

type DialogBodyProps = Omit<ConfirmDialogProps, 'visible'>;

function DialogBody({ title, body, confirmLabel, requireText, onCancel, onConfirm }: DialogBodyProps) {
  const [typed, setTyped] = useState('');
  const ready = !requireText || typed.trim().toUpperCase() === requireText.toUpperCase();

  return (
    <View className="bg-popover w-full gap-3 p-4">
      <Text className="font-display text-[17px] leading-tight">{title}</Text>
      <Text className="text-muted-foreground text-[13px]">{body}</Text>
      {requireText ? (
        <Input
          value={typed}
          onChangeText={setTyped}
          autoCapitalize="characters"
          autoCorrect={false}
          placeholder={requireText}
        />
      ) : null}
      <View className="mt-1 flex-row justify-end gap-2">
        <Button variant="outline" size="sm" onPress={onCancel}>
          <Text>Cancel</Text>
        </Button>
        <Button variant="destructive" size="sm" disabled={!ready} onPress={onConfirm}>
          <Text>{confirmLabel}</Text>
        </Button>
      </View>
    </View>
  );
}

export function ConfirmDialog({ visible, onCancel, ...rest }: ConfirmDialogProps) {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel}>
      <View className="flex-1 items-center justify-center bg-black/50 p-6">
        {visible ? <DialogBody {...rest} onCancel={onCancel} /> : null}
      </View>
    </Modal>
  );
}
