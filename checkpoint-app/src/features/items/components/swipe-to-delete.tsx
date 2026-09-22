import { Trash2 } from 'lucide-react-native';
import type { ReactNode } from 'react';
import { Pressable } from 'react-native';
import Swipeable from 'react-native-gesture-handler/ReanimatedSwipeable';

import { Icon } from '@/components/ui/icon';

/** A row that reveals a destructive action when swiped right-to-left. */
export function SwipeToDelete({
  onDelete,
  label,
  children,
}: {
  onDelete: () => void;
  label: string;
  children: ReactNode;
}) {
  return (
    <Swipeable
      friction={2}
      rightThreshold={40}
      overshootRight={false}
      renderRightActions={(_, __, methods) => (
        <Pressable
          onPress={() => {
            methods.close();
            onDelete();
          }}
          accessibilityRole="button"
          accessibilityLabel={label}
          className="w-20 items-center justify-center bg-destructive active:opacity-80"
        >
          <Icon as={Trash2} size={18} className="text-destructive-foreground" />
        </Pressable>
      )}
    >
      {children}
    </Swipeable>
  );
}
