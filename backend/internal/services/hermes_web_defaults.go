package services

import "strings"

// Managed tenant deployments already provide this internal gateway Service.
// Never derive the trusted origin from a browser Host/Origin header.
func HermesControlUIOrigin(namespace, configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ""
	}
	return "http://clawmanager-gateway." + namespace + ".svc.cluster.local:9001"
}

// An operator can explicitly disable Web; normal managed deployments enable
// the authenticated capability-gated entry without requiring an extra flag.
func HermesWebEnabled(configured string) bool {
	value := strings.TrimSpace(configured)
	return value == "" || strings.EqualFold(value, "true")
}
