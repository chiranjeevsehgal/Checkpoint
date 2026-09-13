import Slider from '@react-native-community/slider';
import { useColorScheme } from 'nativewind';

import { PALETTE } from '@/lib/theme';

interface RangeSliderProps {
  min: number;
  max: number;
  step: number;
  value: number;
  onValueChange: (value: number) => void;
  accessibilityLabel?: string;
  onSlidingComplete?: (value: number) => void;
}

export function RangeSlider({
  min,
  max,
  step,
  value,
  onValueChange,
  accessibilityLabel,
  onSlidingComplete,
}: RangeSliderProps) {
  const { colorScheme } = useColorScheme();
  const palette = colorScheme === 'dark' ? PALETTE.dark : PALETTE.light;

  return (
    <Slider
      minimumValue={min}
      maximumValue={max}
      step={step}
      value={value}
      onValueChange={onValueChange}
      onSlidingComplete={onSlidingComplete}
      accessibilityLabel={accessibilityLabel}
      accessibilityValue={{ min, max, now: value }}
      minimumTrackTintColor={palette.primary}
      maximumTrackTintColor={palette.divider}
      thumbTintColor={palette.primary}
    />
  );
}
