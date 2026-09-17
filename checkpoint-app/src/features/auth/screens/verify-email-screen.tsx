import { useRouter } from 'expo-router';
import { useState } from 'react';
import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { describeAuthError } from '@/features/auth/auth-errors';
import { useAuth } from '@/features/auth/hooks/useAuth';

export function VerifyEmailScreen() {
  const router = useRouter();
  const {
    email,
    status,
    requestEmailVerification,
    confirmEmailVerification,
    hasPendingVerification,
    signOut,
  } = useAuth();
  const [manualEmail, setManualEmail] = useState('');
  const [code, setCode] = useState('');
  const [sent, setSent] = useState(hasPendingVerification);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const address = email ?? manualEmail;

  async function send() {
    setBusy(true);
    setError(null);
    try {
      await requestEmailVerification(address.trim());
      setSent(true);
    } catch (err) {
      setError(describeAuthError(err, 'We could not send a verification code. Please try again.'));
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await confirmEmailVerification(code.trim());
    } catch (err) {
      setError(describeAuthError(err, 'That code was not accepted. Try again.'));
    } finally {
      setBusy(false);
    }
  }

  async function startOver() {
    setBusy(true);
    await signOut();
    router.replace('/(public)/sign-up');
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
            <CardTitle>Verify your email</CardTitle>
            <CardDescription>
              {status === 'authenticated'
                ? 'Your email is already verified.'
                : 'Enter the code we email you to unlock Checkpoint.'}
            </CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            {email ? (
              <Text className="text-muted-foreground">
                We send the code to {email}. Unverified signups are removed automatically, so verify
                soon.
              </Text>
            ) : (
              <Input
                placeholder="Email"
                keyboardType="email-address"
                autoCapitalize="none"
                value={manualEmail}
                onChangeText={setManualEmail}
                editable={!busy && !sent}
              />
            )}
            {!sent ? (
              <Button onPress={() => void send()} disabled={busy || !address}>
                <Text>{busy ? 'Sending…' : 'Send code'}</Text>
              </Button>
            ) : (
              <>
                <Input
                  placeholder="Verification code"
                  keyboardType="number-pad"
                  value={code}
                  onChangeText={setCode}
                  editable={!busy}
                />
                <Button onPress={() => void confirm()} disabled={busy || !code}>
                  <Text>{busy ? 'Verifying…' : 'Verify'}</Text>
                </Button>
                <Button variant="outline" onPress={() => void send()} disabled={busy}>
                  <Text>Resend code</Text>
                </Button>
              </>
            )}
            {error ? <Text className="text-destructive">{error}</Text> : null}
            <Button variant="outline" onPress={() => void startOver()} disabled={busy}>
              <Text>Use a different email</Text>
            </Button>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
