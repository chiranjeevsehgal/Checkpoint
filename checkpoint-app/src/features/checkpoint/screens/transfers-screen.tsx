import { Pause, Play, RefreshCw, Upload } from 'lucide-react-native';
import { FlatList, Pressable, View } from 'react-native';

import { CheckpointScreen } from '../components/checkpoint-screen.tsx';
import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { playback, usePlayback } from '../playback.ts';
import { statusDescriptor, TONE_TEXT } from '../status.ts';
import type { TransferRecord } from '../transferStore.ts';
import {
  formatTransferTime,
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
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { useRefresh } from '@/lib/use-refresh';
import { cn } from '@/lib/utils';
import { useToast } from '@/providers/toast-provider';

const STEPS = ['Receive', 'Analyze', 'Upload'] as const;

const STAGE_INDEX: Record<TransferStage, number> = {
  receiving: 0,
  analyzing: 1,
  uploading: 2,
  done: 3,
};

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
      className="active:bg-foreground/10 border border-border p-1.5"
    >
      <Icon as={isPlaying ? Pause : Play} size={14} />
    </Pressable>
  );
}

function TransferRow({
  item,
  developerMode,
  onRetry,
}: {
  item: TransferRecord;
  developerMode: boolean;
  onRetry: () => void;
}) {
  const view = transferView(item);
  const { tone } = statusDescriptor(view.status);

  return (
    <Card>
      <View className="flex-row items-center justify-between gap-2">
        <Text className="flex-1 font-display text-[15px]">
          Recording · {formatTransferTime(item.createdAt)}
        </Text>
        {item.localUri ? <PlayButton uri={item.localUri} label={view.filename} /> : null}
      </View>
      <Text variant="muted" className="text-[11px]">
        {view.sizeLabel} · {view.vadLabel}
      </Text>
      <Text className={cn('text-[12px]', TONE_TEXT[tone])}>{view.headline}</Text>
      <ProgressBar value={view.pct} className="h-1" />
      {view.failed ? (
        <Button variant="outline" size="sm" onPress={onRetry}>
          <Icon as={RefreshCw} size={14} />
          <Text>Retry</Text>
        </Button>
      ) : null}
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
  const { transfers, settings, shareBench, refreshTransfers } = useCheckpoint();
  const { refreshing, onRefresh } = useRefresh(refreshTransfers);

  const views = transfers.map(transferView);
  const uploaded = views.filter((view) => view.outcome === 'uploaded').length;
  const filtered = views.filter((view) => view.outcome === 'filtered').length;
  const failed = views.filter((view) => view.outcome === 'failed').length;

  return (
    <CheckpointScreen>
      <AppHeader title="Transfers" subtitle={`${transfers.length} audio items`} />
      <FlatList
        className="flex-1"
        data={transfers}
        keyExtractor={(item) => item.fileId}
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ListHeaderComponent={
          <View className="flex-row items-center justify-between gap-2">
            <Text variant="muted" className="flex-1 text-[12px]">
              {transfers.length} total · {uploaded} uploaded · {filtered} filtered · {failed} failed
            </Text>
            {settings.developerMode ? (
              <Button
                variant="ghost"
                size="sm"
                disabled={transfers.length === 0}
                onPress={() => void shareBench()}
              >
                <Icon as={Upload} size={14} />
                <Text>Bench CSV</Text>
              </Button>
            ) : null}
          </View>
        }
        renderItem={({ item }) => (
          <TransferRow
            item={item}
            developerMode={settings.developerMode}
            onRetry={() => void refreshTransfers()}
          />
        )}
        ListEmptyComponent={
          <EmptyState
            title="No transfers yet"
            hint="Connect and sync the pendant to receive audio."
          />
        }
      />
    </CheckpointScreen>
  );
}
