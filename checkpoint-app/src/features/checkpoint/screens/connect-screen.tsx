import { View } from "react-native";

import { AppHeader } from "@/components/shared/app-header";
import { Screen } from "@/components/shared/screen";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";

import { LogView } from "../components/log-view.tsx";
import { Toggle } from "../components/toggle.tsx";
import { useCheckpoint } from "../hooks/useCheckpoint.tsx";

export function ConnectScreen() {
  const {
    connected,
    busy,
    linkState,
    deviceName,
    setDeviceName,
    claimText,
    setClaimText,
    logs,
    settings,
    connect,
    disconnect,
    updateSettings,
    clearLogs,
    needsSettings,
    openAppSettings,
  } = useCheckpoint();

  return (
    <Screen>
      <AppHeader title="Connect" subtitle={`Link: ${linkState}`} />
      <View className="gap-4 pb-6">
        <Card>
          <CardHeader>
            <CardTitle>Pendant</CardTitle>
          </CardHeader>
          <CardContent className="gap-3">
            <Text variant="muted">Device name or address</Text>
            <Input
              value={deviceName}
              onChangeText={setDeviceName}
              editable={!connected && !busy}
              autoCapitalize="none"
            />
            <Text variant="muted">Claim key (enroll only, 64 hex or claim URI)</Text>
            <Input
              value={claimText}
              onChangeText={setClaimText}
              editable={!connected && !busy}
              autoCapitalize="none"
              autoCorrect={false}
              secureTextEntry
              placeholder="Hold the pendant button 5s, then connect"
            />
            <View className="flex-row gap-2">
              <Button
                className="flex-1"
                disabled={connected || busy}
                onPress={() => void connect()}
              >
                <Text>{busy ? "Connecting…" : "Connect"}</Text>
              </Button>
              <Button
                variant="outline"
                className="flex-1"
                disabled={!connected}
                onPress={() => void disconnect()}
              >
                <Text>Disconnect</Text>
              </Button>
            </View>
            {needsSettings ? (
              <Button variant="outline" onPress={() => void openAppSettings()}>
                <Text>Open Settings</Text>
              </Button>
            ) : null}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Options</CardTitle>
          </CardHeader>
          <CardContent>
            <Toggle
              label="Upload to ingestion"
              value={settings.ingestEnabled}
              onChange={(next) => void updateSettings({ ...settings, ingestEnabled: next })}
            />
            <Toggle
              label="Voice-activity gate"
              value={settings.vadEnabled}
              onChange={(next) => void updateSettings({ ...settings, vadEnabled: next })}
            />
            <Toggle
              label="Keep files on device after upload"
              value={settings.keepFiles}
              onChange={(next) => void updateSettings({ ...settings, keepFiles: next })}
            />
          </CardContent>
        </Card>
        <LogView logs={logs} onClear={clearLogs} />
      </View>
    </Screen>
  );
}
