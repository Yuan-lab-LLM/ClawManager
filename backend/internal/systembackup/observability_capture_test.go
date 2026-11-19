package systembackup

import "testing"

func TestObservabilityCaptureRequiresJournalAndStableSecret(t *testing.T) {
	source := CaptureSources{}
	if validateObservationCaptureCoverage(source, false) == nil {
		t.Fatal("omitted durable journal accepted")
	}
	source.Workspace = []FileRoot{{ID: "observability", Path: t.TempDir()}}
	if err := validateObservationCaptureCoverage(source, false); err != nil {
		t.Fatal(err)
	}
	if validateObservationCaptureCoverage(source, true) == nil {
		t.Fatal("omitted stable encryption secret accepted")
	}
	source.Secrets = []KubernetesSelection{{Version: "v1", Resource: "secrets", Name: "clawmanager-observability-key-v1", UID: "immutable-key-uid"}}
	if err := validateObservationCaptureCoverage(source, true); err != nil {
		t.Fatal(err)
	}
}
