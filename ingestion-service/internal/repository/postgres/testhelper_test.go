package postgres

import "os"

// testDatabaseURL returns the integration-test database URL. Tests insert
// and delete only rows with random UUIDs, so the local dev database is
// safe to use.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return ""
}
