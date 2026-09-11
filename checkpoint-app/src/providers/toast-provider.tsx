import * as React from 'react';
import { Animated } from 'react-native';

import { Text } from '@/components/ui/text';

interface ToastContextValue {
  showToast: (message: string) => void;
}

const ToastContext = React.createContext<ToastContextValue | null>(null);

const TOAST_DURATION_MS = 2400;
const TOAST_ANIMATION_MS = 180;

export function useToast(): ToastContextValue {
  const value = React.useContext(ToastContext);
  if (!value) throw new Error('useToast must be used inside ToastProvider');
  return value;
}

export function ToastProvider({ children }: React.PropsWithChildren) {
  const [message, setMessage] = React.useState<string | null>(null);
  const progress = React.useMemo(() => new Animated.Value(0), []);
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTimer = React.useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const showToast = React.useCallback(
    (next: string) => {
      clearTimer();
      setMessage(next);
      progress.setValue(0);
      Animated.timing(progress, {
        toValue: 1,
        duration: TOAST_ANIMATION_MS,
        useNativeDriver: true,
      }).start();
      timerRef.current = setTimeout(() => {
        Animated.timing(progress, {
          toValue: 0,
          duration: TOAST_ANIMATION_MS,
          useNativeDriver: true,
        }).start(({ finished }) => {
          if (finished) setMessage(null);
        });
      }, TOAST_DURATION_MS);
    },
    [clearTimer, progress],
  );

  React.useEffect(() => clearTimer, [clearTimer]);

  const translateY = progress.interpolate({ inputRange: [0, 1], outputRange: [12, 0] });

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      {message ? (
        <Animated.View
          pointerEvents="none"
          style={{
            position: 'absolute',
            left: 0,
            right: 0,
            bottom: 78,
            alignItems: 'center',
            opacity: progress,
            transform: [{ translateY }],
          }}
        >
          <Text className="bg-foreground px-4 py-2 text-sm font-medium text-background">
            {message}
          </Text>
        </Animated.View>
      ) : null}
    </ToastContext.Provider>
  );
}
