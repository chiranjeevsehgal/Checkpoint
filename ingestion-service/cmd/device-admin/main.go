// Command device-admin is the privileged offline pendant provisioning and
// recovery CLI. It is never exposed over HTTP and uses DATABASE_URL.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"checkpoint/ingestion/internal/domain"
)

const commandTimeout = 10 * time.Second

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL must be set")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fail("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	switch os.Args[1] {
	case "provision":
		provision(ctx, conn, os.Args[2:])
	case "status":
		status(ctx, conn, os.Args[2:])
	case "unquarantine":
		unquarantine(ctx, conn, os.Args[2:])
	case "list":
		list(ctx, conn, os.Args[2:])
	case "deletions":
		deletions(ctx, conn, os.Args[2:])
	case "delete-account":
		deleteAccount(ctx, conn, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr,
		"usage: device-admin <provision|status|unquarantine|list|deletions|delete-account> [flags]")
}

func provision(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("provision", flag.ExitOnError)
	deviceFlag := fs.String("device", "", "32-character device id")
	hashFlag := fs.String("claim-hash", "", "SHA-256 of the cloud claim secret (64 hex)")
	_ = fs.Parse(args)

	deviceID, err := domain.NormalizeDeviceID(*deviceFlag)
	if err != nil {
		fail("invalid -device: %v", err)
	}
	claimHash, err := decodeHash(*hashFlag)
	if err != nil {
		fail("invalid -claim-hash: %v", err)
	}

	var state string
	err = conn.QueryRow(ctx, `SELECT state FROM devices WHERE device_id = $1`, deviceID).Scan(&state)
	if err == nil && state != domain.DeviceStateUnowned {
		fail("device %s is %s; refusing to overwrite its claim credential", deviceID, state)
	}
	if err != nil && err != pgx.ErrNoRows {
		fail("read device: %v", err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		fail("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO devices (device_id, state, updated_at)
		VALUES ($1, 'unowned', NOW())
		ON CONFLICT (device_id) DO NOTHING`, deviceID); err != nil {
		fail("insert device: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_claim_credentials (device_id, claim_hash)
		VALUES ($1, $2)
		ON CONFLICT (device_id) DO UPDATE SET claim_hash = EXCLUDED.claim_hash`,
		deviceID, claimHash); err != nil {
		fail("insert claim credential: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		fail("commit: %v", err)
	}
	fmt.Printf("provisioned %s\n", deviceID)
}

func status(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	deviceFlag := fs.String("device", "", "32-character device id")
	_ = fs.Parse(args)

	deviceID, err := domain.NormalizeDeviceID(*deviceFlag)
	if err != nil {
		fail("invalid -device: %v", err)
	}

	var owner *string
	var state string
	var claimedAt *time.Time
	err = conn.QueryRow(ctx, `
		SELECT user_id::text, state, claimed_at FROM devices WHERE device_id = $1`, deviceID).
		Scan(&owner, &state, &claimedAt)
	if err == pgx.ErrNoRows {
		fail("device %s is not provisioned", deviceID)
	}
	if err != nil {
		fail("read device: %v", err)
	}

	ownerText := "none"
	if owner != nil {
		ownerText = *owner
	}
	fmt.Printf("device_id=%s\nstate=%s\nowner=%s\n", deviceID, state, ownerText)
	if formatted := formatTimePtr(claimedAt); formatted != nil {
		fmt.Printf("claimed_at=%s\n", *formatted)
	}
}

type deviceRow struct {
	DeviceID  string  `json:"device_id"`
	UserID    *string `json:"user_id"`
	State     string  `json:"state"`
	ClaimedAt *string `json:"claimed_at"`
	UpdatedAt string  `json:"updated_at"`
}

// list prints every device, optionally filtered by state. -json emits a
// machine-readable array for the admin-console agent.
func list(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	stateFlag := fs.String("state", "", "filter by state (unowned|owned|reset_required)")
	jsonFlag := fs.Bool("json", false, "emit JSON")
	_ = fs.Parse(args)

	if *stateFlag != "" && !validDeviceState(*stateFlag) {
		fail("invalid -state: must be unowned, owned or reset_required")
	}

	query := `SELECT device_id, user_id::text, state, claimed_at, updated_at FROM devices`
	var queryArgs []any
	if *stateFlag != "" {
		query += ` WHERE state = $1`
		queryArgs = append(queryArgs, *stateFlag)
	}
	query += ` ORDER BY updated_at DESC`

	rows, err := conn.Query(ctx, query, queryArgs...)
	if err != nil {
		fail("list devices: %v", err)
	}
	defer rows.Close()

	devices := make([]deviceRow, 0)
	for rows.Next() {
		var (
			row       deviceRow
			claimedAt *time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&row.DeviceID, &row.UserID, &row.State, &claimedAt, &updatedAt); err != nil {
			fail("scan device: %v", err)
		}
		row.ClaimedAt = formatTimePtr(claimedAt)
		row.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		devices = append(devices, row)
	}
	if err := rows.Err(); err != nil {
		fail("list devices: %v", err)
	}

	if *jsonFlag {
		writeJSON(devices)
		return
	}
	for _, device := range devices {
		owner := "none"
		if device.UserID != nil {
			owner = *device.UserID
		}
		fmt.Printf("%s state=%s owner=%s updated_at=%s\n", device.DeviceID, device.State, owner, device.UpdatedAt)
	}
}

type deletionRow struct {
	UserID        string  `json:"user_id"`
	Status        string  `json:"status"`
	AttemptCount  int     `json:"attempt_count"`
	LastError     *string `json:"last_error"`
	NextAttemptAt string  `json:"next_attempt_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	CompletedAt   *string `json:"completed_at"`
}

// deletions lists the account-deletion tombstones and their worker status.
func deletions(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("deletions", flag.ExitOnError)
	jsonFlag := fs.Bool("json", false, "emit JSON")
	_ = fs.Parse(args)

	rows, err := conn.Query(ctx, `
		SELECT user_id::text, status, attempt_count, last_error, next_attempt_at, created_at, updated_at, completed_at
		FROM account_deletions ORDER BY created_at DESC`)
	if err != nil {
		fail("list deletions: %v", err)
	}
	defer rows.Close()

	items := make([]deletionRow, 0)
	for rows.Next() {
		var (
			row         deletionRow
			nextAttempt time.Time
			createdAt   time.Time
			updatedAt   time.Time
			completedAt *time.Time
		)
		if err := rows.Scan(&row.UserID, &row.Status, &row.AttemptCount, &row.LastError,
			&nextAttempt, &createdAt, &updatedAt, &completedAt); err != nil {
			fail("scan deletion: %v", err)
		}
		row.NextAttemptAt = nextAttempt.UTC().Format(time.RFC3339)
		row.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		row.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		row.CompletedAt = formatTimePtr(completedAt)
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		fail("list deletions: %v", err)
	}

	if *jsonFlag {
		writeJSON(items)
		return
	}
	for _, item := range items {
		fmt.Printf("%s status=%s attempts=%d updated_at=%s\n", item.UserID, item.Status, item.AttemptCount, item.UpdatedAt)
	}
}

// deleteAccount queues an account-deletion tombstone; the ingestion worker
// performs the actual purge, including device quarantine and identity removal.
func deleteAccount(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("delete-account", flag.ExitOnError)
	userFlag := fs.String("user", "", "identity UUID")
	_ = fs.Parse(args)

	userID, err := uuid.Parse(strings.TrimSpace(*userFlag))
	if err != nil {
		fail("invalid -user: %v", err)
	}

	tag, err := conn.Exec(ctx, `
		INSERT INTO account_deletions (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`, userID.String())
	if err != nil {
		fail("queue account deletion: %v", err)
	}
	if tag.RowsAffected() == 0 {
		fmt.Printf("already queued %s\n", userID)
		return
	}
	fmt.Printf("queued %s\n", userID)
}

func validDeviceState(state string) bool {
	switch state {
	case domain.DeviceStateUnowned, domain.DeviceStateOwned, domain.DeviceStateResetRequired:
		return true
	default:
		return false
	}
}

func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fail("encode json: %v", err)
	}
}

func unquarantine(ctx context.Context, conn *pgx.Conn, args []string) {
	fs := flag.NewFlagSet("unquarantine", flag.ExitOnError)
	deviceFlag := fs.String("device", "", "32-character device id")
	_ = fs.Parse(args)

	deviceID, err := domain.NormalizeDeviceID(*deviceFlag)
	if err != nil {
		fail("invalid -device: %v", err)
	}

	tag, err := conn.Exec(ctx, `
		UPDATE devices SET state = 'unowned', user_id = NULL, claimed_at = NULL, updated_at = NOW()
		WHERE device_id = $1 AND state = 'reset_required'`, deviceID)
	if err != nil {
		fail("unquarantine: %v", err)
	}
	if tag.RowsAffected() == 0 {
		fail("device %s is not in reset_required", deviceID)
	}
	fmt.Printf("unquarantined %s\n", deviceID)
}

func decodeHash(raw string) ([]byte, error) {
	v := strings.TrimSpace(raw)
	decoded, err := hex.DecodeString(v)
	if err != nil {
		return nil, err
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("must be a 32-byte SHA-256 digest")
	}
	return decoded, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
