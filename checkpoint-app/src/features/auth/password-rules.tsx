import { Check, Circle, X } from 'lucide-react-native';
import { View } from 'react-native';

import { Icon } from '@/components/ui/icon';
import { Text } from '@/components/ui/text';
import {
  PASSWORD_MIN_LENGTH,
  PASSWORD_STATIC_RULES,
  passwordMeetsLength,
} from '@/features/auth/password-policy';

export function PasswordRules({ password }: { password: string }) {
  const lengthMet = passwordMeetsLength(password);

  return (
    <View className="gap-1">
      <View className="flex-row items-center gap-2">
        <Icon
          as={lengthMet ? Check : X}
          size={12}
          className={lengthMet ? 'text-primary-text' : 'text-muted-foreground'}
        />
        <Text className="text-[12px] text-muted-foreground">
          At least {PASSWORD_MIN_LENGTH} characters
        </Text>
      </View>
      {PASSWORD_STATIC_RULES.map((rule) => (
        <View key={rule} className="flex-row items-center gap-2">
          <Icon as={Circle} size={6} className="text-muted-foreground" />
          <Text className="text-[12px] text-muted-foreground">{rule}</Text>
        </View>
      ))}
    </View>
  );
}
