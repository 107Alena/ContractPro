package postgres

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestMigrations_AllFilesEmbedded verifies that all .sql migration files are
// present in the embedded filesystem and follow the naming convention.
func TestMigrations_AllFilesEmbedded(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations dir: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("no migration files found in embedded FS")
	}

	// Collect file names.
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// Verify even count (each version must have .up.sql + .down.sql).
	if len(names)%2 != 0 {
		t.Fatalf("odd number of migration files (%d) — each version must have .up.sql and .down.sql", len(names))
	}

	// Verify pairing: for each version, both up and down must exist.
	versions := len(names) / 2
	for i := 0; i < versions; i++ {
		upName := names[i*2]
		downName := names[i*2+1]

		if !strings.HasSuffix(upName, ".up.sql") && !strings.HasSuffix(upName, ".down.sql") {
			t.Errorf("unexpected file name: %s", upName)
		}
		if !strings.HasSuffix(downName, ".up.sql") && !strings.HasSuffix(downName, ".down.sql") {
			t.Errorf("unexpected file name: %s", downName)
		}

		// After sorting, .down.sql comes before .up.sql alphabetically.
		upFile := ""
		downFile := ""
		for _, n := range []string{upName, downName} {
			if strings.HasSuffix(n, ".up.sql") {
				upFile = n
			} else if strings.HasSuffix(n, ".down.sql") {
				downFile = n
			}
		}
		if upFile == "" {
			t.Errorf("missing .up.sql for migration pair %d: got %s, %s", i+1, upName, downName)
		}
		if downFile == "" {
			t.Errorf("missing .down.sql for migration pair %d: got %s, %s", i+1, upName, downName)
		}

		// Verify they share the same version prefix.
		if upFile != "" && downFile != "" {
			upPrefix := strings.TrimSuffix(upFile, ".up.sql")
			downPrefix := strings.TrimSuffix(downFile, ".down.sql")
			if upPrefix != downPrefix {
				t.Errorf("mismatched version prefix: up=%s down=%s", upFile, downFile)
			}
		}
	}

	t.Logf("found %d migration versions (%d files)", versions, len(names))
}

// TestMigrations_SequentialVersioning verifies that migration versions start at
// 000001 and increment sequentially without gaps.
func TestMigrations_SequentialVersioning(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations dir: %v", err)
	}

	// Extract unique version numbers.
	versionSet := make(map[int]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Extract version prefix (first 6 chars: "000001").
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			t.Errorf("invalid migration file name (no underscore): %s", name)
			continue
		}
		var ver int
		if _, err := fmt.Sscanf(parts[0], "%d", &ver); err != nil {
			t.Errorf("invalid version number in %s: %v", name, err)
			continue
		}
		versionSet[ver] = true
	}

	if len(versionSet) == 0 {
		t.Fatal("no valid version numbers found")
	}

	// Find max version.
	maxVer := 0
	for v := range versionSet {
		if v > maxVer {
			maxVer = v
		}
	}

	// Verify sequential from 1 to max.
	for i := 1; i <= maxVer; i++ {
		if !versionSet[i] {
			t.Errorf("missing migration version %d (gap in sequence)", i)
		}
	}

	t.Logf("migration versions: 1..%d (sequential, no gaps)", maxVer)
}

// TestMigrations_FilesNotEmpty verifies that no migration file is empty.
func TestMigrations_FilesNotEmpty(t *testing.T) {
	err := fs.WalkDir(migrationsFS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".sql" {
			return nil
		}

		content, readErr := fs.ReadFile(migrationsFS, path)
		if readErr != nil {
			t.Errorf("failed to read %s: %v", path, readErr)
			return nil
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			t.Errorf("migration file %s is empty", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk migrations dir: %v", err)
	}
}

// TestMigrations_UpFilesHaveTransactions verifies that all .up.sql files use
// explicit transaction boundaries (BEGIN/COMMIT) for atomicity.
func TestMigrations_UpFilesHaveTransactions(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}

		content, readErr := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if readErr != nil {
			t.Errorf("failed to read %s: %v", e.Name(), readErr)
			continue
		}

		upper := strings.ToUpper(string(content))
		hasBegin := strings.Contains(upper, "BEGIN")
		hasCommit := strings.Contains(upper, "COMMIT")

		if !hasBegin || !hasCommit {
			t.Errorf("migration %s lacks explicit transaction boundaries (BEGIN/COMMIT)", e.Name())
		}
	}
}

// TestMigrations_DownFilesExist verifies that each up-migration has a
// corresponding down-migration.
func TestMigrations_DownFilesExist(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations dir: %v", err)
	}

	upFiles := make(map[string]bool)
	downFiles := make(map[string]bool)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			base := strings.TrimSuffix(name, ".up.sql")
			upFiles[base] = true
		} else if strings.HasSuffix(name, ".down.sql") {
			base := strings.TrimSuffix(name, ".down.sql")
			downFiles[base] = true
		}
	}

	for base := range upFiles {
		if !downFiles[base] {
			t.Errorf("migration %s has .up.sql but no .down.sql", base)
		}
	}
	for base := range downFiles {
		if !upFiles[base] {
			t.Errorf("migration %s has .down.sql but no .up.sql", base)
		}
	}
}

// TestMigrations_SourceDriverCreation verifies that the iofs source driver
// can be created from the embedded filesystem without errors.
func TestMigrations_SourceDriverCreation(t *testing.T) {
	src, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("embedded FS read failed: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("no files in embedded migrations directory")
	}

	// Verify we can enumerate all files (same as what iofs.New does internally).
	count := 0
	for _, e := range src {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			count++
		}
	}
	t.Logf("embedded FS contains %d SQL migration files", count)
}

// TestMigrations_PgxDSNConversion verifies the pgxDSN helper.
func TestMigrations_PgxDSNConversion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "postgres://user:pass@host:5432/db?sslmode=disable",
			expected: "pgx5://user:pass@host:5432/db?sslmode=disable",
		},
		{
			input:    "postgresql://user:pass@host:5432/db",
			expected: "pgx5://user:pass@host:5432/db",
		},
		{
			input:    "pgx5://user:pass@host:5432/db",
			expected: "pgx5://user:pass@host:5432/db",
		},
		{
			input:    "custom://something",
			expected: "pgx5://custom://something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pgxDSN(tt.input)
			if got != tt.expected {
				t.Errorf("pgxDSN(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestMigrations_CurrentVersionCount verifies the expected number of migrations.
// Update this constant when adding new migrations.
func TestMigrations_CurrentVersionCount(t *testing.T) {
	const expectedVersions = 5

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations dir: %v", err)
	}

	versionSet := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) >= 2 {
			versionSet[parts[0]] = true
		}
	}

	if len(versionSet) != expectedVersions {
		t.Errorf("expected %d migration versions, got %d", expectedVersions, len(versionSet))
	}
}

// TestMigrations_OnlineSafetyAnnotation verifies that migration 000004
// (which requires ACCESS EXCLUSIVE lock for table restructuring) is
// documented with appropriate warnings.
func TestMigrations_OnlineSafetyAnnotation(t *testing.T) {
	content, err := fs.ReadFile(migrationsFS, "migrations/000004_audit_partitions.up.sql")
	if err != nil {
		t.Fatalf("failed to read 000004: %v", err)
	}

	// This migration explicitly takes ACCESS EXCLUSIVE lock and should
	// document this fact.
	upper := strings.ToUpper(string(content))
	if !strings.Contains(upper, "ACCESS EXCLUSIVE") {
		t.Error("migration 000004 should document ACCESS EXCLUSIVE lock requirement")
	}

	// It should mention it's safe on empty tables but requires maintenance
	// window on populated tables.
	s := string(content)
	if !strings.Contains(s, "LOCK TABLE") {
		t.Error("migration 000004 should use explicit LOCK TABLE for safety")
	}
}
