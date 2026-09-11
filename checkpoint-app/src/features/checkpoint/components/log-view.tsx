import { useCallback, useRef } from 'react';
import {
  Pressable,
  ScrollView,
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
    const distanceFromBottom =
      contentSize.height - (contentOffset.y + layoutMeasurement.height);
    nearBottom.current = distanceFromBottom <= NEAR_BOTTOM_PX;
  }, []);

  const handleContentSizeChange = useCallback(() => {
    if (nearBottom.current) scrollRef.current?.scrollToEnd({ animated: false });
  }, []);

  return (
    <Collapsible title="Debug log">
      <ScrollView
        ref={scrollRef}
        nestedScrollEnabled
        className="bg-input-bg h-40 p-2"
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
      <Pressable onPress={onClear} accessibilityRole="button" className="self-start">
        <Text className="font-display text-primary text-xs">Clear</Text>
      </Pressable>
    </Collapsible>
  );
}
