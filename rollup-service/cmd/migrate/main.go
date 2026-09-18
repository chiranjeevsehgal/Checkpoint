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
	// Dedicated version table: services share one database and each has its
	// own 00001 migration, so the goose history must not be shared.
	goose.SetTableName("rollup_schema_version")

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
		os.Exit(1)
	}
	if cmdErr != nil {
		fmt.Fprintln(os.Stderr, "migrate:", cmdErr)
		os.Exit(1)
	}
}
