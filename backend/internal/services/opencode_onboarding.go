package services

import (
	_ "embed"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

//go:embed opencode_onboarding.js
var openCodeOnboardingScript string

// OpenCode's precached index.html bypasses the authenticated HTML proxy.
// Replace the upstream worker so navigations continue through that proxy.
func serveOpenCodeOnlineOnlyServiceWorker(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`self.addEventListener("install",event=>event.waitUntil(self.skipWaiting()));self.addEventListener("activate",event=>event.waitUntil(self.clients.claim()));`))
}

var openCodeBrowserHeadClosePattern = regexp.MustCompile(`(?i)</head\s*>`)

// injectOpenCodeOnboarding adds a self-contained help panel to OpenCode Lite's
// browser document. It deliberately inserts near the end of <head>, after the
// storage-scope bootstrap that is inserted at the beginning of <head>. This
// keeps the dismissal preference isolated to the current instance.
func injectOpenCodeOnboarding(html string, canCreateProject bool) string {
	const marker = `id="clawmanager-opencode-onboarding"`
	if html == "" || strings.Contains(html, marker) {
		return html
	}

	scriptBody := strings.ReplaceAll(openCodeOnboardingScript, "__CLAWMANAGER_OPENCODE_CAN_CREATE__", strconv.FormatBool(canCreateProject))
	script := `<script ` + marker + `>` + scriptBody + `</script>`
	if headClose := openCodeBrowserHeadClosePattern.FindStringIndex(html); headClose != nil {
		return html[:headClose[0]] + script + html[headClose[0]:]
	}
	return html + script
}
