import { useState } from 'react';
import { ScrollView } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { useAuth } from '@/features/auth/hooks/useAuth';
import { deleteAccount } from '@/lib/api/account-api';
import { ApiError } from '@/lib/api/api-client';
import { getSessionToken } from '@/lib/session';

export function AccountScreen() {
  const { email, status, changePassword, signOut, signOutEverywhere, signIn } = useAuth();
  const [newPassword, setNewPassword] = useState('');
  const [reauthPassword, setReauthPassword] = useState('');
  const [needsReauth, setNeedsReauth] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function change() {
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await changePassword(email, newPassword);
      setNewPassword('');
      setMessage('Password updated. Other sessions were signed out.');
    } catch {
      setError('Could not change the password.');
    } finally {
      setBusy(false);
    }
  }

  async function deleteWithReauth(
    token: string,
    password?: string,
  ): Promise<'done' | 'needs-password'> {
    try {
      await deleteAccount(token);
      return 'done';
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 403 || err.code !== 'REAUTH_REQUIRED') {
        throw err;
      }
      if (!password) return 'needs-password';
      await signIn(email ?? '', password);
      const fresh = getSessionToken();
      if (!fresh) throw new Error('Re-authentication failed.');
      await deleteAccount(fresh);
      return 'done';
    }
  }

  async function removeAccount(password?: string) {
    setBusy(true);
    setError(null);
    try {
      const token = getSessionToken();
      if (!token) throw new Error('Not signed in.');
      if ((await deleteWithReauth(token, password)) === 'needs-password') {
        setNeedsReauth(true);
        return;
      }
      await signOut();
    } catch {
      setError('Could not delete the account.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen className="px-6">
      <ScrollView
        contentContainerStyle={{ paddingVertical: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <Card>
          <CardHeader>
            <CardTitle>Account</CardTitle>
            <CardDescription>{email ?? 'Signed in'}</CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            <Text className="text-muted-foreground">
              {status === 'authenticated' ? 'Email verified' : 'Email not verified'}
            </Text>
            <Input
              placeholder="New password"
              secureTextEntry
              value={newPassword}
              onChangeText={setNewPassword}
              editable={!busy}
            />
            <Button onPress={() => void change()} disabled={busy || !newPassword}>
              <Text>Change password</Text>
            </Button>
            {message ? <Text className="text-primary-text">{message}</Text> : null}
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardContent className="gap-4 pt-6">
            <Button variant="outline" onPress={() => void signOut()} disabled={busy}>
              <Text>Sign out</Text>
            </Button>
            <Button variant="outline" onPress={() => void signOutEverywhere()} disabled={busy}>
              <Text>Sign out everywhere</Text>
            </Button>
          </CardContent>
        </Card>

        <Card className="mt-4">
          <CardContent className="gap-4 pt-6">
            {needsReauth ? (
              <>
                <Text className="text-muted-foreground">
                  Confirm your password to delete the account.
                </Text>
                <Input
                  placeholder="Password"
                  secureTextEntry
                  value={reauthPassword}
                  onChangeText={setReauthPassword}
                  editable={!busy}
                />
                <Button
                  variant="destructive"
                  onPress={() => void removeAccount(reauthPassword)}
                  disabled={busy || !reauthPassword}
                >
                  <Text>Delete account</Text>
                </Button>
              </>
            ) : (
              <Button variant="destructive" onPress={() => void removeAccount()} disabled={busy}>
                <Text>Delete account</Text>
              </Button>
            )}
            {error ? <Text className="text-destructive">{error}</Text> : null}
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
