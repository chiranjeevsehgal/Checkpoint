import { useState } from "react";
import { View } from "react-native";

import { AppHeader } from "@/components/shared/app-header";
import { Screen } from "@/components/shared/screen";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

import { Toggle } from "../components/toggle.tsx";
import { useCheckpoint } from "../hooks/useCheckpoint.tsx";

export function DeviceScreen() {
  const { connected, status, toggleRec, refreshStatus, applyLed, applySync } =
    useCheckpoint();
  const [muted, setMuted] = useState(false);
  const [brightness, setBrightness] = useState("30");
  const [sync, setSync] = useState(true);

  const statusText = status
    ? `rec: ${status.recording ? "ON" : "OFF"} vad: ${
        status.vadSpeech ? "speech" : status.vadActive ? "active" : "idle"
      } pend: ${status.pending} chunks: ${status.chunks} lvl: ${status.levelDbfs}dB`
    : "rec: —";

  return (
    <Screen>
      <AppHeader title="Device" subtitle="BLE remote control" />
      <View className="gap-4 pb-6">
        <Card>
          <CardHeader>
            <CardTitle>Recording</CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <Text variant="muted">{statusText}</Text>
            <View className="flex-row gap-2">
              <Button
                className="flex-1"
                disabled={!connected}
                variant={status?.recording ? "destructive" : "default"}
                onPress={() => void toggleRec()}
              >
                <Text>
                  {status?.recording ? "● Stop Rec" : "○ Start Rec"}
                </Text>
              </Button>
              <Button
                variant="outline"
                className="flex-1"
                disabled={!connected}
                onPress={() => void refreshStatus()}
              >
                <Text>Refresh</Text>
              </Button>
            </View>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>LED</CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <Toggle label="LED muted" value={muted} onChange={setMuted} disabled={!connected} />
            <Text variant="muted">Brightness (5–255)</Text>
            <Input
              value={brightness}
              onChangeText={setBrightness}
              editable={connected}
              keyboardType="number-pad"
            />
            <Button
              disabled={!connected}
              onPress={() => void applyLed(muted, Number(brightness) || 0)}
            >
              <Text>Apply LED</Text>
            </Button>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Auto-sync</CardTitle>
          </CardHeader>
          <CardContent>
            <Toggle
              label="Sync recordings automatically"
              value={sync}
              disabled={!connected}
              onChange={(next) => {
                setSync(next);
                void applySync(next);
              }}
            />
          </CardContent>
        </Card>
      </View>
    </Screen>
  );
}
