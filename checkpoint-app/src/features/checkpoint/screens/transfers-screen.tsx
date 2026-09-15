import { Pause, Play, RefreshCw, Upload } from 'lucide-react-native';
import { useMemo, useState } from 'react';
import { Pressable, SectionList, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { DisconnectedState } from '../components/disconnected-state.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { playback, usePlayback } from '../playback.ts';
import { statusDescriptor, TONE_TEXT } from '../status.ts';
import type { TransferOutcome, TransferRecord } from '../transferStore.ts';
import {
  formatTransferTime,
  groupTransfersByDay,
  recordTime,
  transferView,
  type TransferStage,
  type TransferView,
} from '../transferView.ts';

import { AppHeader } from '@/components/shared/app-header';
import { DetailRow, DeveloperDetails } from '@/components/shared/developer-details';
import { EmptyState } from '@/components/shared/empty-state';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { useToast } from '@/providers/toast-provider';

const STEPS = ['Receive', 'Analyze', 'Upload'] as const;

const STAGE_INDEX: Record<TransferStage, number> = {
  receiving: 0,
  analyzing: 1,
  uploading: 2,
  done: 3,
};

type TransferFilter = 'all' | TransferOutcome;

const FILTERS: { id: TransferFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'uploaded', label: 'Uploaded' },
  { id: 'filtered', label: 'Filtered' },
  { id: 'failed', label: 'Failed' },
];

function pipelineLabel(view: TransferView): string {
  if (view.failed) return 'Receive ✓ → Analyze ✓ → Upload ✕';
  const index = STAGE_INDEX[view.stage];
  return STEPS.map((step, i) => `${step} ${i < index ? '✓' : i === index ? '…' : '—'}`).join(' → ');
}

function PlayButton({ uri, label }: { uri: string; label: string }) {
  const { label: playingLabel, playing, paused } = usePlayback();
  const { showToast } = useToast();
  const isPlaying = playing && playingLabel === label;
  const isPaused = paused && playingLabel === label;

  const onPress = () => {
    if (isPlaying) {
      playback.pause();
      return;
    }
    if (isPaused) {
      playback.resume();
      return;
    }
    void playback.play(uri, label).catch((error: unknown) => {
      showToast(error instanceof Error ? error.message : 'Playback failed.');
    });
  };

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={isPlaying ? `Pause ${label}` : `Play ${label}`}
      className="active:bg-foreground/10 h-11 w-11 items-center justify-center border border-border"
    >
      <Icon as={isPlaying ? Pause : Play} size={14} />
    </Pressable>
  );
}

function TransferRow({ item, developerMode }: { item: TransferRecord; developerMode: boolean }) {
  const view = transferView(item);
  const { tone } = statusDescriptor(view.status);

  return (
    <Card>
      <View className="flex-row items-center justify-between gap-2">
        <Text className="flex-1 font-display text-[15px]">
          Recording · {formatTransferTime(recordTime(item))}
        </Text>
        {item.localUri ? <PlayButton uri={item.localUri} label={view.filename} /> : null}
      </View>
      <Text variant="muted" className="text-[11px]">
        {view.sizeLabel} · {view.vadLabel}
      </Text>
      <Text className={cn('text-[12px]', TONE_TEXT[tone])}>{view.headline}</Text>
      <ProgressBar value={view.pct} className="h-1" />
      {developerMode ? (
        <DeveloperDetails defaultExpanded>
          <DetailRow label="Pipeline" value={pipelineLabel(view)} />
          <DetailRow label="VAD" value={view.vadLabel} />
          <DetailRow label="Filename" value={view.filename} />
          <DetailRow label="Server result" value={view.ingestLabel} />
        </DeveloperDetails>
      ) : null}
    </Card>
  );
}

export function TransfersScreen() {
  const { connected, transfers, hydrated, settings, shareBench, refreshTransfers } =
    useCheckpoint();
  const [filter, setFilter] = useState<TransferFilter>('all');

  const views = transfers.map(transferView);
  const uploaded = views.filter((view) => view.outcome === 'uploaded').length;
  const filtered = views.filter((view) => view.outcome === 'filtered').length;
  const failed = views.filter((view) => view.outcome === 'failed').length;
  const hasRetryable = transfers.some(
    (record) => record.outcome === 'pending' || record.outcome === 'failed',
  );

  const visible = useMemo(
    () =>
      filter === 'all'
        ? transfers
        : transfers.filter((record) => transferView(record).outcome === filter),
    [transfers, filter],
  );
  const sections = useMemo(() => groupTransfersByDay(visible), [visible]);

  return (
    <CheckpointScreen>
      <AppHeader title="Transfers" subtitle={`${transfers.length} audio items`} />
      <SectionList
        className="flex-1"
        sections={sections}
        keyExtractor={(item) => item.fileId}
        stickySectionHeadersEnabled={false}
        contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ListHeaderComponent={
          transfers.length > 0 ? (
            <View className="gap-3">
              <View className="flex-row items-center justify-between gap-2">
                <Text variant="muted" className="flex-1 text-[12px]">
                  {transfers.length} total · {uploaded} uploaded · {filtered} filtered · {failed}{' '}
                  failed
                </Text>
                {settings.developerMode ? (
                  <Button variant="ghost" size="sm" onPress={() => void shareBench()}>
                    <Icon as={Upload} size={14} />
                    <Text>Bench CSV</Text>
                  </Button>
                ) : null}
              </View>
              <View className="flex-row flex-wrap gap-1.5">
                {FILTERS.map((option) => (
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
              {hasRetryable ? (
                <Button variant="outline" size="sm" onPress={() => void refreshTransfers()}>
                  <Icon as={RefreshCw} size={14} />
                  <Text>Retry all</Text>
                </Button>
              ) : null}
            </View>
          ) : null
        }
        renderSectionHeader={({ section }) => (
          <Text variant="kicker" className="pt-1">
            {section.title}
          </Text>
        )}
        renderItem={({ item }) => (
          <TransferRow item={item} developerMode={settings.developerMode} />
        )}
        ListEmptyComponent={
          !hydrated ? (
            <Text variant="muted" className="py-12 text-center">
              Loading transfers…
            </Text>
          ) : transfers.length === 0 ? (
            connected ? (
              <EmptyState
                title="No transfers yet"
                hint="Listening for audio from the pendant — new recordings appear here."
              />
            ) : (
              <DisconnectedState />
            )
          ) : (
            <EmptyState
              title={`No ${filter} transfers`}
              hint="Try a different filter."
              action={
                <Button variant="outline" onPress={() => setFilter('all')}>
                  <Text>Show all</Text>
                </Button>
              }
            />
          )
        }
      />
    </CheckpointScreen>
  );
}
