package browserorigin

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ServeBootstrapDocument starts a new navigation from the application origin.
// HTTP redirects retain the manager's same-site Fetch Metadata and would make
// the clean, cookie-only request fail the cross-port boundary. Call only after
// authenticating a bootstrap capability, with a caller-generated clean URL.
// It returns false for requests that should keep their existing HTTP behavior.
func ServeBootstrapDocument(w http.ResponseWriter, r *http.Request, location string) bool {
	dest := r.Header.Get("Sec-Fetch-Dest")
	if r.Method != http.MethodGet || (dest != "document" && dest != "iframe") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	u, err := url.Parse(location)
	if err != nil || !strings.HasPrefix(location, "/") || strings.HasPrefix(location, "//") || strings.ContainsAny(location, "\\\r\n\t") || u.IsAbs() || u.Host != "" || u.User != nil || u.Opaque != "" || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		http.Error(w, "Invalid bootstrap location", http.StatusBadRequest)
		return true
	}
	encoded, _ := json.Marshal(location)
	// Browsers do not send the initial fragment to the server. Keep it unless
	// the validated preview target supplied an explicit fragment of its own.
	script := `"use strict";let target=` + string(encoded) + `;if(!target.includes("#"))target+=location.hash;location.replace(target);`
	hash := sha256.Sum256([]byte(script))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; base-uri 'none'; form-action 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Opening application</title><script>`+script+`</script>`)
	return true
}
