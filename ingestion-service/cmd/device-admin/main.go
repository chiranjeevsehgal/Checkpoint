// Command device-admin is the privileged offline pendant provisioning and
// recovery CLI. It is never exposed over HTTP and uses DATABASE_URL.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: device-admin <provision|status|unquarantine> [flags]")
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
	if claimedAt != nil {
		fmt.Printf("claimed_at=%s\n", claimedAt.UTC().Format(time.RFC3339))
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
