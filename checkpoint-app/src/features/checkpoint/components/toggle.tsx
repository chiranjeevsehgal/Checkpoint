import { Pressable, View } from "react-native";

import { Text } from "@/components/ui/text";
import { cn } from "@/lib/utils";

interface ToggleProps {
  label: string;
  value: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
}

export function Toggle({ label, value, onChange, disabled }: ToggleProps) {
  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityState={{ checked: value, disabled }}
      disabled={disabled}
      onPress={() => onChange(!value)}
      className={cn("flex-row items-center gap-3 py-2", disabled && "opacity-50")}
    >
      <View
        className={cn(
          "h-7 w-12 items-center rounded-full px-1",
          value ? "bg-primary items-end" : "bg-muted items-start",
        )}
      >
        <View className="h-5 w-5 rounded-full bg-background shadow-sm shadow-black/20" />
      </View>
      <Text className="flex-1">{label}</Text>
    </Pressable>
  );
}
