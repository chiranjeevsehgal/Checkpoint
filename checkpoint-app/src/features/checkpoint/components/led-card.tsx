import { useState } from 'react';
import { View } from 'react-native';

import { useCheckpoint } from '../hooks/useCheckpoint.tsx';

import { Toggle } from './toggle.tsx';

import { Section } from '@/components/shared/section';
import { Card } from '@/components/ui/card';
import { RangeSlider } from '@/components/ui/slider';
import { Text } from '@/components/ui/text';

export function LedCard() {
  const { connected, status, applyLed } = useCheckpoint();
  const [draftBrightness, setDraftBrightness] = useState<number | null>(null);

  if (!connected) return null;

  const muted = status?.muted ?? false;
  const brightness = draftBrightness ?? status?.brightness ?? 30;

  return (
    <Section title="Status light">
      <Card>
        <Toggle
          label="Enabled"
          description="Light up the pendant status LED."
          value={!muted}
          onChange={(next) => void applyLed(!next, brightness)}
        />
        <View className="gap-1">
          <View className="flex-row justify-between">
            <Text className="text-[12px]">Brightness</Text>
            <Text className="font-mono text-[12px]">{brightness}</Text>
          </View>
          <RangeSlider
            min={5}
            max={255}
            step={1}
            value={brightness}
            accessibilityLabel="Brightness"
            onValueChange={setDraftBrightness}
            onSlidingComplete={(value) => {
              setDraftBrightness(null);
              void applyLed(muted, value);
            }}
          />
        </View>
      </Card>
    </Section>
  );
}
