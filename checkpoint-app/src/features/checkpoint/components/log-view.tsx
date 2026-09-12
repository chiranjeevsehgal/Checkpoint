import * as Clipboard from 'expo-clipboard';
import { useCallback, useRef } from 'react';
import {
  Pressable,
  ScrollView,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native';

import { Collapsible } from '@/components/shared/collapsible';
import { Text } from '@/components/ui/text';

interface LogViewProps {
  logs: string[];
  onClear: () => void;
}

const NEAR_BOTTOM_PX = 24;

export function LogView({ logs, onClear }: LogViewProps) {
  const scrollRef = useRef<ScrollView>(null);
  const nearBottom = useRef(true);

  const handleScroll = useCallback((event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
    const distanceFromBottom = contentSize.height - (contentOffset.y + layoutMeasurement.height);
    nearBottom.current = distanceFromBottom <= NEAR_BOTTOM_PX;
  }, []);

  const handleContentSizeChange = useCallback(() => {
    if (nearBottom.current) scrollRef.current?.scrollToEnd({ animated: false });
  }, []);

  const copyLogs = useCallback(() => {
    void Clipboard.setStringAsync(logs.join('\n'));
  }, [logs]);

  return (
    <Collapsible title="Debug log">
      <ScrollView
        ref={scrollRef}
        nestedScrollEnabled
        className="h-52 bg-input-bg p-2"
        contentContainerStyle={{ paddingBottom: 8 }}
        onScroll={handleScroll}
        scrollEventThrottle={16}
        onContentSizeChange={handleContentSizeChange}
      >
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
      <View className="flex-row gap-3">
        <Pressable onPress={copyLogs} accessibilityRole="button" className="self-start">
          <Text className="font-display text-xs text-primary-text">Copy logs</Text>
        </Pressable>
        <Pressable onPress={onClear} accessibilityRole="button" className="self-start">
          <Text className="font-display text-xs text-primary-text">Clear</Text>
        </Pressable>
      </View>
    </Collapsible>
  );
}
