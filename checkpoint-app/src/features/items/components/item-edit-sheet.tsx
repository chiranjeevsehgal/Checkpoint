import { DateTimePicker } from '@expo/ui/community/datetime-picker';
import { X } from 'lucide-react-native';
import { useState } from 'react';
import { KeyboardAvoidingView, Modal, Pressable, ScrollView, View } from 'react-native';

import { HeaderIconButton } from '@/components/shared/header-icon-button';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Text } from '@/components/ui/text';
import { formatItemDate, type Category } from '@/features/items/itemsView';
import { describeUserError } from '@/lib/user-errors';

export interface EditTarget {
  category: Category;
  id: number;
  text: string;
  remindAt?: string | null;
  important?: boolean;
}

export interface EditUpdate {
  text: string;
  remindAt?: string | null;
  important?: boolean;
}

const TITLES: Record<Category, string> = {
  todos: 'Edit to-do',
  reminders: 'Edit reminder',
  insights: 'Edit insight',
};

function withDate(base: Date, picked: Date): Date {
  return new Date(
    picked.getFullYear(),
    picked.getMonth(),
    picked.getDate(),
    base.getHours(),
    base.getMinutes(),
  );
}

function withTime(base: Date, picked: Date): Date {
  return new Date(
    base.getFullYear(),
    base.getMonth(),
    base.getDate(),
    picked.getHours(),
    picked.getMinutes(),
  );
}

export function ItemEditSheet({
  target,
  onCancel,
  onSave,
}: {
  target: EditTarget;
  onCancel: () => void;
  onSave: (update: EditUpdate) => Promise<void>;
}) {
  const [text, setText] = useState(target.text);
  const [important, setImportant] = useState(target.important ?? true);
  const [due, setDue] = useState<Date | null>(target.remindAt ? new Date(target.remindAt) : null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isReminder = target.category === 'reminders';
  const initialDue = target.remindAt ?? null;
  const currentDue = due ? due.toISOString() : null;
  const trimmed = text.trim();
  const changed =
    trimmed !== target.text ||
    (isReminder && (currentDue !== initialDue || important !== (target.important ?? true)));
  const ready = trimmed.length > 0 && changed && !busy;

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await onSave({
        text: trimmed,
        remindAt: isReminder ? currentDue : undefined,
        important: isReminder ? important : undefined,
      });
    } catch (err) {
      setError(describeUserError(err, 'Could not save your changes.'));
      setBusy(false);
    }
  };

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onCancel}>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
        <View className="flex-1 justify-end bg-black/50">
          <Pressable
            className="flex-1"
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close editor"
          />
          <View className="max-h-[85%] gap-3 bg-popover p-4 pb-8">
            <View className="flex-row items-center justify-between gap-2">
              <Text className="font-display text-[17px]">{TITLES[target.category]}</Text>
              <HeaderIconButton icon={X} label="Close" onPress={onCancel} />
            </View>

            <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: 12 }}>
              <Input
                value={text}
                onChangeText={setText}
                multiline
                textAlignVertical="top"
                className="h-24 py-2"
                editable={!busy}
                maxLength={1000}
                autoFocus
                placeholder="Text"
              />

              {isReminder ? (
                <>
                  <View className="flex-row items-center justify-between gap-2">
                    <Text className="text-[13px]">Important</Text>
                    <Switch value={important} onValueChange={setImportant} disabled={busy} />
                  </View>
                  <View className="gap-2">
                    <View className="flex-row items-center justify-between gap-2">
                      <Text className="flex-1 text-[13px]">
                        Due {due ? formatItemDate(due.toISOString()) : '· No time set'}
                      </Text>
                      {due ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy}
                          onPress={() => setDue(null)}
                        >
                          <Text>Clear</Text>
                        </Button>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onPress={() => setDue(new Date())}
                        >
                          <Text>Set time</Text>
                        </Button>
                      )}
                    </View>
                    {due ? (
                      <View className="gap-2">
                        <DateTimePicker
                          value={due}
                          mode="date"
                          presentation="inline"
                          onValueChange={(_event, picked) => setDue(withDate(due, picked))}
                        />
                        <DateTimePicker
                          value={due}
                          mode="time"
                          presentation="inline"
                          onValueChange={(_event, picked) => setDue(withTime(due, picked))}
                        />
                      </View>
                    ) : null}
                  </View>
                </>
              ) : null}

              {error ? <Text className="text-[12px] text-destructive">{error}</Text> : null}
            </ScrollView>

            <View className="flex-row justify-end gap-2">
              <Button variant="outline" size="sm" disabled={busy} onPress={onCancel}>
                <Text>Cancel</Text>
              </Button>
              <Button size="sm" disabled={!ready} onPress={() => void save()}>
                <Text>{busy ? 'Saving…' : 'Save'}</Text>
              </Button>
            </View>
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}
