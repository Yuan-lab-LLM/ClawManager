package services

import (
	"fmt"
	"os"
	"strings"

	"clawreef/internal/services/k8s"
)

// InstanceRuntimeConfig describes how a given instance type runs inside Kubernetes.
type InstanceRuntimeConfig struct {
	Image     string
	Port      int32
	MountPath string
	Env       map[string]string
}

func buildRuntimeConfig(instanceType, osType, osVersion string, registry, tag *string) InstanceRuntimeConfig {
	if registry != nil && strings.TrimSpace(*registry) != "" && (tag == nil || strings.TrimSpace(*tag) == "") {
		return InstanceRuntimeConfig{
			Image:     strings.TrimSpace(*registry),
			Port:      defaultPortForInstanceType(instanceType),
			MountPath: defaultMountPathForInstanceType(instanceType),
			Env:       defaultEnvForInstanceType(instanceType),
		}
	}

	defaultRegistry := "docker.io/clawreef"
	if registry != nil && *registry != "" {
		defaultRegistry = *registry
	}

	defaultTag := osVersion
	if tag != nil && *tag != "" {
		defaultTag = *tag
	}

	config := InstanceRuntimeConfig{
		Port:      3001,
		MountPath: "/home/user/data",
		Env:       map[string]string{},
	}

	switch instanceType {
	case "ubuntu":
		config.Image = "lscr.io/linuxserver/webtop:ubuntu-xfce"
		config.Port = 3001
		config.MountPath = "/config"
		config.Env = map[string]string{
			"TITLE":     "ClawManager Desktop",
			"SUBFOLDER": "/",
		}
	case "webtop":
		config.Image = "lscr.io/linuxserver/webtop:ubuntu-xfce"
		config.Port = 3001
		config.MountPath = "/config"
		config.Env = map[string]string{
			"TITLE":     "ClawManager Webtop",
			"SUBFOLDER": "/",
		}
	case "hermes":
		config.Image = defaultSystemImageSettings["hermes"]
		config.Port = 3001
		config.MountPath = "/config/.hermes"
		config.Env = map[string]string{
			"TITLE":     "Hermes Runtime",
			"SUBFOLDER": "/",
		}
	case "openclaw":
		config.MountPath = "/config"
		if (registry == nil || strings.TrimSpace(*registry) == "") && (tag == nil || strings.TrimSpace(*tag) == "") {
			config.Image = defaultSystemImageSettings["openclaw"]
		} else {
			config.Image = fmt.Sprintf("%s/%s:%s", defaultRegistry, "openclaw-desktop", defaultTag)
		}
		config.Env = map[string]string{
			"TITLE":     "ClawManager Desktop",
			"SUBFOLDER": "/",
		}
	case "debian":
		config.Image = fmt.Sprintf("%s/%s:%s", defaultRegistry, "debian-desktop", defaultTag)
	case "centos":
		config.Image = fmt.Sprintf("%s/%s:%s", defaultRegistry, "centos-desktop", defaultTag)
	default:
		config.Image = fmt.Sprintf("%s/%s:%s", defaultRegistry, fmt.Sprintf("%s-desktop", osType), defaultTag)
	}
	return config
}

func defaultPortForInstanceType(instanceType string) int32 {
	switch instanceType {
	case "ubuntu", "webtop", "hermes":
		return 3001
	default:
		return 3001
	}
}

func defaultMountPathForInstanceType(instanceType string) string {
	switch instanceType {
	case "ubuntu", "webtop", "openclaw":
		return "/config"
	case "hermes":
		return "/config/.hermes"
	default:
		return "/home/user/data"
	}
}

func defaultEnvForInstanceType(instanceType string) map[string]string {
	switch instanceType {
	case "ubuntu", "webtop", "openclaw":
		return map[string]string{
			"TITLE":     "ClawManager Desktop",
			"SUBFOLDER": "/",
		}
	case "hermes":
		return map[string]string{
			"TITLE":     "Hermes Runtime",
			"SUBFOLDER": "/",
		}
	default:
		return map[string]string{}
	}
}

func withInstanceProxyEnv(instanceType string, instanceID int, env map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range env {
		merged[key] = value
	}

	if proxyURL, ok := defaultEgressProxyURL(); ok {
		noProxy := defaultNoProxyList()
		merged["HTTP_PROXY"] = proxyURL
		merged["HTTPS_PROXY"] = proxyURL
		merged["http_proxy"] = proxyURL
		merged["https_proxy"] = proxyURL
		merged["NO_PROXY"] = noProxy
		merged["no_proxy"] = noProxy
	}

	if usesWebtopImage(instanceType) {
		merged["SUBFOLDER"] = fmt.Sprintf("/api/v1/instances/%d/proxy/", instanceID)
	}

	return merged
}

func usesWebtopImage(instanceType string) bool {
	switch instanceType {
	case "ubuntu", "webtop", "hermes", "openclaw":
		return true
	default:
		return false
	}
}

// defaultImagePullPolicy returns the image pull policy to use for instance
// pods. Managed runtime instances always use IfNotPresent so local caches can
// be reused without forcing a remote registry pull during create/start flows.
func defaultImagePullPolicy() string {
	return "IfNotPresent"
}

func defaultEgressProxyURL() (string, bool) {
	if override := strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_PROXY_URL")); override != "" {
		return override, true
	}

	client := k8s.GetClient()
	var systemNamespace string
	if overrideNamespace := strings.TrimSpace(os.Getenv("CLAWMANAGER_SYSTEM_NAMESPACE")); overrideNamespace != "" {
		systemNamespace = overrideNamespace
	} else if client != nil {
		systemNamespace = fmt.Sprintf("%s-system", client.Namespace)
	} else if baseNamespace := strings.TrimSpace(os.Getenv("K8S_NAMESPACE")); baseNamespace != "" {
		systemNamespace = fmt.Sprintf("%s-system", baseNamespace)
	}

	if strings.TrimSpace(systemNamespace) == "" {
		return "", false
	}

	serviceName := strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_PROXY_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_PROXY_SERVICE"))
	}
	if serviceName == "" {
		serviceName = "clawmanager-egress-proxy"
	}

	port := normalizePortValue(
		strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_PROXY_SERVICE_PORT")),
		strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_PROXY_PORT")),
	)
	if port == "" {
		port = "3128"
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%s", serviceName, systemNamespace, port), true
}

func defaultGatewayBaseURL() (string, bool) {
	if override := strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL")); override != "" {
		return override, true
	}

	systemNamespace := strings.TrimSpace(os.Getenv("CLAWMANAGER_SYSTEM_NAMESPACE"))
	if systemNamespace == "" {
		if client := k8s.GetClient(); client != nil {
			systemNamespace = client.GetSystemNamespace()
		} else if baseNamespace := strings.TrimSpace(os.Getenv("K8S_NAMESPACE")); baseNamespace != "" {
			systemNamespace = fmt.Sprintf("%s-system", baseNamespace)
		}
	}
	if systemNamespace == "" {
		return "", false
	}

	serviceName := strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE"))
	}
	if serviceName == "" {
		serviceName = "clawmanager-gateway"
	}

	port := normalizePortValue(
		strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_PORT")),
		strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_PORT")),
	)
	if port == "" {
		port = "9001"
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%s/api/v1/gateway/llm", serviceName, systemNamespace, port), true
}

func defaultAgentControlBaseURL() (string, bool) {
	if override := strings.TrimSpace(os.Getenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL")); override != "" {
		return override, true
	}

	systemNamespace := strings.TrimSpace(os.Getenv("CLAWMANAGER_SYSTEM_NAMESPACE"))
	if systemNamespace == "" {
		if client := k8s.GetClient(); client != nil {
			systemNamespace = client.GetSystemNamespace()
		} else if baseNamespace := strings.TrimSpace(os.Getenv("K8S_NAMESPACE")); baseNamespace != "" {
			systemNamespace = fmt.Sprintf("%s-system", baseNamespace)
		}
	}
	if systemNamespace == "" {
		return "", false
	}

	serviceName := strings.TrimSpace(os.Getenv("CLAWMANAGER_AGENT_CONTROL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_NAME"))
	}
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE"))
	}
	if serviceName == "" {
		serviceName = "clawmanager-gateway"
	}

	port := normalizePortValue(
		strings.TrimSpace(os.Getenv("CLAWMANAGER_AGENT_CONTROL_SERVICE_PORT")),
		strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_SERVICE_PORT")),
		strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GATEWAY_PORT")),
	)
	if port == "" {
		port = "9001"
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%s", serviceName, systemNamespace, port), true
}

func defaultNoProxyList() string {
	if override := strings.TrimSpace(os.Getenv("CLAWMANAGER_NO_PROXY")); override != "" {
		return override
	}

	systemNamespace := strings.TrimSpace(os.Getenv("CLAWMANAGER_SYSTEM_NAMESPACE"))
	if systemNamespace == "" {
		if client := k8s.GetClient(); client != nil {
			systemNamespace = fmt.Sprintf("%s-system", client.Namespace)
		} else if baseNamespace := strings.TrimSpace(os.Getenv("K8S_NAMESPACE")); baseNamespace != "" {
			systemNamespace = fmt.Sprintf("%s-system", baseNamespace)
		}
	}

	serviceNames := []string{
		"localhost",
		"127.0.0.1",
		"clawmanager-frontend",
		"clawmanager-gateway",
		"clawmanager-egress-proxy",
	}

	if systemNamespace != "" {
		serviceNames = append(serviceNames,
			fmt.Sprintf("clawmanager-frontend.%s", systemNamespace),
			fmt.Sprintf("clawmanager-frontend.%s.svc", systemNamespace),
			fmt.Sprintf("clawmanager-frontend.%s.svc.cluster.local", systemNamespace),
			fmt.Sprintf("clawmanager-gateway.%s", systemNamespace),
			fmt.Sprintf("clawmanager-gateway.%s.svc", systemNamespace),
			fmt.Sprintf("clawmanager-gateway.%s.svc.cluster.local", systemNamespace),
			fmt.Sprintf("clawmanager-egress-proxy.%s", systemNamespace),
			fmt.Sprintf("clawmanager-egress-proxy.%s.svc", systemNamespace),
			fmt.Sprintf("clawmanager-egress-proxy.%s.svc.cluster.local", systemNamespace),
		)
	}

	return strings.Join(serviceNames, ",")
}

func normalizePortValue(values ...string) string {
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "://") {
			return value
		}

		lastColon := strings.LastIndex(value, ":")
		if lastColon >= 0 && lastColon < len(value)-1 {
			return value[lastColon+1:]
		}
	}

	return ""
}
