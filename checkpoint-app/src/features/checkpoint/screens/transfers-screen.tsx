import { Upload } from 'lucide-react-native';
import { Fragment, useState } from 'react';
import { FlatList, Pressable, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Icon } from '@/components/ui/icon';
import { ProgressBar } from '@/components/ui/progress-bar';
import { AppRefreshControl } from '@/components/ui/refresh-control';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { useRefresh } from '@/lib/use-refresh';

import { useCheckpoint } from '../hooks/useCheckpoint.tsx';
import { isTerminal, type TransferRecord } from '../transferStore.ts';
import { transferView, type TransferStage, type TransferView } from '../transferView.ts';

const STEPS = ['Receive', 'Analyze', 'Upload'] as const;

const STAGE_INDEX: Record<TransferStage, number> = {
  receiving: 0,
  analyzing: 1,
  uploading: 2,
  done: 3,
};

function stepBackground(index: number, stageIndex: number): string {
  if (stageIndex > index) return 'bg-success';
  if (stageIndex === index) return 'bg-primary';
  return 'bg-input-bg';
}

function stepText(index: number, stageIndex: number): string {
  if (stageIndex > index) return 'text-background';
  if (stageIndex === index) return 'text-primary-foreground';
  return 'text-muted-foreground';
}

function statusTextClass(view: TransferView): string {
  if (view.failed) return 'text-destructive';
  if (view.stage === 'receiving' || view.stage === 'uploading') return 'text-primary';
  if (view.stage === 'analyzing') return 'text-warning';
  if (view.outcome === 'uploaded') return 'text-success';
  return 'text-muted-foreground';
}

function Chip({ label, className, textClassName }: { label: string; className?: string; textClassName?: string }) {
  return (
    <View className={cn('px-2.5 py-1', className)}>
      <Text className={cn('text-[11px]', textClassName)}>{label}</Text>
    </View>
  );
}

function TransferRow({ item, expanded, onToggle }: { item: TransferRecord; expanded: boolean; onToggle: () => void }) {
  const view = transferView(item);
  const stageIndex = STAGE_INDEX[view.stage];

  return (
    <Card>
      <Pressable onPress={onToggle} accessibilityRole="button" className="gap-2">
        <View className="flex-row items-baseline justify-between gap-2">
          <Text className="font-mono text-[13px]">{view.filename}</Text>
          <Text variant="muted" className="text-[11px]">
            {view.sizeLabel} · {Math.round(view.pct * 100)}%
          </Text>
        </View>
        <ProgressBar value={view.pct} className="h-1" />
        <View className="flex-row items-center gap-1.5">
          {STEPS.map((step, index) => (
            <Fragment key={step}>
              {index > 0 ? <View className="bg-divider h-px flex-1" /> : null}
              <Text className={cn('px-2 py-0.5 text-[10px]', stepBackground(index, stageIndex), stepText(index, stageIndex))}>
                {step}
              </Text>
            </Fragment>
          ))}
        </View>
        <Text className="text-muted-foreground text-[12px]">VAD: {view.vadLabel}</Text>
        <Text className={cn('text-[12px]', statusTextClass(view))}>Ingest: {view.ingestLabel}</Text>
        {expanded ? (
          <Text className="border-divider text-subtle-foreground border-t pt-2 font-mono text-[11px]">
            fragments {Math.round(view.pct * 100)}% received · stage tracked live from the pendant transfer
          </Text>
        ) : null}
      </Pressable>
    </Card>
  );
}

export function TransfersScreen() {
  const { transfers, shareBench, refreshTransfers } = useCheckpoint();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const { refreshing, onRefresh } = useRefresh(refreshTransfers);

  const views = transfers.map(transferView);
  const uploaded = views.filter((view) => view.outcome === 'uploaded').length;
  const filtered = views.filter((view) => view.outcome === 'filtered').length;
  const failed = views.filter((view) => view.outcome === 'failed').length;
  const queued = transfers.filter((item) => !isTerminal(item) && item.localUri).length;

  return (
    <Screen>
      <AppHeader title="Transfers" subtitle={`${transfers.length} audio items`} />
      <FlatList
        className="flex-1"
        data={transfers}
        keyExtractor={(item) => item.fileId}
        refreshControl={<AppRefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        contentContainerStyle={{ gap: 12, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
        ListHeaderComponent={
          <View className="gap-3">
            <Button variant="outline" onPress={() => void shareBench()}>
              <Icon as={Upload} size={15} />
              <Text>Share bench CSV</Text>
            </Button>
            {transfers.length > 0 ? (
              <View className="flex-row flex-wrap gap-2">
                <Chip
                  label={`${uploaded} uploaded`}
                  className="bg-success"
                  textClassName="text-background"
                />
                <Chip
                  label={`${filtered} filtered`}
                  className="bg-secondary"
                  textClassName="text-secondary-foreground"
                />
                {queued > 0 ? (
                  <Chip
                    label={`${queued} queued`}
                    className="bg-warning"
                    textClassName="text-background"
                  />
                ) : null}
                <Chip label={`${failed} failed`} className="bg-destructive" textClassName="text-background" />
              </View>
            ) : null}
          </View>
        }
        renderItem={({ item }) => (
          <TransferRow
            item={item}
            expanded={expandedId === item.fileId}
            onToggle={() => setExpandedId((current) => (current === item.fileId ? null : item.fileId))}
          />
        )}
        ListEmptyComponent={
          <EmptyState title="No transfers yet" hint="Connect and sync the pendant to receive audio." />
        }
      />
    </Screen>
  );
}
