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
	// OpenClaw Lite reserves a primary gateway port plus its adjacent browser
	// ports. Hermes Lite only exposes the primary dashboard port.
	RuntimeGatewayPortOffset        = 0
	RuntimeBrowserCDPPortOffset     = 1
	RuntimeBrowserControlPortOffset = 2
	RuntimeOpenClawPortsPerInstance = RuntimeBrowserControlPortOffset + 1
	RuntimeHermesPortsPerInstance   = 1
	// Keep the shared range large enough for the runtime with the largest port
	// block. Runtime-specific allocation still stops at the Pod slot capacity.
	RuntimeGatewayPortEnd = RuntimeGatewayPortStart + RuntimePodCapacity*RuntimeOpenClawPortsPerInstance - 1
	RuntimeLinuxIDBase    = 200000
)

func RuntimeGatewayPortBlockSize(runtimeType string) int {
	if normalized, ok := NormalizeV2RuntimeType(runtimeType); ok && normalized == RuntimeTypeHermes {
		return RuntimeHermesPortsPerInstance
	}
	return RuntimeOpenClawPortsPerInstance
}

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
