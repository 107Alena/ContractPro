// Package main provides the dm-migrate CLI tool for managing PostgreSQL
// schema migrations for the Document Management service.
//
// Usage:
//
//	dm-migrate <command> [args]
//
// Commands:
//
//	up                       Apply all pending migrations
//	down --confirm-destroy   Roll back all applied migrations (destructive)
//	goto <N>                 Migrate up or down to version N
//	version                  Print current migration version
//
// Environment:
//
//	DM_DB_DSN   PostgreSQL connection string (required)
//
// Concurrency safety:
//
//	golang-migrate uses a PostgreSQL advisory lock to serialize concurrent
//	migration runs. Multiple dm-migrate instances (e.g., in a multi-replica
//	Kubernetes deployment) are safe — only one will apply migrations, the
//	others will wait and then see no pending changes.
//
// This tool is intended for use as a docker-compose init-container
// and for manual operational tasks (rollback, version check).
package main

import (
	"fmt"
	"os"
	"strconv"

	"contractpro/document-management/internal/infra/postgres"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// parseCommand validates the command and arguments without connecting to DB.
// Returns the command name and any error. This allows tests to verify argument
// parsing without needing a live database.
func parseCommand(args []string) (cmd string, gotoVersion string, err error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("no command specified")
	}

	cmd = args[0]
	switch cmd {
	case "up", "version":
		return cmd, "", nil
	case "down":
		if len(args) < 2 || args[1] != "--confirm-destroy" {
			return "", "", fmt.Errorf("'down' is destructive and drops ALL tables; pass --confirm-destroy to proceed")
		}
		return cmd, "", nil
	case "goto":
		if len(args) < 2 {
			return "", "", fmt.Errorf("'goto' requires a version number argument")
		}
		return cmd, args[1], nil
	default:
		return "", "", fmt.Errorf("unknown command %q", cmd)
	}
}

func run(args []string) int {
	cmd, gotoVer, err := parseCommand(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		printUsage()
		return 1
	}

	dsn := os.Getenv("DM_DB_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "error: DM_DB_DSN environment variable is required")
		return 1
	}

	migrator, err := postgres.NewMigrator(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to create migrator: %v\n", err)
		return 1
	}
	defer func() {
		_ = migrator.Close()
	}()

	switch cmd {
	case "up":
		return cmdUp(migrator)
	case "down":
		return cmdDown(migrator)
	case "goto":
		return cmdGoto(migrator, gotoVer)
	case "version":
		return cmdVersion(migrator)
	default:
		return 1
	}
}

func cmdUp(m *postgres.Migrator) int {
	fmt.Println("applying all pending migrations...")
	if err := m.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "error: migration up failed: %v\n", err)
		return 1
	}
	ver, dirty, err := m.Version()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: applied but failed to read version: %v\n", err)
		return 0
	}
	fmt.Printf("migrations applied successfully — current version: %d (dirty: %v)\n", ver, dirty)
	return 0
}

func cmdDown(m *postgres.Migrator) int {
	fmt.Println("rolling back all migrations (--confirm-destroy acknowledged)...")
	if err := m.Down(); err != nil {
		fmt.Fprintf(os.Stderr, "error: migration down failed: %v\n", err)
		return 1
	}
	fmt.Println("all migrations rolled back successfully")
	return 0
}

func cmdGoto(m *postgres.Migrator, versionStr string) int {
	v, err := strconv.ParseUint(versionStr, 10, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid version number %q: %v\n", versionStr, err)
		return 1
	}
	fmt.Printf("migrating to version %d...\n", v)
	if err := m.MigrateToVersion(uint(v)); err != nil {
		fmt.Fprintf(os.Stderr, "error: migrate to version %d failed: %v\n", v, err)
		return 1
	}
	fmt.Printf("migrated to version %d successfully\n", v)
	return 0
}

func cmdVersion(m *postgres.Migrator) int {
	ver, dirty, err := m.Version()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to read version: %v\n", err)
		return 1
	}
	if ver == 0 {
		fmt.Println("no migrations applied")
		return 0
	}
	fmt.Printf("current version: %d (dirty: %v)\n", ver, dirty)
	return 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: dm-migrate <command> [args]

Commands:
  up                       Apply all pending migrations
  down --confirm-destroy   Roll back all applied migrations (destructive!)
  goto <N>                 Migrate up or down to version N
  version                  Print current migration version

Environment:
  DM_DB_DSN   PostgreSQL connection string (required)`)
}
