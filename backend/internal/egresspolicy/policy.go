package egresspolicy

import (
	"os"
	"strings"
)

type Mode string

const (
	ModeOpen      Mode = "open"
	ModeDenylist  Mode = "denylist"
	ModeAllowlist Mode = "allowlist"
)

type Policy struct {
	Mode                Mode
	DeniedHostSuffixes  []string
	AllowedHostSuffixes []string
}

func LoadFromEnv() Policy {
	mode := Mode(strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMANAGER_EGRESS_LLM_POLICY"))))
	if mode == "" {
		mode = ModeDenylist
	}

	policy := Policy{
		Mode: mode,
		DeniedHostSuffixes: append(defaultDeniedHostSuffixes(),
			splitCSV(os.Getenv("CLAWMANAGER_EGRESS_DENIED_SUFFIXES"))...),
		AllowedHostSuffixes: append(defaultAllowedHostSuffixes(),
			splitCSV(os.Getenv("CLAWMANAGER_EGRESS_ALLOWED_SUFFIXES"))...),
	}
	return policy
}

func defaultDeniedHostSuffixes() []string {
	return []string{
		"api.openai.com",
		"openai.azure.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
		"api.deepseek.com",
		"api.moonshot.cn",
		"open.bigmodel.cn",
		"dashscope.aliyuncs.com",
	}
}

func defaultAllowedHostSuffixes() []string {
	return []string{
		"github.com",
		"registry-1.docker.io",
		"pypi.org",
		"npmjs.org",
		"clawmanager-gateway",
		"clawmanager-egress-proxy",
	}
}

func (p Policy) AllowHost(host string) (bool, string) {
	host = normalizeHost(host)
	if host == "" {
		return false, "empty host"
	}

	switch p.Mode {
	case ModeOpen, "":
		return true, ""
	case ModeAllowlist:
		if matchesAnySuffix(host, p.AllowedHostSuffixes) {
			return true, ""
		}
		return false, "host not in allowlist"
	default:
		if matchesAnySuffix(host, p.DeniedHostSuffixes) {
			return false, "llm provider host blocked"
		}
		return true, ""
	}
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return strings.Trim(host, ".")
}

func matchesAnySuffix(host string, suffixes []string) bool {
	for _, suffix := range suffixes {
		suffix = strings.TrimSpace(strings.ToLower(suffix))
		if suffix == "" {
			continue
		}
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
