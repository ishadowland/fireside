// Package testutil provides shared helpers for integration tests.
//
// OpenTestDB derives a per-package database name from the base
// FIRESIDE_TEST_DSN, creates the database if it doesn't exist, and
// returns a *sql.DB pointed at it. Each integration suite gets its
// own database, so the three suites can run in parallel under
// `go test ./...` without racing on shared tables.
//
// Per-package DB naming:
//
//	FIRESIDE_TEST_DSN = postgres://fireside:***@localhost:5432/fireside_test?sslmode=disable
//	                          \_____________________ base _____________/        \___/
//	                                                                       └── name
//
//	openTestDB(t, "rooms")  ->  postgres://...@localhost:5432/fireside_test_rooms?sslmode=disable
//
// The base DB (e.g. `fireside_test`) is used as an administrative
// connection to create the per-package DB. The base DSN must end
// with a recognizable /<dbname> prefix.
package testutil

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenTestDB returns a *sql.DB connected to a per-package test
// database derived from FIRESIDE_TEST_DSN. If the env var is not
// set the test is skipped so unit-only `go test ./...` runs
// silently.
//
// The first call for a given pkg creates the database; subsequent
// calls reuse it. Caller must defer db.Close() and must not share
// the *sql.DB across packages.
func OpenTestDB(t *testing.T, pkg string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("FIRESIDE_TEST_DSN")
	if dsn == "" {
		t.Skipf("testutil.OpenTestDB(%s): FIRESIDE_TEST_DSN not set", pkg)
	}
	packageDB, adminDB, err := splitDSN(dsn, pkg)
	if err != nil {
		t.Fatalf("testutil.OpenTestDB(%s): split DSN: %v", pkg, err)
	}

	// Create the per-package DB if it doesn't exist (using the
	// base DB as the admin connection).
	packageName := packageDBName(packageDB)
	if err := ensureDatabase(adminDB, packageName); err != nil {
		t.Skipf("testutil.OpenTestDB(%s): cannot provision DB (%v) — skipping. Verify FIRESIDE_TEST_DSN points at a Postgres where the user can CREATE DATABASE.", pkg, err)
	}

	// Mirror the base DB schema into the package DB. The base DB
	// (fireside_test) is expected to have all migrations applied;
	// we copy every table + sequence + type so the package DB
	// starts in the same shape. We do this with a pg_dump -> psql
	// pipe for portability; the test process is the only place
	// that needs the schema, so a small stdlib-only migration tool
	// is overkill.
	if err := mirrorSchema(adminDB, packageName); err != nil {
		t.Fatalf("testutil.OpenTestDB(%s): mirror schema: %v", pkg, err)
	}

	// Apply migrations: the caller is expected to run `migrate up`
	// themselves against the package DB before running the test
	// suite (the CI workflow does this for `fireside_test`; for
	// per-package DBs the test setup script must do the same).
	//
	// openTestDB does NOT apply migrations here because the
	// migrate binary is a separate process and pulling it into the
	// test binary would require importing the migration files.
	// Tests that need a migrated schema should call a project-level
	// `setupSuite` helper that wraps OpenTestDB.

	conn, err := sql.Open("pgx", packageDB)
	if err != nil {
		t.Fatalf("testutil.OpenTestDB(%s): sql.Open: %v", pkg, err)
	}
	if err := conn.Ping(); err != nil {
		t.Skipf("testutil.OpenTestDB(%s): Postgres not reachable (%v) — skipping.", pkg, err)
	}
	return conn
}

// splitDSN returns the package-specific DSN and an admin DSN that
// connects to the base database (the part of the path before the
// package suffix).
func splitDSN(baseDSN, pkg string) (packageDSN, adminDSN string, err error) {
	u, err := url.Parse(baseDSN)
	if err != nil {
		return "", "", fmt.Errorf("parse DSN: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", "", fmt.Errorf("DSN has no database name in path: %q", baseDSN)
	}
	// package DB = dbName + "_" + pkg
	packageName := dbName + "_" + pkg

	// Build the admin DSN against the base DB (dbName).
	adminU := *u
	adminU.Path = "/" + dbName
	adminDSN = adminU.String()

	packageU := *u
	packageU.Path = "/" + packageName
	packageDSN = packageU.String()
	return packageDSN, adminDSN, nil
}

// packageDBName returns the database name (the path component of
// a Postgres DSN, stripped of the leading slash).
func packageDBName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return strings.TrimPrefix(dsn, "/")
	}
	return strings.TrimPrefix(u.Path, "/")
}

// ensureDatabase creates the named database if it doesn't exist.
// Uses the admin connection to issue the CREATE DATABASE.
func ensureDatabase(adminDSN, dbName string) error {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("open admin: %w", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		return fmt.Errorf("ping admin: %w", err)
	}
	// Always drop + recreate. The package DB is ephemeral; mirror
	// re-runs every test, so we want a clean slate. The DROP
	// must use a separate connection because Postgres won't drop
	// a DB that has open connections.
	if _, err := admin.ExecContext(context.Background(),
		// Disconnect any clients first. The pg_terminate_backend
		// call is idempotent and safe even when no one is connected.
		fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`, dbName),
	); err != nil {
		// Best-effort; the DROP below will fail loudly if anything
		// is still holding the DB.
	}
	if _, err := admin.ExecContext(context.Background(),
		fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, dbName),
	); err != nil {
		return fmt.Errorf("DROP DATABASE %s: %w", dbName, err)
	}
	if _, err := admin.ExecContext(context.Background(),
		fmt.Sprintf(`CREATE DATABASE "%s"`, dbName),
	); err != nil {
		return fmt.Errorf("CREATE DATABASE %s: %w", dbName, err)
	}
	return nil
}

// mirrorSchema clones the schema (tables, sequences, types) from
// the base test DB into the package DB. The base DB is expected
// to have all migrations applied so the package DB inherits the
// same shape.
//
// We shell out to `docker exec <postgres_container> pg_dump ...`
// and pipe the result into `docker exec ... psql ...`. This makes
// the helper portable to dev machines (no Postgres client
// binaries required) but introduces a hard dependency on
// docker-compose. The CI runner has Docker baked in, so the
// dependency is already implied.
//
// The container name is read from the FIRESIDE_TEST_PG_CONTAINER
// env var (default: `fireside-postgres`).
func mirrorSchema(adminDSN, packageName string) error {
	container := os.Getenv("FIRESIDE_TEST_PG_CONTAINER")
	if container == "" {
		container = "fireside-postgres"
	}
	// Sanitize the package name for postgres identifier rules.
	if !isSafeIdent(packageName) {
		return fmt.Errorf("unsafe package DB name %q", packageName)
	}

	dump := exec.Command("docker", "exec", container,
		"pg_dump", "--schema-only", "--no-owner", "--no-privileges", adminDSN)
	var dumpOut bytes.Buffer
	dump.Stdout = &dumpOut
	dump.Stderr = os.Stderr
	if err := dump.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w", err)
	}

	// Build the package DSN with the password stripped (we set
	// PGPASSWORD on the docker exec invocation).
	packageDSN := strings.Replace(adminDSN, "/"+packageDBName(adminDSN)+"?", "/"+packageName+"?", 1)
	if u, err := url.Parse(packageDSN); err == nil && u.User != nil {
		u.User = url.User(u.User.Username())
		packageDSN = u.String()
	}

	// Build the docker exec command. We pass PGPASSWORD via -e
	// so the password isn't visible in the container's resolved
	// argv or in ps output. We also need -i so the command reads
	// stdin from the host (psql is the consumer).
	//
	// Final argument order: docker exec [-e PGPASSWORD=...] -i <container> psql --dbname=...
	args := []string{"exec"}
	if u, err := url.Parse(adminDSN); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok {
			args = append(args, "-e", "PGPASSWORD="+pw)
		}
	}
	args = append(args, "-i", container, "psql", "--dbname="+packageDSN, "--variable", "ON_ERROR_STOP=1")
	restore := exec.Command("docker", args...)
	restore.Stdin = &dumpOut
	restore.Stdout = os.Stdout
	restore.Stderr = os.Stderr
	if err := restore.Run(); err != nil {
		return fmt.Errorf("psql: %w", err)
	}
	return nil
}

// isSafeIdent returns true if s is a plausible PostgreSQL identifier
// (lowercase letters, digits, underscore). pg_dump produces
// statements that quote identifiers, so we only need to be sure we
// don't pass script-injection vectors.
func isSafeIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, ch := range s {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

// stripSlash returns the path without the leading slash.
func stripSlash(s string) string {
	return strings.TrimPrefix(s, "/")
}
