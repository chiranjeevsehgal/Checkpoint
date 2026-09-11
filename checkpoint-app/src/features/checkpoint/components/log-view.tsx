import { Pressable, ScrollView } from 'react-native';

import { Collapsible } from '@/components/shared/collapsible';
import { Text } from '@/components/ui/text';

interface LogViewProps {
  logs: string[];
  onClear: () => void;
}

export function LogView({ logs, onClear }: LogViewProps) {
  return (
    <Collapsible title="Debug log">
      <ScrollView className="bg-input-bg max-h-40 p-2">
        {logs.length === 0 ? (
          <Text variant="muted" className="text-[11px]">
            No logs yet.
          </Text>
        ) : (
          logs.map((line, index) => (
            <Text key={`${index}-${line.slice(0, 24)}`} className="font-mono text-[11px]">
              {line}
            </Text>
          ))
        )}
      </ScrollView>
      <Pressable onPress={onClear} accessibilityRole="button" className="self-start">
        <Text className="font-display text-primary text-xs">Clear</Text>
      </Pressable>
    </Collapsible>
  );
}
