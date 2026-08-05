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
//
// Provisioning the per-package DB needs docker (pg_dump/psql inside
// the Postgres container). If docker or the container isn't
// available, OpenTestDB falls back to the base FIRESIDE_TEST_DSN
// directly so local machines can still run the suites — callers must
// then serialize with -p 1 because the base DB is shared.
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

	// Isolated per-package DBs are only worth it when we can also
	// provision them; otherwise connect to the base test DB (the
	// pre-change behavior) and let the caller serialize with -p 1.
	if !pgDumpAvailable(adminDB) {
		t.Logf("testutil.OpenTestDB(%s): docker/PG container unavailable — using base FIRESIDE_TEST_DSN directly (parallel runs need -p 1)", pkg)
		return openTestDB(t, dsn)
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

	return openTestDB(t, packageDB)
}

// openTestDB connects to a DSN, skipping the test when Postgres is
// unreachable.
func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("testutil.OpenTestDB: sql.Open: %v", err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		t.Skipf("testutil.OpenTestDB: Postgres not reachable (%v) — skipping.", err)
	}
	return conn
}

// pgDumpAvailable reports whether per-package DBs can be provisioned
// via docker exec pg_dump: docker must exist and the resolved PG
// container must be running.
func pgDumpAvailable(adminDSN string) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	container := discoverContainer(adminDSN)
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	for _, name := range strings.Fields(string(out)) {
		if name == container {
			return true
		}
	}
	return false
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
	if !isSafeIdent(dbName) {
		return fmt.Errorf("unsafe database name %q", dbName)
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("open admin: %w", err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		return fmt.Errorf("ping admin: %w", err)
	}
	// Always drop + recreate. The package DB is ephemeral; mirror
	// re-runs every test, so we want a clean slate. The DROP
	// must use a separate connection because Postgres won't drop
	// a DB that has open connections.
	// Disconnect any clients first. pg_terminate_backend is idempotent
	// and safe even when no one is connected; if it fails, the DROP
	// below will fail loudly while the DB is still in use.
	if _, err := admin.ExecContext(context.Background(),
		fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`, dbName),
	); err != nil {
		_ = err
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
// The container name is resolved by discoverContainer: an explicit
// FIRESIDE_TEST_PG_CONTAINER env var wins, otherwise the container is
// located by its published port (GitHub Actions service containers get
// auto-generated names), with `fireside-postgres` (docker-compose) as
// the last resort.
func mirrorSchema(adminDSN, packageName string) error {
	container := discoverContainer(adminDSN)
	if !isSafeIdent(packageName) {
		return fmt.Errorf("unsafe package DB name %q", packageName)
	}

	// Never put the DB password in the docker argv — it would show up
	// in `ps` output on the host and in the container. Pass it via
	// PGPASSWORD and strip it from the DSNs we hand to pg_dump/psql.
	pw := dsnPassword(adminDSN)
	dumpDSN := stripPasswordDSN(adminDSN)
	pkgDSN, err := dbDSN(adminDSN, packageName)
	if err != nil {
		return fmt.Errorf("build package DSN: %w", err)
	}
	pkgDSN = stripPasswordDSN(pkgDSN)

	execArgs := func(extra ...string) []string {
		args := []string{"exec"}
		if pw != "" {
			args = append(args, "-e", "PGPASSWORD="+pw)
		}
		return append(args, extra...)
	}

	dump := exec.Command("docker", execArgs(container,
		"pg_dump", "--schema-only", "--no-owner", "--no-privileges", dumpDSN)...)
	var dumpOut bytes.Buffer
	dump.Stdout = &dumpOut
	dump.Stderr = os.Stderr
	if err := dump.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w", err)
	}

	// Build the docker exec command for psql: -i so it reads stdin
	// (the pg_dump output) from the host.
	restore := exec.Command("docker", execArgs("-i", container,
		"psql", "--dbname="+pkgDSN, "--variable", "ON_ERROR_STOP=1")...)
	restore.Stdin = &dumpOut
	restore.Stdout = os.Stdout
	restore.Stderr = os.Stderr
	if err := restore.Run(); err != nil {
		return fmt.Errorf("psql: %w", err)
	}
	return nil
}

// dsnPassword extracts the password embedded in a Postgres DSN.
func dsnPassword(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return ""
	}
	pw, _ := u.User.Password()
	return pw
}

// stripPasswordDSN returns the DSN without its password (the
// password travels via PGPASSWORD instead of the argv).
func stripPasswordDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	u.User = url.User(u.User.Username())
	return u.String()
}

// dbDSN rebuilds a DSN pointing at the named database, preserving
// host, port, user and query string from baseDSN.
func dbDSN(baseDSN, dbName string) (string, error) {
	u, err := url.Parse(baseDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// discoverContainer resolves the Postgres container to run pg_dump
// through. Priority: FIRESIDE_TEST_PG_CONTAINER env var → a container
// publishing the DSN's port (works for GitHub Actions service
// containers, whose names are auto-generated) → `fireside-postgres`
// (the docker-compose default).
func discoverContainer(adminDSN string) string {
	if env := os.Getenv("FIRESIDE_TEST_PG_CONTAINER"); env != "" {
		return env
	}
	port := "5432"
	if u, err := url.Parse(adminDSN); err == nil && u.Port() != "" {
		port = u.Port()
	}
	out, err := exec.Command("docker", "ps", "--filter", "publish="+port, "--format", "{{.Names}}").Output()
	if err == nil {
		for _, name := range strings.Fields(string(out)) {
			if name != "" {
				return name
			}
		}
	}
	return "fireside-postgres"
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
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}
