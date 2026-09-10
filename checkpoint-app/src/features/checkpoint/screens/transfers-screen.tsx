import { FlatList, View } from "react-native";

import { AppHeader } from "@/components/shared/app-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Screen } from "@/components/shared/screen";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Text } from "@/components/ui/text";

import { useCheckpoint, type TransferInfo } from "../hooks/useCheckpoint.tsx";

function TransferRow({ item }: { item: TransferInfo }) {
  const pct =
    item.totalFrags > 0 ? Math.round((100 * item.received) / item.totalFrags) : 0;
  return (
    <Card>
      <CardContent className="gap-1 pt-4">
        <Text className="font-mono text-sm">file_{item.fileId}.ogg</Text>
        <Text variant="muted">
          {item.totalBytes}B · BLE {pct}% ({item.received}/{item.totalFrags})
        </Text>
        <Text variant="muted">VAD {item.vad}</Text>
        <Text variant="muted">Ingest {item.ingest}</Text>
        <View className="mt-1 h-2 overflow-hidden rounded-full bg-muted">
          <View className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
        </View>
      </CardContent>
    </Card>
  );
}

export function TransfersScreen() {
  const { transfers, shareBench } = useCheckpoint();

  return (
    <Screen>
      <AppHeader title="Transfers" subtitle={`${transfers.length} audio items`} />
      <Button variant="outline" onPress={() => void shareBench()}>
        <Text>Share bench CSV</Text>
      </Button>
      <FlatList
        data={transfers}
        keyExtractor={(item) => item.fileId}
        contentContainerStyle={{ gap: 12, paddingVertical: 12 }}
        renderItem={({ item }) => <TransferRow item={item} />}
        ListEmptyComponent={
          <EmptyState title="No transfers yet" hint="Connect and sync the pendant to receive audio." />
        }
      />
    </Screen>
  );
}
