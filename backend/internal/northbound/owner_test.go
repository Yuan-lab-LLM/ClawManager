package northbound

import (
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

func ownerPointer(value string) *string { return &value }

type northboundRuntimeImageProviderStub struct {
	images map[string]services.RuntimeImageConfig
}

func (s northboundRuntimeImageProviderStub) GetRuntimeImage(instanceType string) (services.RuntimeImageConfig, bool) {
	config, ok := s.images[instanceType]
	return config, ok
}

func (s northboundRuntimeImageProviderStub) GetRuntimeImageForRuntimeType(instanceType, runtimeType string) (services.RuntimeImageConfig, bool) {
	config, ok := s.images[instanceType+":"+runtimeType]
	return config, ok
}

func (s northboundRuntimeImageProviderStub) GetRuntimeImageForImage(string, string) (services.RuntimeImageConfig, bool) {
	return services.RuntimeImageConfig{}, false
}

func TestCoreServiceListsOnlyExactOwnerLiteInstances(t *testing.T) {
	service := &CoreService{instances: &northboundInstanceStub{items: map[int]*models.Instance{
		1: {ID: 1, UserID: 7, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite, Name: "lite-match"},
		2: {ID: 2, UserID: 7, Owner: ownerPointer("Tenant-A"), Type: services.RuntimeTypeHermes, InstanceMode: services.InstanceModeLite, Name: "case-mismatch"},
		3: {ID: 3, UserID: 8, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeOpenCode, InstanceMode: services.InstanceModeLite, Name: "other-user"},
		4: {ID: 4, UserID: 7, Owner: ownerPointer("tenant-a"), Type: "workbuddy", RuntimeVariant: services.WorkbuddyRuntimeLinux, InstanceMode: services.InstanceModePro, Name: "pro-match"},
		5: {ID: 5, UserID: 7, Owner: ownerPointer("tenant-a"), Type: "custom", InstanceMode: services.InstanceModeLite, Name: "unsupported"},
	}}}

	items, total, err := service.ListInstances(7, " tenant-a ", 1, 20)
	if err != nil {
		t.Fatalf("ListInstances returned error: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("unexpected owner-scoped instances: total=%d items=%+v", total, items)
	}
	seen := map[int]bool{}
	for _, item := range items {
		seen[item.ID] = true
		if item.Owner != "tenant-a" {
			t.Fatalf("unexpected owner-scoped instance owner: %+v", item)
		}
	}
	if !seen[1] || !seen[4] {
		t.Fatalf("unified list must contain Lite and Linux WorkBuddy Pro: %+v", items)
	}
}

func TestCoreServiceListsOnlyExactOwnerSupportedProInstances(t *testing.T) {
	service := &CoreService{instances: &northboundInstanceStub{items: map[int]*models.Instance{
		1: {ID: 1, UserID: 7, Owner: ownerPointer("tenant-a"), Type: "workbuddy", RuntimeVariant: "linux", InstanceMode: services.InstanceModePro, Name: "match"},
		2: {ID: 2, UserID: 7, Owner: ownerPointer("tenant-a"), Type: "workbuddy", RuntimeVariant: "windows", InstanceMode: services.InstanceModePro, Name: "windows"},
		3: {ID: 3, UserID: 7, Owner: ownerPointer("tenant-a"), Type: "workbuddy", RuntimeVariant: "linux", InstanceMode: services.InstanceModeLite, Name: "lite"},
		4: {ID: 4, UserID: 8, Owner: ownerPointer("tenant-a"), Type: "workbuddy", RuntimeVariant: "linux", InstanceMode: services.InstanceModePro, Name: "other-user"},
		5: {ID: 5, UserID: 7, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModePro, Name: "openclaw-pro"},
		6: {ID: 6, UserID: 7, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeHermes, InstanceMode: services.InstanceModePro, Name: "hermes-pro"},
		7: {ID: 7, UserID: 7, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeOpenCode, InstanceMode: services.InstanceModePro, Name: "opencode-pro"},
		8: {ID: 8, UserID: 7, Owner: ownerPointer("tenant-a"), Type: services.RuntimeTypeDeepSeekHarness, InstanceMode: services.InstanceModePro, Name: "deepseek-harness-pro"},
	}}}

	items, total, err := service.ListProInstances(7, " tenant-a ", 1, 20)
	if err != nil {
		t.Fatalf("ListProInstances returned error: %v", err)
	}
	if total != 5 || len(items) != 5 {
		t.Fatalf("unexpected owner-scoped Pro instances: total=%d items=%+v", total, items)
	}
	seen := map[int]bool{}
	for _, item := range items {
		seen[item.ID] = true
		if item.InstanceMode != services.InstanceModePro || item.RuntimeType != "" && item.RuntimeType != services.RuntimeBackendDesktop {
			t.Fatalf("unexpected Pro response: %+v", item)
		}
	}
	for _, id := range []int{1, 5, 6, 7, 8} {
		if !seen[id] {
			t.Fatalf("supported Pro instance %d missing from response: %+v", id, items)
		}
	}
}

func TestCoreServiceRequiresOwnerForList(t *testing.T) {
	service := &CoreService{instances: &northboundInstanceStub{items: map[int]*models.Instance{}}}
	if _, _, err := service.ListInstances(7, "", 1, 20); apiErrorCode(err) != "INVALID_REQUEST" {
		t.Fatalf("error = %v, want INVALID_REQUEST", err)
	}
}

func TestLiteCreateRequestPropagatesOwner(t *testing.T) {
	request := liteCreateRequest(
		&models.NorthboundOperation{OperationID: "op_owner_test"},
		CreateLiteInstanceRequest{Name: "owner-test", Owner: "tenant-a", Type: services.RuntimeTypeOpenClaw},
	)
	if request.Owner == nil || *request.Owner != "tenant-a" {
		t.Fatalf("owner = %v, want tenant-a", request.Owner)
	}
	if request.ProvisioningOperationID != "op_owner_test" || request.InstanceMode != services.InstanceModeLite {
		t.Fatalf("unexpected create request: %+v", request)
	}
}

func TestNorthboundLiteCreateSupportsEveryManagedLiteRuntime(t *testing.T) {
	for _, instanceType := range []string{
		services.RuntimeTypeOpenClaw,
		services.RuntimeTypeHermes,
		services.RuntimeTypeOpenCode,
		services.RuntimeTypeDeepSeekHarness,
		services.RuntimeTypeDeepSeekHarness,
	} {
		if !isSupportedNorthboundType(instanceType) {
			t.Fatalf("runtime %q must be accepted by the northbound Lite contract", instanceType)
		}
		request := liteCreateRequest(
			&models.NorthboundOperation{OperationID: "op_" + instanceType},
			CreateLiteInstanceRequest{Name: instanceType + "-test", Owner: "tenant-a", Type: instanceType},
		)
		if request.Type != instanceType || request.InstanceMode != services.InstanceModeLite || request.RuntimeType != services.RuntimeBackendGateway {
			t.Fatalf("unexpected %s Lite request: %+v", instanceType, request)
		}
		if request.DiskGB != services.DefaultLiteDiskGB {
			t.Fatalf("%s Lite disk = %dGiB, want %dGiB", instanceType, request.DiskGB, services.DefaultLiteDiskGB)
		}
	}
	if !isSupportedNorthboundType("workbuddy") {
		t.Fatal("historical workbuddy records must remain supported by the northbound lifecycle contract")
	}
	if isCreatableNorthboundType("workbuddy") {
		t.Fatal("workbuddy must not be accepted for new provisioning in the team distribution")
	}
	for _, instanceType := range []string{"codex", "claude-code", "custom"} {
		if isSupportedNorthboundType(instanceType) {
			t.Fatalf("runtime %q must not be accepted by the northbound contract", instanceType)
		}
	}
}

func TestProCreateRequestUsesFixedLinuxWorkbuddyPreset(t *testing.T) {
	t.Setenv("CLAWMANAGER_WORKBUDDY_LINUX_IMAGE", "registry.example/workbuddy-linux:test")
	request, err := proCreateRequest(
		&models.NorthboundOperation{OperationID: "op_pro_owner_test"},
		CreateProInstanceRequest{Name: "workbuddy-test", Owner: "tenant-a", Type: "workbuddy"},
	)
	if err != nil {
		t.Fatalf("proCreateRequest returned error: %v", err)
	}
	if request.Owner == nil || *request.Owner != "tenant-a" {
		t.Fatalf("owner = %v, want tenant-a", request.Owner)
	}
	if request.Type != "workbuddy" || request.RuntimeVariant != services.WorkbuddyRuntimeLinux ||
		request.Mode != services.InstanceModePro || request.InstanceMode != services.InstanceModePro ||
		request.RuntimeType != services.RuntimeBackendDesktop {
		t.Fatalf("unexpected WorkBuddy runtime selection: %+v", request)
	}
	if request.CPUCores != 4 || request.MemoryGB != 8 || request.DiskGB != 40 || request.GPUEnabled || request.GPUCount != 0 {
		t.Fatalf("unexpected WorkBuddy resource preset: %+v", request)
	}
	if request.ImageRegistry == nil || !strings.Contains(*request.ImageRegistry, "workbuddy-linux") {
		t.Fatalf("unexpected WorkBuddy image: %v", request.ImageRegistry)
	}
	if request.ProvisioningOperationID != "op_pro_owner_test" {
		t.Fatalf("operation ID = %q", request.ProvisioningOperationID)
	}
}

func TestProCreateRequestSupportsManagedDesktopRuntimes(t *testing.T) {
	services.SetRuntimeImageSettingsProvider(nil)
	for _, instanceType := range []string{
		services.RuntimeTypeOpenClaw,
		services.RuntimeTypeHermes,
		services.RuntimeTypeOpenCode,
	} {
		if !isSupportedNorthboundProType(instanceType) {
			t.Fatalf("runtime %q must be accepted by the northbound Pro contract", instanceType)
		}
		request, err := proCreateRequest(
			&models.NorthboundOperation{OperationID: "op_pro_" + instanceType},
			CreateProInstanceRequest{Name: instanceType + "-pro", Owner: "tenant-a", Type: instanceType},
		)
		if err != nil {
			t.Fatalf("proCreateRequest(%q) returned error: %v", instanceType, err)
		}
		if request.Type != instanceType || request.Mode != services.InstanceModePro ||
			request.InstanceMode != services.InstanceModePro || request.RuntimeType != services.RuntimeBackendDesktop {
			t.Fatalf("unexpected %s Pro request: %+v", instanceType, request)
		}
		if request.ImageRegistry == nil || strings.Contains(strings.ToLower(*request.ImageRegistry), "-lite") {
			t.Fatalf("%s Pro request selected an invalid image: %v", instanceType, request.ImageRegistry)
		}
	}
	for _, instanceType := range []string{"codex", "claude-code", "custom"} {
		if isSupportedNorthboundProType(instanceType) {
			t.Fatalf("runtime %q must not be accepted by this Pro contract", instanceType)
		}
	}
	if !isSupportedNorthboundProType("workbuddy") {
		t.Fatal("historical WorkBuddy Pro records must remain supported")
	}
	if isCreatableNorthboundProType("workbuddy") {
		t.Fatal("WorkBuddy Pro must not be accepted for new provisioning in the team distribution")
	}
}

func TestProCreateRequestUsesSavedClawManagerDesktopImage(t *testing.T) {
	services.SetRuntimeImageSettingsProvider(northboundRuntimeImageProviderStub{images: map[string]services.RuntimeImageConfig{
		"opencode:desktop": {
			Image:       "10.130.15.40:5000/agentsruntime/opencode-pro:saved",
			RuntimeType: services.RuntimeBackendDesktop,
		},
		"deepseek-harness:desktop": {
			Image:       "10.130.15.40:5000/agentsruntime/deepseek-harness-pro:saved",
			RuntimeType: services.RuntimeBackendDesktop,
		},
	}})
	t.Cleanup(func() { services.SetRuntimeImageSettingsProvider(nil) })

	for _, instanceType := range []string{services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness} {
		request, err := proCreateRequest(
			&models.NorthboundOperation{OperationID: "op_saved_image_" + instanceType},
			CreateProInstanceRequest{Name: instanceType + "-pro", Owner: "tenant-a", Type: instanceType},
		)
		if err != nil {
			t.Fatalf("proCreateRequest(%s) returned error: %v", instanceType, err)
		}
		want := "10.130.15.40:5000/agentsruntime/" + instanceType + "-pro:saved"
		if request.ImageRegistry == nil || *request.ImageRegistry != want {
			t.Fatalf("northbound %s Pro request did not use the saved desktop image: %v", instanceType, request.ImageRegistry)
		}
	}
}

func TestOperationCreateRequestSelectsModeFromType(t *testing.T) {
	lite, _, err := operationCreateRequest(&models.NorthboundOperation{
		OperationID:    "op_lite",
		OperationType:  OperationTypeLiteInstance,
		RequestPayload: `{"name":"lite-test","owner":"tenant-a","type":"openclaw"}`,
	})
	if err != nil || lite.InstanceMode != services.InstanceModeLite || lite.Type != "openclaw" {
		t.Fatalf("unexpected Lite operation request: request=%+v err=%v", lite, err)
	}
	workbuddy, _, err := operationCreateRequest(&models.NorthboundOperation{
		OperationID:    "op_unified_workbuddy",
		OperationType:  OperationTypeLiteInstance,
		RequestPayload: `{"name":"workbuddy-test","owner":"tenant-a","type":"workbuddy"}`,
	})
	if err != nil || workbuddy.InstanceMode != services.InstanceModePro || workbuddy.RuntimeVariant != services.WorkbuddyRuntimeLinux {
		t.Fatalf("unexpected unified WorkBuddy operation request: request=%+v err=%v", workbuddy, err)
	}
	// Legacy queued pro_instance operations remain executable during upgrades.
	pro, _, err := operationCreateRequest(&models.NorthboundOperation{
		OperationID:    "op_pro",
		OperationType:  OperationTypeProInstance,
		RequestPayload: `{"name":"pro-test","owner":"tenant-a","type":"workbuddy"}`,
	})
	if err != nil || pro.InstanceMode != services.InstanceModePro || pro.RuntimeVariant != services.WorkbuddyRuntimeLinux {
		t.Fatalf("unexpected Pro operation request: request=%+v err=%v", pro, err)
	}
	openCodePro, _, err := operationCreateRequest(&models.NorthboundOperation{
		OperationID:    "op_opencode_pro",
		OperationType:  OperationTypeProInstance,
		RequestPayload: `{"name":"opencode-pro","owner":"tenant-a","type":"opencode"}`,
	})
	if err != nil || openCodePro.InstanceMode != services.InstanceModePro || openCodePro.RuntimeType != services.RuntimeBackendDesktop {
		t.Fatalf("unexpected OpenCode Pro operation request: request=%+v err=%v", openCodePro, err)
	}
}

func TestLifecycleHelpersUseNarrowRestartAndResetCapabilities(t *testing.T) {
	stub := &northboundInstanceStub{items: map[int]*models.Instance{}}
	if err := restartInstance(stub, 41); err != nil {
		t.Fatalf("restartInstance returned error: %v", err)
	}
	if err := resetInstance(stub, 42); err != nil {
		t.Fatalf("resetInstance returned error: %v", err)
	}
	if err := permanentlyDeleteInstance(stub, 43); err != nil {
		t.Fatalf("permanentlyDeleteInstance returned error: %v", err)
	}
	if len(stub.restartCalls) != 1 || stub.restartCalls[0] != 41 || len(stub.resetCalls) != 1 || stub.resetCalls[0] != 42 || len(stub.deleteCalls) != 1 || stub.deleteCalls[0] != 43 {
		t.Fatalf("unexpected lifecycle calls: restart=%v reset=%v delete=%v", stub.restartCalls, stub.resetCalls, stub.deleteCalls)
	}
}

func TestLifecycleOperationModeIsPreservedForAudit(t *testing.T) {
	for _, operationType := range []string{OperationTypeProRestart, OperationTypeProReset, OperationTypeProDelete} {
		if mode := operationInstanceMode(&models.NorthboundOperation{OperationType: operationType}); mode != services.InstanceModePro {
			t.Fatalf("operation %s mode = %s, want pro", operationType, mode)
		}
	}
	for _, operationType := range []string{OperationTypeLiteRestart, OperationTypeLiteReset, OperationTypeLiteDelete} {
		if mode := operationInstanceMode(&models.NorthboundOperation{OperationType: operationType}); mode != services.InstanceModeLite {
			t.Fatalf("operation %s mode = %s, want lite", operationType, mode)
		}
	}
}
