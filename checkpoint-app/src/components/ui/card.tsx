import { View } from 'react-native';

import { Text, TextClassContext } from '@/components/ui/text';
import { cn } from '@/lib/utils';

function Card({
  className,
  ...props
}: React.ComponentProps<typeof View> & React.RefAttributes<View>) {
  return (
    <TextClassContext.Provider value="text-card-foreground">
      <View
        className={cn('flex flex-col gap-3 rounded-none bg-surface p-4', className)}
        {...props}
      />
    </TextClassContext.Provider>
  );
}

function CardHeader({
  className,
  ...props
}: React.ComponentProps<typeof View> & React.RefAttributes<View>) {
  return <View className={cn('flex flex-col gap-1.5', className)} {...props} />;
}

function CardKicker({
  className,
  ...props
}: React.ComponentProps<typeof Text> & React.RefAttributes<typeof Text>) {
  return <Text variant="kicker" className={cn(className)} {...props} />;
}

function CardTitle({
  className,
  ...props
}: React.ComponentProps<typeof Text> & React.RefAttributes<typeof Text>) {
  return (
    <Text
      role="heading"
      aria-level={3}
      className={cn('font-display text-[17px] leading-tight', className)}
      {...props}
    />
  );
}

function CardDescription({
  className,
  ...props
}: React.ComponentProps<typeof Text> & React.RefAttributes<typeof Text>) {
  return <Text className={cn('text-[13px] text-muted-foreground', className)} {...props} />;
}

function CardContent({
  className,
  ...props
}: React.ComponentProps<typeof View> & React.RefAttributes<View>) {
  return <View className={cn('gap-3', className)} {...props} />;
}

function CardFooter({
  className,
  ...props
}: React.ComponentProps<typeof View> & React.RefAttributes<View>) {
  return <View className={cn('flex flex-row items-center gap-2', className)} {...props} />;
}

export { Card, CardContent, CardDescription, CardFooter, CardHeader, CardKicker, CardTitle };
