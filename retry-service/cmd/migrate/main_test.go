package main

import (
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestMigrationVersionsUnique guards against the duplicate goose versions
// that broke `migrate up` when two branches each added a 00002/00003.
func TestMigrationVersionsUnique(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no migrations found")
	}

	versionPrefix := regexp.MustCompile(`^(\d+)_`)
	seen := make(map[int]string)
	for _, path := range paths {
		base := filepath.Base(path)
		match := versionPrefix.FindStringSubmatch(base)
		if match == nil {
			t.Fatalf("unversioned migration %q", base)
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parsing version of %q: %v", base, err)
		}
		if prev, ok := seen[version]; ok {
			t.Fatalf("duplicate migration version %d: %q and %q", version, prev, base)
		}
		seen[version] = base
	}
}
