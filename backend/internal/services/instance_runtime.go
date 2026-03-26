package services

import (
	"fmt"
	"strings"
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
	case "webtop":
		config.Image = "lscr.io/linuxserver/webtop:ubuntu-xfce"
		config.Port = 3001
		config.MountPath = "/config"
		config.Env = map[string]string{
			"CUSTOM_USER": "abc",
			"PASSWORD":    "",
			"TITLE":       "ClawManager Webtop",
			"SUBFOLDER":   "/",
		}
	case "openclaw":
		if (registry == nil || strings.TrimSpace(*registry) == "") && (tag == nil || strings.TrimSpace(*tag) == "") {
			config.Image = defaultSystemImageSettings["openclaw"]
		} else {
			config.Image = fmt.Sprintf("%s/%s:%s", defaultRegistry, "openclaw-desktop", defaultTag)
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
	case "ubuntu", "webtop":
		return 3001
	default:
		return 3001
	}
}

func defaultMountPathForInstanceType(instanceType string) string {
	switch instanceType {
	case "ubuntu", "webtop":
		return "/config"
	default:
		return "/home/user/data"
	}
}

func defaultEnvForInstanceType(instanceType string) map[string]string {
	switch instanceType {
	case "webtop":
		return map[string]string{
			"CUSTOM_USER": "abc",
			"PASSWORD":    "",
			"TITLE":       "ClawManager Webtop",
			"SUBFOLDER":   "/",
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

	if usesWebtopImage(instanceType) {
		merged["SUBFOLDER"] = fmt.Sprintf("/api/v1/instances/%d/proxy/", instanceID)
	}

	return merged
}

func usesWebtopImage(instanceType string) bool {
	switch instanceType {
	case "ubuntu", "webtop", "openclaw":
		return true
	default:
		return false
	}
}
