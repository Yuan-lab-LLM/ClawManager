package buildinfo

import (
	"os"
	"strings"
)

// These values are overridden with -ldflags for release images. Environment
// variables remain available as an explicit override for private deployments.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func Current() Info {
	return Info{
		Version:   resolvedValue("CLAWMANAGER_VERSION", Version, "dev"),
		Commit:    resolvedValue("CLAWMANAGER_COMMIT", Commit, "unknown"),
		BuildTime: resolvedValue("CLAWMANAGER_BUILD_TIME", BuildTime, "unknown"),
	}
}

func resolvedValue(environmentName, linkedValue, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(environmentName)); value != "" {
		return value
	}
	if value := strings.TrimSpace(linkedValue); value != "" {
		return value
	}
	return fallback
}
