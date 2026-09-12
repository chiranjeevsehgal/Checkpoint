import { RefreshCw } from 'lucide-react-native';
import { useCallback, useMemo } from 'react';
import { Animated } from 'react-native';

import { Button } from '@/components/ui/button';
import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';

const SPIN_DURATION_MS = 600;

export function RefreshButton({
  onPress,
  disabled,
  label = 'Refresh',
}: {
  onPress: () => void;
  disabled?: boolean;
  label?: string;
}) {
  const spin = useMemo(() => new Animated.Value(0), []);

  const handlePress = useCallback(() => {
    spin.setValue(0);
    Animated.timing(spin, {
      toValue: 1,
      duration: SPIN_DURATION_MS,
      useNativeDriver: true,
    }).start();
    onPress();
  }, [onPress, spin]);

  const rotate = spin.interpolate({ inputRange: [0, 1], outputRange: ['0deg', '360deg'] });

  return (
    <Button variant="ghost" size="sm" disabled={disabled} onPress={handlePress}>
      <Animated.View style={{ transform: [{ rotate }] }}>
        <Icon as={RefreshCw} size={14} />
      </Animated.View>
      <Text>{label}</Text>
    </Button>
  );
}
