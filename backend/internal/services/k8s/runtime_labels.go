package k8s

import "strings"

func appendManagedRuntimeLabels(instanceType string, labels map[string]string) {
	if labels == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "openclaw", "hermes", "opencode":
		labels["clawmanager.io/managed-runtime"] = "true"
	}
}
