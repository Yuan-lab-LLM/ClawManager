package systembackup

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRegistryCoversEmbeddedTableMigrations(t *testing.T) {
	files, err := filepath.Glob("../db/migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("no migrations")
	}
	pattern := regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([a-z_][a-z0-9_]*)")
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(raw), -1) {
			if _, ok := Policy(match[1]); !ok {
				t.Errorf("unassigned migration table %s in %s", match[1], file)
			}
		}
	}
	seen := map[string]bool{}
	for _, p := range Registry() {
		if seen[p.Name] {
			t.Fatalf("duplicate owner: %s", p.Name)
		}
		seen[p.Name] = true
		if p.Category == "system_backup" && p.BackupStrategy != "metadata_only" {
			t.Fatal("control payload included")
		}
	}
}
func TestCoverageRejectsUnknownObjectsAndInjection(t *testing.T) {
	c := CheckCoverage([]string{"teams", "new_runnable_jobs"})
	if len(c.Unassigned) != 1 || c.Unassigned[0] != "new_runnable_jobs" {
		t.Fatal(c)
	}
	for _, s := range []string{"teams;DROP TABLE users", "../users", "`users`", ""} {
		if _, err := QuoteIdentifier(s); err == nil {
			t.Fatal("unsafe identifier", s)
		}
	}
	if err := ValidateObjectTypes(map[string]string{"teams": "VIEW"}); err == nil {
		t.Fatal("unregistered view accepted")
	}
	if len(RegistryHash()) != 64 {
		t.Fatal("invalid registry hash")
	}
}

func TestRuntimeManagementCoordinationIsReferenceOnly(t *testing.T) {
	for name, classification := range map[string]string{
		"runtime_management_operations":     "secret",
		"runtime_management_admission_lock": "internal",
		"runtime_pod_deletion_intents":      "secret",
	} {
		policy, ok := Policy(name)
		if !ok || policy.Category != "system_backup" || policy.Classification != classification || policy.BackupStrategy != "metadata_only" || policy.RestoreStrategy != "reference_only" {
			t.Errorf("live runtime coordination %s must not be captured or replayed: %+v", name, policy)
		}
	}
}

func TestRepositoriesDoNotCreateSchema(t *testing.T) {
	files, err := filepath.Glob("../repository/*.go")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS`)
	for _, file := range files {
		if len(file) >= 8 && file[len(file)-8:] == "_test.go" {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if pattern.Match(raw) {
			t.Errorf("runtime repository owns DDL instead of migration: %s", file)
		}
	}
}
