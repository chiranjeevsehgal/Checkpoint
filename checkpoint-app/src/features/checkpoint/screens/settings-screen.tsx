import { useState } from "react";
import { View } from "react-native";

import { AppHeader } from "@/components/shared/app-header";
import { Screen } from "@/components/shared/screen";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

import { useCheckpoint } from "../hooks/useCheckpoint.tsx";
import type { CheckpointSettings } from "../settings.ts";

function numberField(raw: string, fallback: number): number {
  const value = Number(raw);
  return Number.isFinite(value) ? value : fallback;
}

export function CheckpointSettingsScreen() {
  const { settings, updateSettings } = useCheckpoint();
  const [draft, setDraft] = useState<CheckpointSettings>(settings);
  const [saved, setSaved] = useState(false);

  const set = <K extends keyof CheckpointSettings>(key: K, value: CheckpointSettings[K]) => {
    setSaved(false);
    setDraft((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <Screen>
      <AppHeader title="Settings" subtitle="Server, identity and VAD" />
      <View className="gap-4 pb-6">
        <Card>
          <CardHeader>
            <CardTitle>Ingestion server</CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <Text variant="muted">Server URL (LAN IP for on-device testing)</Text>
            <Input
              value={draft.serverUrl}
              onChangeText={(text) => set("serverUrl", text)}
              autoCapitalize="none"
              autoCorrect={false}
            />
            <Text variant="muted">User ID (Bearer token)</Text>
            <Input
              value={draft.userId}
              onChangeText={(text) => set("userId", text)}
              autoCapitalize="none"
              autoCorrect={false}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Voice gate</CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <Text variant="muted">VAD threshold</Text>
            <Input
              value={String(draft.vadThreshold)}
              onChangeText={(text) =>
                set("vadThreshold", numberField(text, settings.vadThreshold))
              }
              keyboardType="decimal-pad"
            />
            <Text variant="muted">Minimum speech (seconds)</Text>
            <Input
              value={String(draft.minSpeechS)}
              onChangeText={(text) =>
                set("minSpeechS", numberField(text, settings.minSpeechS))
              }
              keyboardType="decimal-pad"
            />
          </CardContent>
        </Card>
        <Button
          onPress={() => {
            void updateSettings(draft).then(() => setSaved(true));
          }}
        >
          <Text>Save settings</Text>
        </Button>
        {saved ? <Text variant="muted">Saved.</Text> : null}
      </View>
    </Screen>
  );
}
