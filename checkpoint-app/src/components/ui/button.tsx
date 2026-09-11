import { TextClassContext } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { cva, type VariantProps } from 'class-variance-authority';
import { Platform, Pressable } from 'react-native';

const buttonVariants = cva(
  cn(
    'group font-display shrink-0 flex-row items-center justify-start gap-1.5 rounded-none',
    Platform.select({
      web: 'outline-none transition-colors',
    })
  ),
  {
    variants: {
      variant: {
        default: cn(
          'bg-primary text-primary-foreground active:bg-accent-600',
          Platform.select({ web: 'hover:bg-accent-600' })
        ),
        destructive: cn(
          'bg-destructive text-destructive-foreground active:bg-accent-800',
          Platform.select({ web: 'hover:bg-accent-800' })
        ),
        outline: cn(
          'border-border text-foreground active:bg-foreground/10 border bg-transparent',
          Platform.select({ web: 'hover:bg-foreground/5' })
        ),
        secondary: cn(
          'bg-secondary text-secondary-foreground active:bg-neutral-300',
          Platform.select({ web: 'hover:bg-neutral-300' })
        ),
        ghost: cn(
          'text-primary active:bg-primary/10',
          Platform.select({ web: 'hover:bg-primary/10' })
        ),
        link: 'text-primary p-0',
      },
      size: {
        default: 'h-10 px-3.5 py-2',
        sm: 'h-9 gap-1.5 px-3',
        lg: 'h-11 px-4',
        icon: 'h-10 w-10 justify-center p-0',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
);

const buttonTextVariants = cva('font-display text-sm', {
  variants: {
    variant: {
      default: 'text-primary-foreground',
      destructive: 'text-destructive-foreground',
      outline: 'text-foreground',
      secondary: 'text-secondary-foreground',
      ghost: 'text-primary',
      link: 'text-primary underline-offset-4',
    },
    size: {
      default: '',
      sm: '',
      lg: '',
      icon: '',
    },
  },
  defaultVariants: {
    variant: 'default',
    size: 'default',
  },
});

type ButtonProps = React.ComponentProps<typeof Pressable> &
  React.RefAttributes<typeof Pressable> &
  VariantProps<typeof buttonVariants>;

function Button({ className, variant, size, ...props }: ButtonProps) {
  return (
    <TextClassContext.Provider value={buttonTextVariants({ variant, size })}>
      <Pressable
        className={cn(props.disabled && 'opacity-45', buttonVariants({ variant, size }), className)}
        role="button"
        {...props}
      />
    </TextClassContext.Provider>
  );
}

export { Button, buttonTextVariants, buttonVariants };
export type { ButtonProps };
