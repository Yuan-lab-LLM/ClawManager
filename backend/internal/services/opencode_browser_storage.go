package services

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"clawreef/internal/models"
)

//go:embed opencode_browser_storage.js
var openCodeBrowserStorageScript string

var openCodeBrowserHeadPattern = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)

// Creation identity survives restarts and credential refreshes, but changes
// when an instance ID or browser origin is reused for a different workspace.
func openCodeBrowserStorageScope(instance *models.Instance) string {
	if instance == nil {
		return ""
	}
	workspace := ""
	if instance.WorkspacePath != nil {
		workspace = *instance.WorkspacePath
	}
	identity, _ := json.Marshal([]interface{}{
		instance.UserID, instance.ID, instance.CreatedAt.UTC().Format(time.RFC3339Nano), workspace,
	})
	digest := sha256.Sum256(identity)
	return fmt.Sprintf("%x", digest[:16])
}

func isOpenCodeBrowserDocumentRequest(r *http.Request, targetPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.TrimSpace(targetPath) == "/" ||
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") ||
		r.Header.Get("Sec-Fetch-Dest") == "document" || r.Header.Get("Sec-Fetch-Dest") == "iframe"
}

func injectOpenCodeBrowserStorageScope(html, scope string) string {
	const marker = `id="clawmanager-opencode-storage-scope"`
	if scope == "" || strings.Contains(html, marker) {
		return html
	}
	encoded, _ := json.Marshal(scope)
	script := "<script " + marker + ">" +
		strings.ReplaceAll(openCodeBrowserStorageScript, "__CLAWMANAGER_OPENCODE_SCOPE__", string(encoded)) + "</script>"
	if head := openCodeBrowserHeadPattern.FindStringIndex(html); head != nil {
		return html[:head[1]] + script + html[head[1]:]
	}
	return script + html
}
