import * as Clipboard from 'expo-clipboard';
import * as Device from 'expo-device';
import { useFocusEffect, useRouter } from 'expo-router';
import { useCallback, useMemo, useState } from 'react';
import { Platform, ScrollView, View } from 'react-native';

import { AppHeader } from '@/components/shared/app-header';
import { ConfirmDialog } from '@/components/shared/confirm-dialog';
import { EmptyState } from '@/components/shared/empty-state';
import { Screen } from '@/components/shared/screen';
import { Section } from '@/components/shared/section';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Text } from '@/components/ui/text';
import {
  claudeCodeCommand,
  defaultKeyName,
  formatDateTime,
  mcpJsonSnippet,
  normalizeKeyName,
} from '@/features/auth/mcpKeysView';
import { ApiError } from '@/lib/api/api-client';
import {
  createMcpKey,
  listMcpKeys,
  revokeMcpKey,
  type CreatedMcpKey,
  type McpKey,
} from '@/lib/api/mcp-keys-api';
import { resolveMcpUrl } from '@/lib/server-config';
import { getSessionToken } from '@/lib/session';
import { useToast } from '@/providers/toast-provider';

type Copy = (value: string, label: string) => void;

function describeError(error: unknown): string {
  if (error instanceof ApiError && error.status === 401) {
    return 'Your session expired. Sign in again.';
  }
  return 'Something went wrong. Check your connection and try again.';
}

function Snippet({ label, value, onCopy }: { label: string; value: string; onCopy: Copy }) {
  return (
    <View className="gap-1">
      <View className="flex-row items-center justify-between">
        <Text variant="kicker">{label}</Text>
        <Button variant="ghost" size="sm" onPress={() => onCopy(value, label)}>
          <Text>Copy</Text>
        </Button>
      </View>
      <Text selectable className="font-mono text-[11px]">
        {value}
      </Text>
    </View>
  );
}

function RevealCard({
  endpoint,
  created,
  onCopy,
}: {
  endpoint: string;
  created: CreatedMcpKey;
  onCopy: Copy;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Your new key</CardTitle>
        <CardDescription>Copy it now — the server never shows it again.</CardDescription>
      </CardHeader>
      <CardContent className="gap-4">
        <Snippet label="Access key" value={created.key} onCopy={onCopy} />
        <Snippet
          label="Claude Code"
          value={claudeCodeCommand(endpoint, created.key)}
          onCopy={onCopy}
        />
        <Snippet label="mcp.json" value={mcpJsonSnippet(endpoint, created.key)} onCopy={onCopy} />
      </CardContent>
    </Card>
  );
}

function ConnectorCard({ endpoint, onCopy }: { endpoint: string; onCopy: Copy }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Connect Claude or ChatGPT</CardTitle>
        <CardDescription>
          Add this as a custom connector. You sign in with your Checkpoint account — no key needed.
        </CardDescription>
      </CardHeader>
      <CardContent className="gap-4">
        <Snippet label="Connector URL" value={endpoint} onCopy={onCopy} />
      </CardContent>
    </Card>
  );
}

function KeyRow({ item, onRevoke }: { item: McpKey; onRevoke: (item: McpKey) => void }) {
  return (
    <Card>
      <View className="flex-row items-start justify-between gap-2">
        <View className="flex-1 gap-0.5">
          <Text className="font-display text-[15px]">{item.name}</Text>
          <Text variant="muted" className="font-mono text-[11px]">
            {item.prefix}…
          </Text>
          <Text variant="muted" className="text-[11px]">
            Created {formatDateTime(item.created_at)}
          </Text>
          <Text variant="muted" className="text-[11px]">
            Last used {formatDateTime(item.last_used_at)}
          </Text>
        </View>
        <Button variant="outline" size="sm" onPress={() => onRevoke(item)}>
          <Text>Revoke</Text>
        </Button>
      </View>
    </Card>
  );
}

export function McpKeysScreen() {
  const router = useRouter();
  const { showToast } = useToast();
  const endpoint = useMemo(() => resolveMcpUrl(), []);
  const [keys, setKeys] = useState<McpKey[] | null>(null);
  const [name, setName] = useState(() => defaultKeyName(Device.modelName, Platform.OS));
  const [created, setCreated] = useState<CreatedMcpKey | null>(null);
  const [pendingRevoke, setPendingRevoke] = useState<McpKey | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const token = getSessionToken();
    if (!token) return;
    try {
      setKeys(await listMcpKeys(token));
      setError(null);
    } catch (err) {
      setError(describeError(err));
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      void load();
    }, [load]),
  );

  const copy = useCallback<Copy>(
    (value, label) => {
      void Clipboard.setStringAsync(value);
      showToast(`${label} copied`);
    },
    [showToast],
  );

  const create = useCallback(async () => {
    const token = getSessionToken();
    const trimmed = normalizeKeyName(name);
    if (!token || !trimmed) return;
    setBusy(true);
    setError(null);
    try {
      const result = await createMcpKey(token, trimmed);
      setCreated(result);
      setName(defaultKeyName(Device.modelName, Platform.OS));
      await load();
      showToast('Key created');
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  }, [name, load, showToast]);

  const revoke = useCallback(async () => {
    const target = pendingRevoke;
    const token = getSessionToken();
    if (!target || !token) return;
    try {
      await revokeMcpKey(token, target.id);
      setPendingRevoke(null);
      showToast('Key revoked');
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }, [pendingRevoke, load, showToast]);

  return (
    <Screen className="px-6">
      <AppHeader title="MCP access" subtitle="Connect AI assistants" onBack={() => router.back()} />
      <ScrollView
        className="flex-1"
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ gap: 24, paddingBottom: 24 }}
        showsVerticalScrollIndicator={false}
      >
        <Section title="AI apps">
          <ConnectorCard endpoint={endpoint} onCopy={copy} />
        </Section>

        <Section title="New CLI key">
          <Card>
            <Input
              value={name}
              onChangeText={setName}
              placeholder="Key name"
              autoCapitalize="words"
              autoCorrect={false}
              editable={!busy}
            />
            <Text variant="muted" className="text-[11px]">
              A key can read your transcripts, todos, reminders, insights and summaries. Revoke any
              key that leaks.
            </Text>
            <Button
              onPress={() => void create()}
              disabled={busy || normalizeKeyName(name) === null}
            >
              <Text>{busy ? 'Creating…' : 'Create key'}</Text>
            </Button>
          </Card>
        </Section>

        {created ? <RevealCard endpoint={endpoint} created={created} onCopy={copy} /> : null}

        <Section title="Your keys">
          {keys === null && !error ? (
            <Text variant="muted">Loading…</Text>
          ) : keys !== null && keys.length === 0 ? (
            <EmptyState
              title="No keys yet"
              hint="Create a key to connect Claude Code, Cursor or another MCP client."
            />
          ) : keys !== null ? (
            <View className="gap-3">
              {keys.map((key) => (
                <KeyRow key={key.id} item={key} onRevoke={(item) => setPendingRevoke(item)} />
              ))}
            </View>
          ) : null}
        </Section>

        {error ? <Text className="text-destructive">{error}</Text> : null}
      </ScrollView>

      <ConfirmDialog
        visible={pendingRevoke !== null}
        title="Revoke key?"
        body={`"${pendingRevoke?.name ?? ''}" stops working immediately for any connected client.`}
        confirmLabel="Revoke"
        onCancel={() => setPendingRevoke(null)}
        onConfirm={revoke}
      />
    </Screen>
  );
}
