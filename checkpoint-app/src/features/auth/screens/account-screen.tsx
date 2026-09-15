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
import { deleteAccount } from '@/lib/api/account-api';
import { getSessionToken } from '@/lib/session';

export function AccountScreen() {
  const {
    email,
    name,
    status,
    changePassword,
    signOut,
    signOutEverywhere,
    requestEmailVerification,
    confirmEmailVerification,
  } = useAuth();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [deleteCode, setDeleteCode] = useState('');
  const [deleteCodeSent, setDeleteCodeSent] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function change() {
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await changePassword(email, currentPassword, newPassword);
      setCurrentPassword('');
      setNewPassword('');
      setMessage('Password updated. Other sessions were signed out.');
    } catch (err) {
      setError(describeAuthError(err, 'We could not change your password. Please try again.'));
    } finally {
      setBusy(false);
    }
  }

  async function sendDeleteCode() {
    setBusy(true);
    setError(null);
    try {
      await requestEmailVerification(email ?? '');
      setDeleteCodeSent(true);
    } catch (err) {
      setError(describeAuthError(err, 'We could not send a confirmation code. Please try again.'));
    } finally {
      setBusy(false);
    }
  }

  async function removeAccount() {
    setBusy(true);
    setError(null);
    try {
      await confirmEmailVerification(deleteCode.trim());
      const token = getSessionToken();
      if (!token) throw new Error('Not signed in.');
      await deleteAccount(token);
      await signOut();
    } catch (err) {
      setError(describeAuthError(err, 'We could not delete your account. Please try again.'));
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
            <CardDescription>{name ?? email ?? 'Signed in'}</CardDescription>
          </CardHeader>
          <CardContent className="gap-4">
            <Text className="text-muted-foreground">
              {status === 'authenticated' ? 'Email verified' : 'Email not verified'}
            </Text>
            <PasswordInput
              placeholder="Current password"
              autoComplete="current-password"
              value={currentPassword}
              onChangeText={setCurrentPassword}
              editable={!busy}
            />
            <PasswordInput
              placeholder="New password"
              autoComplete="new-password"
              value={newPassword}
              onChangeText={setNewPassword}
              editable={!busy}
            />
            <PasswordRules password={newPassword} />
            <Button
              onPress={() => void change()}
              disabled={busy || !currentPassword || !passwordMeetsLength(newPassword)}
            >
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
            {!deleteCodeSent ? (
              <Button variant="destructive" onPress={() => void sendDeleteCode()} disabled={busy}>
                <Text>Delete account</Text>
              </Button>
            ) : (
              <>
                <Text className="text-muted-foreground">
                  Enter the code we emailed to {email} to confirm deletion.
                </Text>
                <Input
                  placeholder="Confirmation code"
                  keyboardType="number-pad"
                  value={deleteCode}
                  onChangeText={setDeleteCode}
                  editable={!busy}
                />
                <Button
                  variant="destructive"
                  onPress={() => void removeAccount()}
                  disabled={busy || !deleteCode}
                >
                  <Text>Confirm deletion</Text>
                </Button>
                <Button variant="outline" onPress={() => void sendDeleteCode()} disabled={busy}>
                  <Text>Resend code</Text>
                </Button>
              </>
            )}
            {error ? <Text className="text-destructive">{error}</Text> : null}
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
