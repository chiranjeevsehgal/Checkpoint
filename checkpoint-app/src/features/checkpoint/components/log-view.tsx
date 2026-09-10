import { ScrollView } from "react-native";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Text } from "@/components/ui/text";

interface LogViewProps {
  logs: string[];
  onClear: () => void;
}

export function LogView({ logs, onClear }: LogViewProps) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle>Logs</CardTitle>
        <Button variant="ghost" size="sm" onPress={onClear}>
          <Text>Clear</Text>
        </Button>
      </CardHeader>
      <CardContent>
        <ScrollView className="max-h-64">
          {logs.length === 0 ? (
            <Text variant="muted">No logs yet.</Text>
          ) : (
            logs.map((line, index) => (
              <Text key={`${index}-${line.slice(0, 24)}`} className="font-mono text-xs">
                {line}
              </Text>
            ))
          )}
        </ScrollView>
      </CardContent>
    </Card>
  );
}
