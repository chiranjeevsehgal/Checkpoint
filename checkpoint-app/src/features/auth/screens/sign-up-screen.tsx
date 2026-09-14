import { useRouter } from 'expo-router';
import { useState } from 'react';
import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { useAuth } from '@/features/auth/hooks/useAuth';

export function SignUpScreen() {
  const router = useRouter();
  const { signUp } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await signUp(email.trim(), password);
      router.replace('/(public)/verify-email');
    } catch {
      setError('Could not create the account. Check your details and try again.');
    } finally {
      setBusy(false);
    }
  }

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
            <CardDescription>Start using Checkpoint.</CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            <Input
              placeholder="Email"
              keyboardType="email-address"
              autoCapitalize="none"
              autoComplete="email"
              value={email}
              onChangeText={setEmail}
              editable={!busy}
            />
            <Input
              placeholder="Password"
              secureTextEntry
              value={password}
              onChangeText={setPassword}
              editable={!busy}
            />
            {error ? <Text className="text-destructive">{error}</Text> : null}
            <Button onPress={() => void submit()} disabled={busy || !email || !password}>
              <Text>{busy ? 'Creating…' : 'Sign up'}</Text>
            </Button>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
