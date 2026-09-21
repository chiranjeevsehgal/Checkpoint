# checkpoint-app

Expo (React Native) pendant companion: BLE sync from the pendant, Kratos
auth, uploads to ingestion-service, reminders over self-hosted ntfy, MCP key
management.

## Run

```bash
npm install
npm run check   # typecheck + lint + format check
npm test        # node --test "src/**/__tests__/*.test.ts"
npx expo start  # EXPO_PUBLIC_API_URL / EXPO_PUBLIC_KRATOS_URL: LAN IP for hardware
```

## Auth

Kratos-native (no SDK): session token + identity in SecureStore.
`loading | anonymous | unverified | authenticated | unavailable |
reconnecting | deleting`. 401 clears the session, 503/`reconnecting` keeps
it (backoff retry on foreground); `VERIFICATION_REQUIRED` routes to
verify-email. Revealing an MCP key or confirming account deletion asks for
the device biometric/PIN first.

## Uploads

`Bearer <session token>` + owned `device_id`; the pendant is claimed via
`GET/POST /v1/device*` after BLE enrollment. AES-CCM uses the vetted
`@noble/ciphers` block primitive (the pure-TS CCM framing is covered by
cross-implementation vectors in `__tests__/crypto.test.ts`).

## Security notes

- `android.usesCleartextTraffic` is on for LAN development (Kratos/MinIO
  over plain HTTP). Release builds must disable it and pin certificates.
- Sign-out, deletion and 401 wipe local recordings; transient `unavailable`
  never does.
- No jailbreak/root detection yet.

## Config

`EXPO_PUBLIC_API_URL`, `EXPO_PUBLIC_KRATOS_URL` (default localhost:4433),
EAS project in `app.json`.
