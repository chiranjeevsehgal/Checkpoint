import { Eye, EyeOff } from 'lucide-react-native';
import { useState } from 'react';
import { View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Icon } from '@/components/ui/icon';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

type PasswordInputProps = React.ComponentProps<typeof Input> & {
  containerClassName?: string;
};

function PasswordInput({ className, containerClassName, ...props }: PasswordInputProps) {
  const [visible, setVisible] = useState(false);

  return (
    <View className={cn('flex-row gap-2', containerClassName)}>
      <Input {...props} secureTextEntry={!visible} className={cn('flex-1', className)} />
      <Button
        variant="ghost"
        size="icon"
        className="h-9 w-9"
        disabled={props.editable === false}
        onPress={() => setVisible((value) => !value)}
        accessibilityLabel={visible ? 'Hide password' : 'Show password'}
      >
        <Icon as={visible ? EyeOff : Eye} size={16} />
      </Button>
    </View>
  );
}

export { PasswordInput };
