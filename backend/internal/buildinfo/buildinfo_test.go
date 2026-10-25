package buildinfo

import "testing"

func TestCurrentUsesLinkedBuildInformation(t *testing.T) {
	previousVersion, previousCommit, previousBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = previousVersion, previousCommit, previousBuildTime
	})
	Version, Commit, BuildTime = "v2026.9.1", "abcdef123456", "2026-09-01T08:00:00Z"

	got := Current()
	if got.Version != Version || got.Commit != Commit || got.BuildTime != BuildTime {
		t.Fatalf("Current() = %+v, want linked build information", got)
	}
}

func TestCurrentAllowsDeploymentOverrides(t *testing.T) {
	t.Setenv("CLAWMANAGER_VERSION", "private-42")
	t.Setenv("CLAWMANAGER_COMMIT", "123456789abc")
	t.Setenv("CLAWMANAGER_BUILD_TIME", "2026-09-01T09:00:00Z")

	got := Current()
	if got.Version != "private-42" || got.Commit != "123456789abc" || got.BuildTime != "2026-09-01T09:00:00Z" {
		t.Fatalf("Current() = %+v, want environment overrides", got)
	}
}
