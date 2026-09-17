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

export function RecoveryScreen() {
  const router = useRouter();
  const { requestPasswordRecovery, confirmPasswordRecovery } = useAuth();
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function send() {
    setBusy(true);
    setError(null);
    try {
      await requestPasswordRecovery(email.trim());
      setSent(true);
    } catch {
      // Always present the same outward result to avoid revealing accounts.
      setSent(true);
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await confirmPasswordRecovery(code.trim(), password);
      router.replace('/(public)/sign-in');
    } catch (err) {
      setError(describeAuthError(err, 'We could not reset your password. Please try again.'));
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
            <CardTitle>Reset password</CardTitle>
            <CardDescription>
              {sent
                ? 'Enter the code and choose a new password.'
                : 'We will email you a recovery code.'}
            </CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            {!sent ? (
              <>
                <Input
                  placeholder="Email"
                  keyboardType="email-address"
                  autoCapitalize="none"
                  value={email}
                  onChangeText={setEmail}
                  editable={!busy}
                />
                <Button onPress={() => void send()} disabled={busy || !email}>
                  <Text>{busy ? 'Sending…' : 'Send code'}</Text>
                </Button>
              </>
            ) : (
              <>
                <Input
                  placeholder="Recovery code"
                  keyboardType="number-pad"
                  value={code}
                  onChangeText={setCode}
                  editable={!busy}
                />
                <PasswordInput
                  placeholder="New password"
                  autoComplete="new-password"
                  value={password}
                  onChangeText={setPassword}
                  editable={!busy}
                />
                <PasswordRules password={password} />
                {error ? <Text className="text-destructive">{error}</Text> : null}
                <Button
                  onPress={() => void confirm()}
                  disabled={busy || !code || !passwordMeetsLength(password)}
                >
                  <Text>{busy ? 'Saving…' : 'Set new password'}</Text>
                </Button>
              </>
            )}
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
