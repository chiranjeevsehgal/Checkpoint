import { Link } from 'expo-router';
import { useState } from 'react';
import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { useAuth } from '@/features/auth/hooks/useAuth';

export function SignInScreen() {
  const { signIn } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await signIn(email.trim(), password);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign in failed.');
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
            <CardTitle>Sign in</CardTitle>
            <CardDescription>Welcome back to Checkpoint.</CardDescription>
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
              <Text>{busy ? 'Signing in…' : 'Sign in'}</Text>
            </Button>
            <Link href="/(public)/sign-up" className="text-center">
              <Text className="text-primary-text">Create an account</Text>
            </Link>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
