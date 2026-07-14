package repository

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildV2SchedulerInstanceQueryRequiresWorkspaceTypeAndStatuses(t *testing.T) {
	query, args := buildV2SchedulerInstanceQuery([]string{"creating", "running"}, 25)
	normalized := normalizeSQLForTest(query)

	requiredFragments := []string{
		"FROM instances",
		"status IN (?, ?)",
		"runtime_type = ?",
		"instance_mode = ?",
		"workspace_path IS NOT NULL",
		"TRIM(workspace_path) <> ''",
		"type IN (?, ?)",
		"ORDER BY id",
		"LIMIT ?",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("query %q does not contain %q", normalized, fragment)
		}
	}

	wantArgs := []any{"creating", "running", "gateway", "lite", "openclaw", "hermes", 25}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestV2DesiredRunningStatusesIncludeRecoverableErrorCandidates(t *testing.T) {
	want := []string{"creating", "running", "error"}
	if got := v2DesiredRunningStatuses(); !reflect.DeepEqual(got, want) {
		t.Fatalf("desired running statuses = %#v, want %#v", got, want)
	}
}

func TestBuildV2SchedulerInstanceQueryDefaultsLimit(t *testing.T) {
	query, args := buildV2SchedulerInstanceQuery([]string{"creating"}, 0)
	normalized := normalizeSQLForTest(query)

	if !strings.Contains(normalized, "status IN (?)") {
		t.Fatalf("query %q does not contain single status predicate", normalized)
	}
	wantArgs := []any{"creating", "gateway", "lite", "openclaw", "hermes", 100}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestGatewayTokenHashTrimsAndDoesNotExposeToken(t *testing.T) {
	hash := gatewayTokenHash(" igt_secret ")
	if len(hash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hash))
	}
	if strings.Contains(hash, "igt_secret") {
		t.Fatalf("hash must not contain raw token")
	}
	if hash != gatewayTokenHash("igt_secret") {
		t.Fatalf("hash should trim surrounding whitespace")
	}
	if hash == gatewayTokenHash("igt_other") {
		t.Fatalf("different tokens should produce different hashes")
	}
}
func normalizeSQLForTest(query string) string {
	return strings.Join(strings.Fields(query), " ")
}
