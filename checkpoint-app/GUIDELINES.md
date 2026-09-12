# Checkpoint App Guidelines

Conventions for `checkpoint-app` (Expo + React Native + NativeWind). Read the versioned Expo docs at
https://docs.expo.dev/versions/v57.0.0/ before using any API.

## Layout

```
src/
  app/                 expo-router routes (thin: re-export screens)
  features/<name>/     screens/, components/, hooks/, __tests__
  components/ui/       design-system primitives
  components/shared/   app-wide components
  hooks/               shared hooks
  lib/                 env, api, storage, theme, utils
  providers/           React context providers
```

- One feature owns its screens, logic, and tests.
- Route files in `app/` stay one-liners that re-export a screen.
- Shared code (`components`, `lib`, `hooks`, `constants`, `types`) must not import a feature, and
  features must not import each other. Lint enforces this.

## Commands

```bash
npm run check    # typecheck + lint + format check — run before every commit
npm test         # unit tests (node:test)
npm run lint:fix
npm run format
npm run android
npm run build:android -- -PreactNativeArchitectures=arm64-v8a
```

## TypeScript

- `strict` and `noUncheckedIndexedAccess` are on. No `any`, no `@ts-ignore`.
- Use `import type` for type-only imports.
- Trust the types; don't add redundant null checks that lint flags.

## Style

- Prettier is the source of truth: single quotes, 100 columns, trailing commas (`npm run format`).
- Imports are grouped external → `@/` → relative, alphabetized with a blank line between groups;
  `npm run lint:fix` fixes order automatically.
- Files are `kebab-case`, components `PascalCase`, functions and variables `camelCase`.
- Comments explain non-obvious _why_, never _what_.

## UI

- Reuse primitives: `Text`, `Button`, `Card`, `Input`, `Icon`, `Screen` (`CheckpointScreen` in the
  checkpoint feature).
- Use theme classes (`bg-surface`, `text-muted-foreground`, `border-border-strong`) — never
  hardcoded hex.
- Semantic colors: brand coral = `primary`, completed = `success`, waiting = `warning`, failed or
  destructive = `destructive`. Status fills pair with `text-background`; do not use `primary` for
  delete/erase/error states.
- Text hierarchy: `foreground` → `muted-foreground` → `subtle-foreground`.

## Async and errors

- Never leave a floating promise: `await` it, `.catch` it, or mark it `void`.
- Handle errors; only swallow them for explicitly best-effort work (e.g. cache writes).
- Logging: `console.warn`, `console.error`, or `console.debug`. `console.log` is not allowed.

## State

- Keep state inside the feature; use a small typed store with `useSyncExternalStore`.
- No global state library.

## Testing

- Pure logic (parsers, crypto, protocol, stores) needs `node:test` tests in
  `features/<name>/__tests__/*.test.ts`.
- Add or update tests with every behavior change.

## Env and secrets

- Only `EXPO_PUBLIC_*` variables reach the app. Never put secrets in the app, repo, or `.env` files.
- Config resolution lives in `lib/env.ts`.

## Git and CI

- Commit small, scoped changes with a short one-line message.
- Never commit generated `android/`/`ios/` output or local `.env` files.
- CI runs `npm run check` and the tests; the nightly workflow builds and publishes the APK.
