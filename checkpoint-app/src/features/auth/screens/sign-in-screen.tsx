import { Link } from 'expo-router';
import { useState } from 'react';
import { ScrollView, View } from 'react-native';

import { Screen } from '@/components/shared/screen';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import { applyServerConfig } from '@/features/auth/auth-store';
import { useAuth } from '@/features/auth/hooks/useAuth';
import { env } from '@/lib/env';
import { getDevHost, setDevHost } from '@/lib/server-config';

export function SignInScreen() {
  const { signIn } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [devHost, setDevHostValue] = useState(getDevHost() ?? '');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      if (env.devBuild) {
        await setDevHost(devHost);
        applyServerConfig();
      }
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
            {env.devBuild ? (
              <View className="gap-1">
                <Text className="text-[11px] text-subtle-foreground">
                  Server host (dev — blank uses the build default)
                </Text>
                <Input
                  placeholder="192.168.1.5"
                  autoCapitalize="none"
                  autoCorrect={false}
                  className="font-mono text-[13px]"
                  value={devHost}
                  onChangeText={setDevHostValue}
                  editable={!busy}
                />
              </View>
            ) : null}
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
            <Link href="/(public)/recovery" className="text-center">
              <Text className="text-primary-text">Forgot password?</Text>
            </Link>
            <Link href="/(public)/sign-up" className="text-center">
              <Text className="text-primary-text">Create an account</Text>
            </Link>
          </CardContent>
        </Card>
      </ScrollView>
    </Screen>
  );
}
