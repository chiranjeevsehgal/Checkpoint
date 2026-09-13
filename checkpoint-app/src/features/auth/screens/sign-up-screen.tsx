import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';

export function SignUpScreen() {
  return (
    <Screen className="px-6">
      <ScrollView
        className="flex-1"
        contentContainerStyle={{ flexGrow: 1, justifyContent: 'center' }}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        <Card>
          <CardHeader>
            <CardTitle>Create account</CardTitle>
            <CardDescription>Get started with Checkpoint.</CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            <Input placeholder="Email" keyboardType="email-address" autoCapitalize="none" />
            <Input placeholder="Password" secureTextEntry />
            <Button>
              <Text>Sign up</Text>
            </Button>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
