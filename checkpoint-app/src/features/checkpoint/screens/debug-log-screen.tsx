import * as Clipboard from 'expo-clipboard';
import { Directory, File, Paths } from 'expo-file-system';
import { useRouter } from 'expo-router';
import { isAvailableAsync, shareAsync } from 'expo-sharing';
import { useCallback, useMemo, useState } from 'react';
import { FlatList, Pressable, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import {
  classifyLog,
  filterLogs,
  formatLogTime,
  isErrorLog,
  LOG_FILTERS,
  type LogFilter,
} from '../logFilter.ts';
import type { LogEntry } from '../types.ts';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { Button } from '@/components/ui/button';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

function exportText(logs: LogEntry[]): string {
  return logs.map((entry) => `${new Date(entry.at).toISOString()} ${entry.text}`).join('\n');
}

function LogEvent({ entry }: { entry: LogEntry }) {
  const category = classifyLog(entry.text);
  const error = isErrorLog(entry.text);

  return (
    <View className="gap-0.5">
      <View className="flex-row items-center gap-2">
        <Text className="font-mono text-[11px] text-subtle-foreground">
          {formatLogTime(entry.at)}
        </Text>
        <Text className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">
          {category}
        </Text>
      </View>
      <Text className={cn('font-mono text-[11px]', error ? 'text-destructive' : 'text-foreground')}>
        {entry.text}
      </Text>
    </View>
  );
}

export function DebugLogScreen() {
  const { logs, clearLogs } = useCheckpoint();
  const router = useRouter();
  const [filter, setFilter] = useState<LogFilter>('all');
  const visible = useMemo(() => filterLogs(logs, filter), [logs, filter]);

  const share = useCallback(() => {
    void (async () => {
      if (logs.length === 0) return;
      try {
        if (!(await isAvailableAsync())) {
          await Clipboard.setStringAsync(exportText(logs));
          return;
        }
        const dir = new Directory(Paths.cache, 'checkpoint');
        if (!dir.exists) dir.create();
        const file = new File(dir, `debug-log-${Date.now()}.txt`);
        if (file.exists) file.delete();
        file.create();
        file.write(new TextEncoder().encode(exportText(logs)));
        await shareAsync(file.uri, { dialogTitle: 'Share debug log' });
      } catch (error) {
        console.warn(
          `[ui] log share failed: ${error instanceof Error ? error.message : 'unknown'}`,
        );
      }
    })();
  }, [logs]);

  return (
    <CheckpointScreen>
      <AppHeader
        title="Debug Log"
        subtitle={`${logs.length} events`}
        showDebugLog={false}
        onBack={() => router.back()}
      />
      <FlatList
        className="flex-1"
        data={visible}
        keyExtractor={(item, index) => `${item.at}-${index}`}
        contentContainerStyle={{ gap: 10, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ListHeaderComponent={
          <View className="gap-3">
            <View className="flex-row flex-wrap gap-1.5">
              {LOG_FILTERS.map((option) => (
                <Pressable
                  key={option.id}
                  onPress={() => setFilter(option.id)}
                  accessibilityRole="button"
                  accessibilityState={{ selected: filter === option.id }}
                  className={cn(
                    'border px-2.5 py-1 active:opacity-80',
                    filter === option.id ? 'border-primary bg-primary' : 'border-border',
                  )}
                >
                  <Text
                    className={cn(
                      'text-[11px]',
                      filter === option.id ? 'text-primary-foreground' : 'text-muted-foreground',
                    )}
                  >
                    {option.label}
                  </Text>
                </Pressable>
              ))}
            </View>
            <View className="flex-row gap-2">
              <Button variant="outline" size="sm" className="flex-1" onPress={clearLogs}>
                <Text>Clear</Text>
              </Button>
              <Button variant="outline" size="sm" className="flex-1" onPress={share}>
                <Text>Share</Text>
              </Button>
            </View>
          </View>
        }
        renderItem={({ item }) => <LogEvent entry={item} />}
        ListEmptyComponent={
          <EmptyState
            title="No log events"
            hint="Connect and use the pendant to populate the log."
          />
        }
      />
    </CheckpointScreen>
  );
}
