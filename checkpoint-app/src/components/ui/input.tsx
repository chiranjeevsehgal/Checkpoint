import { cn } from '@/lib/utils';
import { Platform, TextInput } from 'react-native';

function Input({
  className,
  ...props
}: React.ComponentProps<typeof TextInput> & React.RefAttributes<TextInput>) {
  return (
    <TextInput
      className={cn(
        'h-9 w-full min-w-0 rounded-none border border-border bg-input-bg px-2.5 py-1.5 font-sans text-[14px] text-foreground selection:text-primary-foreground',
        props.editable === false && 'opacity-45',
        Platform.select({
          web: 'outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-border-strong',
          native: 'placeholder:text-muted-foreground',
        }),
        className,
      )}
      {...props}
    />
  );
}

export { Input };
