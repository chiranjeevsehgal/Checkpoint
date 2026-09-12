import { cn } from '@/lib/utils';
import { Platform, TextInput } from 'react-native';

function Input({
  className,
  ...props
}: React.ComponentProps<typeof TextInput> & React.RefAttributes<TextInput>) {
  return (
    <TextInput
      className={cn(
        'bg-input-bg border-border text-foreground selection:text-primary-foreground h-9 w-full min-w-0 rounded-none border px-2.5 py-1.5 font-sans text-[14px]',
        props.editable === false && 'opacity-45',
        Platform.select({
          web: 'placeholder:text-muted-foreground focus-visible:border-border-strong outline-none transition-colors',
          native: 'placeholder:text-muted-foreground',
        }),
        className
      )}
      {...props}
    />
  );
}

export { Input };
