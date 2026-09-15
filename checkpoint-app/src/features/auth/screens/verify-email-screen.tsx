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
  const { email, status, requestEmailVerification, confirmEmailVerification } = useAuth();
  const [address, setAddress] = useState(email ?? '');
  const [code, setCode] = useState('');
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

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
            <Input
              placeholder="Email"
              keyboardType="email-address"
              autoCapitalize="none"
              value={address}
              onChangeText={setAddress}
              editable={!busy && !sent}
            />
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
                {error ? <Text className="text-destructive">{error}</Text> : null}
                <Button onPress={() => void confirm()} disabled={busy || !code}>
                  <Text>{busy ? 'Verifying…' : 'Verify'}</Text>
                </Button>
              </>
            )}
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
