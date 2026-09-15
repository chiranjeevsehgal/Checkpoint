import { useRouter } from 'expo-router';
import { Check } from 'lucide-react-native';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, View } from 'react-native';

import { filterLanguages, sameSelection, selectedFirst } from '../languageView.ts';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { getUserSettings, putUserSettings, type LanguageOption } from '@/lib/api/settings-api';
import { getSessionToken } from '@/lib/session';
import { cn } from '@/lib/utils';
import { useToast } from '@/providers/toast-provider';

function LanguageRow({
  language,
  selected,
  onToggle,
}: {
  language: LanguageOption;
  selected: boolean;
  onToggle: () => void;
}) {
  return (
    <Pressable
      onPress={onToggle}
      accessibilityRole="checkbox"
      accessibilityState={{ checked: selected }}
      accessibilityLabel={language.name}
      className="flex-row items-center justify-between gap-3 border-b border-border py-3 active:opacity-70"
    >
      <View className="flex-1">
        <Text className="text-[14px]">{language.name}</Text>
        <Text variant="muted" className="text-[11px] uppercase">
          {language.code}
        </Text>
      </View>
      <View
        className={cn(
          'h-5 w-5 items-center justify-center border',
          selected ? 'border-primary bg-primary' : 'border-border',
        )}
      >
        {selected ? <Icon as={Check} size={13} className="text-primary-foreground" /> : null}
      </View>
    </Pressable>
  );
}

export function LanguagesScreen() {
  const router = useRouter();
  const { showToast } = useToast();
  const [catalog, setCatalog] = useState<LanguageOption[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [baseline, setBaseline] = useState<string[]>([]);
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const token = getSessionToken();
      if (!token) {
        if (!cancelled) {
          setError('Not signed in.');
          setLoading(false);
        }
        return;
      }
      try {
        const settings = await getUserSettings(token);
        if (cancelled) return;
        setCatalog(settings.available);
        setSelected(settings.languages);
        setBaseline(settings.languages);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Could not load languages.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const toggle = useCallback((code: string) => {
    setSelected((current) =>
      current.includes(code) ? current.filter((item) => item !== code) : [...current, code],
    );
  }, []);

  const save = useCallback(async () => {
    const token = getSessionToken();
    if (!token) {
      showToast('Not signed in.');
      return;
    }
    setSaving(true);
    try {
      const saved = await putUserSettings(token, selected);
      setSelected(saved);
      setBaseline(saved);
      showToast('Languages saved.');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Could not save languages.');
    } finally {
      setSaving(false);
    }
  }, [selected, showToast]);

  const visible = useMemo(
    () => selectedFirst(filterLanguages(catalog, query), selected),
    [catalog, query, selected],
  );
  const dirty = !sameSelection(baseline, selected);

  return (
    <Screen>
      <AppHeader
        title="Transcription languages"
        subtitle="Only transcribe the languages you select"
        onBack={() => router.back()}
      />
      <View className="gap-1 pb-2">
        <Input
          value={query}
          onChangeText={setQuery}
          placeholder="Search languages"
          autoCapitalize="none"
          autoCorrect={false}
          editable={!loading}
        />
        <View className="flex-row items-center justify-between gap-2">
          <Text variant="muted" className="flex-1 text-[11px]">
            {selected.length > 0
              ? `${selected.length} selected`
              : 'No languages selected — all languages are transcribed.'}
          </Text>
          {selected.length > 0 ? (
            <Button variant="ghost" size="sm" onPress={() => setSelected([])}>
              <Text>Clear all</Text>
            </Button>
          ) : null}
        </View>
      </View>

      {loading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <EmptyState title="Could not load languages" hint={error} />
      ) : (
        <FlatList
          className="flex-1"
          data={visible}
          keyExtractor={(item) => item.code}
          keyboardShouldPersistTaps="handled"
          renderItem={({ item }) => (
            <LanguageRow
              language={item}
              selected={selected.includes(item.code)}
              onToggle={() => toggle(item.code)}
            />
          )}
          ListEmptyComponent={
            <EmptyState title="No languages found" hint="Try a different search." />
          }
        />
      )}

      <Button
        className="my-3"
        disabled={saving || loading || error !== null || !dirty}
        onPress={() => void save()}
      >
        <Text>{saving ? 'Saving…' : 'Save'}</Text>
      </Button>
    </Screen>
  );
}
