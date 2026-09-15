import { useRouter } from 'expo-router';
import { useState } from 'react';
import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { PasswordInput } from '@/components/ui/password-input';
import { Text } from '@/components/ui/text';
import { describeAuthError } from '@/features/auth/auth-errors';
import { useAuth } from '@/features/auth/hooks/useAuth';
import { passwordMeetsLength } from '@/features/auth/password-policy';
import { PasswordRules } from '@/features/auth/password-rules';

export function SignUpScreen() {
  const router = useRouter();
  const { signUp } = useAuth();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await signUp(name.trim(), email.trim(), password);
      router.replace('/(public)/verify-email');
    } catch (err) {
      setError(
        describeAuthError(
          err,
          'We could not create your account. Check your details and try again.',
        ),
      );
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
              placeholder="Name"
              autoCapitalize="words"
              autoComplete="name"
              maxLength={100}
              value={name}
              onChangeText={setName}
              editable={!busy}
            />
            <Input
              placeholder="Email"
              keyboardType="email-address"
              autoCapitalize="none"
              autoComplete="email"
              value={email}
              onChangeText={setEmail}
              editable={!busy}
            />
            <PasswordInput
              placeholder="Password"
              autoComplete="new-password"
              value={password}
              onChangeText={setPassword}
              editable={!busy}
            />
            <PasswordRules password={password} />
            {error ? <Text className="text-destructive">{error}</Text> : null}
            <Button
              onPress={() => void submit()}
              disabled={busy || !name.trim() || !email || !passwordMeetsLength(password)}
            >
              <Text>{busy ? 'Creating…' : 'Sign up'}</Text>
            </Button>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
