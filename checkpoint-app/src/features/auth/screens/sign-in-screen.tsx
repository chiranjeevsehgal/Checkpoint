import { View } from 'react-native';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';

export function SignInScreen() {
  return (
    <View className="flex-1 justify-center bg-background px-6">
      <Card>
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>Welcome back to Checkpoint.</CardDescription>
        </CardHeader>
        <CardContent className="gap-4">
          <Input placeholder="Email" keyboardType="email-address" autoCapitalize="none" />
          <Input placeholder="Password" secureTextEntry />
          <Button>
            <Text>Sign in</Text>
          </Button>
        </CardContent>
      </Card>
    </View>
  );
}
