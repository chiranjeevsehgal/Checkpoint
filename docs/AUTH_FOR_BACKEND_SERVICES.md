# Authentication & tenancy for backend services

Ory Kratos is the only identity provider. There is no custom auth microservice: every
backend is a **resource server** that validates a Kratos session token and derives the
caller from it. `ingestion-service` is the reference implementation; copy its shape.

## Mental model

| Layer | Responsibility |
| --- | --- |
| Kratos (`:4433` public, `:4434` admin, bridge-only) | Identities, sessions, credentials, email verification. |
| Backend service | Validate the session, derive `UserID`, isolate data per user. |
| Postgres | Enforces isolation with row-level security (RLS). |
| Kafka workers | Run with a `BYPASSRLS` role and carry `user_id` on every event. |

## 1. Authenticate the session

Call Kratos `GET /sessions/whoami` with header `X-Session-Token: <opaque token>` and map
the response to a principal (`ingestion-service/internal/auth/kratos.go:32-36`):

```go
type Principal struct {
	UserID          string
	SessionID       string
	AuthenticatedAt time.Time
}
```

Rules:

- The token is opaque. Never decode it, never trust a client-supplied `user_id`.
- `401`/`403` mean invalid session. Any other failure is **provider unavailable → 503,
  never a logout** (`kratos.go:87-92`).
- `id` is a UUID; reject non-UUIDs as provider errors.

## 2. Attach the principal

Wrap protected routes with the `Auth` middleware
(`ingestion-service/internal/http/middleware.go:146`). It:

1. Extracts the bearer token.
2. Authenticates and optionally checks the account-deletion guard.
3. Stores `Principal` in the request context.

Handlers read it via `PrincipalFrom(ctx).UserID` and never take a user id from the
body or query string. Gate destructive actions with `HasRecentAuth()`
(`middleware.go:198`).

## 3. Isolate data with Postgres RLS

Two roles (`infra/postgres/bootstrap.sh`):

- `checkpoint_request` — `NOBYPASSRLS`, used by HTTP handlers.
- `checkpoint_worker` — `BYPASSRLS`, used by background jobs.

Every per-user table gets a `user_id` column and a policy
(`migrations/00008_request_rls.sql:6-16`):

```sql
ALTER TABLE my_table ENABLE ROW LEVEL SECURITY;

CREATE POLICY my_table_tenant ON my_table
    USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);
```

Run request work inside `Pool.WithUserTx`, which sets the value transaction-locally so
it cannot leak across pooled connections (`internal/repository/postgres/postgres.go:56-69`):

```go
err := pool.WithUserTx(ctx, principal.UserID, func(tx pgx.Tx) error {
	// queries here only see this user's rows
	return nil
})
```

Use `SECURITY DEFINER` functions for multi-row invariants (for example `claim_device`,
`migrations/00008_request_rls.sql:21`) and `REVOKE ... FROM PUBLIC` then grant explicitly.

## 4. Async workers

- Connect with the worker role (`BYPASSRLS`); RLS does not apply.
- Every event carries `user_id`; filter by it explicitly.
- Check the deletion tombstone and drop stale events for deleted accounts.

## 5. Admin operations

Identity deletion and session revocation use the Kratos **Admin API** (`:4434`), which is
bridge-only and never published (`internal/auth/kratos.go:122-223`). Reach it over the
internal Docker network.

## Adding a new service — checklist

- [ ] New Go module with `internal/auth` (copy the Kratos authenticator).
- [ ] Config `KRATOS_PUBLIC_URL` (`http://kratos:4433`); no user/session tables of your own.
- [ ] Add the `Auth` middleware to protected routes.
- [ ] If it stores per-user data: migration adding `user_id`, RLS + policy, and grants to
      both roles (see the `DO $$ ... $$` block in `migrations/00008_request_rls.sql`).
- [ ] Wrap handler DB work in `WithUserTx`.
- [ ] Publish only your own port; keep `:4434` internal.
- [ ] Use the shared error taxonomy below.

## Error taxonomy

| Status | Code | Meaning |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Missing or invalid session. |
| 403 | `VERIFICATION_REQUIRED` | Valid identity, email not verified. |
| 403 | `ACCOUNT_DELETING` | Deletion tombstone present. |
| 503 | `AUTH_UNAVAILABLE` | Kratos unreachable — never a logout. |

## Tradeoffs

- **Shared auth package vs per-service copy** — ingestion and transcription each carry
  their own `internal/auth`. A shared module removes duplication but adds versioning.
- **RLS vs app-layer filtering** — RLS is the strong guarantee and costs a `user_id`
  column plus a policy per table; one forgotten `WHERE` in app-layer filtering leaks data.
- **One Postgres vs DB-per-service** — same instance with per-service roles/schemas is the
  current norm and simpler to operate.
