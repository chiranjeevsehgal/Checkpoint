import { FlatList, View } from "react-native";

import { AppHeader } from "@/components/shared/app-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Screen } from "@/components/shared/screen";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Text } from "@/components/ui/text";

import { fileStateLabel, formatBytes } from "../parsers.ts";
import { useCheckpoint } from "../hooks/useCheckpoint.tsx";

export function StorageScreen() {
  const {
    connected,
    storage,
    fileList,
    listPage,
    refreshStorage,
    listPrev,
    listNext,
    deleteFile,
    eraseStorage,
  } = useCheckpoint();

  const pct =
    storage && storage.total > 0
      ? Math.min(100, Math.round((100 * storage.used) / storage.total))
      : 0;
  const pageLabel =
    listPage.total > 0
      ? `${listPage.start + 1}–${listPage.start + listPage.count} of ${listPage.total}`
      : "—";

  return (
    <Screen>
      <AppHeader title="Storage" subtitle="Pendant SD card" />
      <View className="gap-4 pb-6">
        <Card>
          <CardHeader>
            <CardTitle>
              {storage
                ? `SD: ${formatBytes(storage.used)} / ${formatBytes(storage.total)} (${pct}%)`
                : "SD: —"}
            </CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <View className="h-2 overflow-hidden rounded-full bg-muted">
              <View className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
            </View>
            <Text variant="muted">
              {storage
                ? `${storage.files} files · ${storage.pending} pending`
                : "No storage info yet."}
            </Text>
            <Button variant="outline" disabled={!connected} onPress={() => void refreshStorage()}>
              <Text>Refresh</Text>
            </Button>
          </CardContent>
        </Card>
        <View className="flex-row items-center justify-between">
          <Text variant="large">Device files</Text>
          <Text variant="muted">{pageLabel}</Text>
        </View>
        <FlatList
          data={fileList?.entries ?? []}
          keyExtractor={(item) => item.name}
          scrollEnabled={false}
          contentContainerStyle={{ gap: 8 }}
          renderItem={({ item }) => (
            <Card>
              <CardContent className="gap-1 pt-4">
                <Text className="font-mono text-sm">{item.name}</Text>
                <Text variant="muted">
                  {formatBytes(item.size)} · {fileStateLabel(item.flags)}
                </Text>
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={!connected}
                  onPress={() => void deleteFile(item.name)}
                >
                  <Text>Delete</Text>
                </Button>
              </CardContent>
            </Card>
          )}
          ListEmptyComponent={
            <EmptyState title="No files listed" hint="Refresh to load the pendant file list." />
          }
        />
        <View className="flex-row gap-2">
          <Button variant="outline" className="flex-1" disabled={!connected} onPress={() => void listPrev()}>
            <Text>{"< Prev"}</Text>
          </Button>
          <Button variant="outline" className="flex-1" disabled={!connected} onPress={() => void listNext()}>
            <Text>{"Next >"}</Text>
          </Button>
        </View>
        <Button variant="destructive" disabled={!connected} onPress={() => void eraseStorage()}>
          <Text>Erase all…</Text>
        </Button>
      </View>
    </Screen>
  );
}
