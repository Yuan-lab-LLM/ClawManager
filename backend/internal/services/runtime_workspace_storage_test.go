package services

import (
	"encoding/json"
	"fmt"
	"testing"
)

func runtimeStorageIdentityForTest(slots int, backing, root, subPath string, readOnly, nonuniform bool) string {
	mounts := []string{}
	for index := -1; index < slots; index++ {
		name := "runtime"
		if index >= 0 {
			name = fmt.Sprintf("slot-%d", index)
		}
		path := subPath
		if nonuniform && index == slots-1 {
			path += "/different"
		}
		raw, _ := json.Marshal([]any{name, root, path, readOnly})
		mounts = append(mounts, string(raw))
	}
	raw, _ := json.Marshal([]any{backing, mounts})
	return string(raw)
}

func TestRuntimeStorageCompatibilityIgnoresOnlyUniformSlotCount(t *testing.T) {
	source := elasticManagementPod(1)
	source.CapabilitiesJSON = stringPtrOrNil(`["container-slots-v1"]`)
	source.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(8, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, false)
	target := source
	target.ID = 2
	target.DeploymentName = "managed-two-slots"
	target.Capacity = 2
	target.Slots = target.Slots[:2]
	target.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, false)
	if err := validateRuntimeManagementTarget(source, target); err != nil {
		t.Fatalf("same PVC and uniform mounts rejected solely for slot count: %v", err)
	}
	for _, identity := range []string{
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:other-uid:pv-name", "/data", "", false, false),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:other-pv", "/data", "", false, false),
		runtimeStorageIdentityForTest(2, "pvc:other/shared:pvc-uid:pv-name", "/data", "", false, false),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/elsewhere", "", false, false),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "other-subpath", false, false),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", true, false),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, true),
		runtimeStorageIdentityForTest(2, "pvc:runtime/shared::pv-name", "/data", "", false, false),
		runtimeStorageIdentityForTest(2, "nfs:server:/share", "/data", "", false, false),
	} {
		bad := target
		bad.WorkspaceStorageIdentity = identity
		if validateRuntimeManagementTarget(source, bad) == nil {
			t.Fatalf("different or unknown storage proof accepted: %s", identity)
		}
	}
	source.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(8, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, true)
	if validateRuntimeManagementTarget(source, target) == nil {
		t.Fatal("nonuniform source slot mapping broadened")
	}
}

func TestRuntimeStorageCompatibilityKeepsExactLegacyIdentity(t *testing.T) {
	source := elasticManagementPod(1)
	target := source
	target.ID = 2
	if err := validateRuntimeManagementTarget(source, target); err != nil {
		t.Fatal(err)
	}
	target.WorkspaceStorageIdentity = "pvc:runtime/other"
	if validateRuntimeManagementTarget(source, target) == nil {
		t.Fatal("different legacy unknown layout accepted")
	}
}
