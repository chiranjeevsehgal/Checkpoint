// Command migrate applies the transcription-service schema with goose.
//
// Usage, from the transcription-service directory:
//
//	DATABASE_URL=... go run ./cmd/migrate up
//	go run ./cmd/migrate status
//	go run ./cmd/migrate down   # one version back
//
// Migrations run as a separate deployment step, never from the worker.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	dir := flag.String("dir", "migrations", "migrations directory")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: migrate [-dir migrations] <up|down|status|version>")
		os.Exit(2)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL must be set")
		os.Exit(1)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "ping db:", err)
		os.Exit(1)
	}

	goose.SetDialect("postgres")

	var cmdErr error
	switch flag.Arg(0) {
	case "up":
		cmdErr = goose.Up(db, *dir)
	case "down":
		cmdErr = goose.Down(db, *dir)
	case "status":
		cmdErr = goose.Status(db, *dir)
	case "version":
		cmdErr = goose.Version(db, *dir)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", flag.Arg(0))
		os.Exit(2)
	}
	if cmdErr != nil {
		fmt.Fprintln(os.Stderr, "migrate:", cmdErr)
		os.Exit(1)
	}
}