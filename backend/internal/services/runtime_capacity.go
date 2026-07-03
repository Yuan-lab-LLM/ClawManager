package services

import (
	"fmt"
	"path"
	"strings"
)

const (
	RuntimeTypeOpenClaw = "openclaw"
	RuntimeTypeHermes   = "hermes"

	InstanceModeLite = "lite"
	InstanceModePro  = "pro"

	RuntimeBackendGateway = "gateway"
	RuntimeBackendDesktop = "desktop"
	RuntimeBackendShell   = "shell"

	RuntimeGatewayPortStart = 20000
	RuntimePodCapacity      = 100
	// OpenClaw gateway slots reserve adjacent service ports in addition to the
	// public gateway port. Capacity is instance count, not raw port count.
	RuntimeGatewayPortsPerInstance = 3
	RuntimeGatewayPortEnd          = RuntimeGatewayPortStart + RuntimePodCapacity*RuntimeGatewayPortsPerInstance - 1
	RuntimeLinuxIDBase             = 200000
)

func NormalizeV2RuntimeType(instanceType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case RuntimeTypeOpenClaw:
		return RuntimeTypeOpenClaw, true
	case RuntimeTypeHermes:
		return RuntimeTypeHermes, true
	default:
		return "", false
	}
}

func NormalizeInstanceMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case InstanceModeLite:
		return InstanceModeLite, true
	case InstanceModePro:
		return InstanceModePro, true
	default:
		return "", false
	}
}

func RuntimeTypeForInstanceMode(mode string) (string, bool) {
	normalized, ok := NormalizeInstanceMode(mode)
	if !ok {
		return "", false
	}
	if normalized == InstanceModeLite {
		return RuntimeBackendGateway, true
	}
	return RuntimeBackendDesktop, true
}

func InstanceModeForRuntimeType(runtimeType string) string {
	if strings.EqualFold(strings.TrimSpace(runtimeType), RuntimeBackendGateway) {
		return InstanceModeLite
	}
	return InstanceModePro
}

func RuntimeWorkspacePath(runtimeType string, userID int, instanceID int) string {
	return RuntimeWorkspacePathWithRoot("/workspaces", runtimeType, userID, instanceID)
}

func RuntimeWorkspacePathWithRoot(root, runtimeType string, userID int, instanceID int) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/workspaces"
	}
	return path.Join(root, fmt.Sprintf("%s/user-%d/instance-%d", runtimeType, userID, instanceID))
}

func RuntimeLinuxID(instanceID int) int {
	return RuntimeLinuxIDBase + instanceID
}
