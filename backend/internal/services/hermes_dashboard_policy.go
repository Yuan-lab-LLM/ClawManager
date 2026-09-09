package services

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	hermesDashboardUpstream  = "upstream"
	hermesDashboardLocalJSON = "local_json"
	hermesDashboardWSTicket  = "ws_ticket"
)

type hermesDashboardHTTPRule struct {
	Kind      string
	Path      string
	Query     url.Values
	LocalJSON []byte
}

var (
	hermesDashboardChannel = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	hermesDashboardAttach  = regexp.MustCompile(`^[a-f0-9]{32}$`)
	hermesDashboardTicket  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	hermesDashboardSource  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	hermesDashboardAsset   = regexp.MustCompile(`^/(assets|fonts|fonts-terminal|ds-assets)/[A-Za-z0-9_./-]+\.(js|css|woff2?|ttf|otf|svg|png|jpe?g|webp|avif|gif|ico|wasm)$`)
)

// Policy covers the pinned classic dashboard's chat/history surface, not its
// machine-management console. Authentication and authorization MUST precede
// local responses too. The caller must still sanitize every upstream JSON body,
// suppress upstream cookies/redirects and never forward browser auth headers.
func hermesDashboardHTTPPolicy(method, targetPath string, query url.Values) (hermesDashboardHTTPRule, error) {
	rule := hermesDashboardHTTPRule{Kind: hermesDashboardUpstream, Path: targetPath, Query: make(url.Values)}
	deny := func() (hermesDashboardHTTPRule, error) { return hermesDashboardHTTPRule{}, ErrHermesDesktopForbidden }
	if !hermesDashboardCanonicalPath(targetPath) {
		return deny()
	}
	if targetPath == "/api/auth/ws-ticket" {
		if method != http.MethodPost || len(query) != 0 {
			return deny()
		}
		rule.Kind = hermesDashboardWSTicket
		return rule, nil
	}
	if method != http.MethodGet && method != http.MethodHead {
		return deny()
	}

	allowed := map[string]bool{}
	local := ""
	switch targetPath {
	case "/", "/chat", "/chat/", "/sessions", "/sessions/":
		rule.Path = "/chat" // Preserve browser SPA location; fetch one reviewed shell.
		allowed["resume"] = true
		allowed["profile"] = true
	case "/favicon.ico":
	case "/api/auth/me":
		// This is a CM-managed identity marker, never an upstream Session.
		local = `{"user_id":"clawmanager-managed","email":"","display_name":"ClawManager","org_id":"","provider":"clawmanager","expires_at":0}`
	case "/api/auth/mode":
		local = `{"auth_required":true,"mode":"clawmanager","managed":true}`
	case "/api/auth/providers":
		local = `{"providers":[]}`
	case "/api/profiles":
		// Do not enumerate machine-level profiles. The empty management profile
		// means the gateway process's isolated HERMES_HOME, not a named default.
		local = `{"profiles":[]}`
	case "/api/profiles/active":
		local = `{"active":"current","current":"current"}`
	case "/api/dashboard/plugins":
		// Plugin JS/API routes would bypass this reviewed browser/API surface.
		local = `[]`
	case "/api/config":
		// CM's managed chat surface has fixed presentation defaults. Do not
		// fetch the runtime config: it can contain provider keys or host paths.
		allowed["profile"] = true
		local = `{"dashboard":{"show_token_analytics":false},"agent":{"reasoning_effort":null}}`
	case "/api/status", "/api/model/info":
		allowed["profile"] = true
	case "/api/dashboard/themes", "/api/dashboard/font":
	case "/api/model/options":
		for _, key := range []string{"profile", "include_unconfigured", "explicit_only"} {
			allowed[key] = true
		}
	case "/api/sessions":
		for _, key := range []string{"profile", "limit", "offset", "min_messages", "archived", "order", "source", "sources", "exclude_sources"} {
			allowed[key] = true
		}
	case "/api/sessions/search":
		for _, key := range []string{"profile", "q", "limit", "source", "sources", "exclude_sources"} {
			allowed[key] = true
		}
	case "/api/sessions/stats", "/api/sessions/empty/count":
		allowed["profile"] = true
	default:
		parts := strings.Split(strings.TrimPrefix(targetPath, "/"), "/")
		if hermesDashboardAsset.MatchString(targetPath) {
			// Pinned assets are content-hashed; arbitrary asset query arguments
			// are unnecessary and could conceal credential material in logs.
		} else if len(parts) >= 3 && len(parts) <= 4 && parts[0] == "api" && parts[1] == "sessions" && hermesDesktopSessionID.MatchString(parts[2]) {
			allowed["profile"] = true
			if len(parts) == 4 {
				switch parts[3] {
				case "messages":
					allowed["limit"], allowed["offset"], allowed["order"] = true, true, true
				case "latest-descendant":
				default:
					return deny()
				}
			}
		} else if len(parts) == 2 && parts[0] == "sessions" && hermesDesktopSessionID.MatchString(parts[1]) {
			rule.Path = "/chat"
			allowed["profile"] = true
		} else {
			return deny()
		}
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return deny()
		}
		value := values[0]
		switch key {
		case "profile":
			// Even "default" may refer to a different home; never silently erase
			// an explicitly selected named profile, including "current".
			if value != "" {
				return deny()
			}
			continue
		case "resume":
			if !hermesDesktopSessionID.MatchString(value) {
				return deny()
			}
		case "limit", "offset", "min_messages":
			maximum := 10000
			if key == "limit" {
				maximum = 100
				if strings.HasSuffix(targetPath, "/messages") {
					maximum = 500 // stock getSessionMessages requests limit=500
				}
			}
			if !hermesDashboardNumber(value, maximum) {
				return deny()
			}
		case "order":
			if targetPath == "/api/sessions" {
				if value != "recent" && value != "created" {
					return deny()
				}
			} else if value != "latest" && value != "oldest" {
				return deny()
			}
		case "archived":
			if value != "exclude" {
				return deny()
			}
		case "include_unconfigured", "explicit_only":
			if value != "1" && value != "true" {
				return deny()
			}
		case "source", "sources", "exclude_sources":
			items := strings.Split(value, ",")
			if len(items) > 32 || (key == "source" && len(items) != 1) {
				return deny()
			}
			for _, item := range items {
				if !hermesDashboardSource.MatchString(item) {
					return deny()
				}
			}
		case "q":
			if len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
				return deny()
			}
		}
		rule.Query[key] = []string{value}
	}
	if local != "" {
		rule.Kind, rule.LocalJSON = hermesDashboardLocalJSON, []byte(local)
	}
	return rule, nil
}

// Returns only business parameters for the upstream URL. A query ticket is a
// CM-owned credential: callers must validate/consume it separately (or obtain
// it from the browser subprotocol) and mint a NEW server-only Hermes ticket.
// The returned URL must never receive upstream ticket/token query parameters.
func hermesDashboardWebSocketPolicy(targetPath string, query url.Values) (url.Values, error) {
	allowed := map[string]bool{"ticket": true, "profile": true}
	switch targetPath {
	case "/api/pty":
		for _, key := range []string{"channel", "resume", "fresh", "attach"} {
			allowed[key] = true
		}
	case "/api/events":
		allowed["channel"] = true
	case "/api/ws":
	default:
		return nil, ErrHermesDesktopForbidden
	}
	clean := make(url.Values)
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return nil, ErrHermesDesktopForbidden
		}
		value := values[0]
		switch key {
		case "ticket":
			if len(value) > 4096 || !hermesDashboardTicket.MatchString(value) {
				return nil, ErrHermesDesktopForbidden
			}
			continue
		case "profile":
			if value != "" {
				return nil, ErrHermesDesktopForbidden
			}
			continue
		case "channel":
			if !hermesDashboardChannel.MatchString(value) {
				return nil, ErrHermesDesktopForbidden
			}
		case "resume":
			if !hermesDesktopSessionID.MatchString(value) {
				return nil, ErrHermesDesktopForbidden
			}
		case "fresh":
			if value != "1" {
				return nil, ErrHermesDesktopForbidden
			}
		case "attach":
			if !hermesDashboardAttach.MatchString(value) {
				return nil, ErrHermesDesktopForbidden
			}
		}
		clean[key] = []string{value}
	}
	if targetPath != "/api/ws" && clean.Get("channel") == "" {
		return nil, ErrHermesDesktopForbidden
	}
	return clean, nil
}

// Only the classic sidebar's small sidecar speaks JSON-RPC. /api/pty carries
// raw terminal bytes and /api/events is receive-only; do NOT apply this parser
// to either. The pinned ChatSidebar uses a disposable session only to probe the
// connection and ignores the result; its model and PTY events have separate
// channels. Adapt that exact legacy request to a real, stateless Hermes ping.
// This is NOT a session.create API: no session ID or creation success is made
// up. Creating a web session instead would allocate an unused agent and depend
// on the upstream orphan grace reaper, since Lite rejects close_on_disconnect.
// Desktop session creation has its own policy and is unaffected.
func hermesDashboardFilterRPC(frame []byte) ([]byte, error) {
	var packet struct {
		JSONRPC string                     `json:"jsonrpc"`
		ID      json.RawMessage            `json:"id"`
		Method  string                     `json:"method"`
		Params  map[string]json.RawMessage `json:"params,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&packet) != nil || packet.JSONRPC != "2.0" || len(packet.ID) == 0 || len(packet.ID) > 256 || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrHermesDesktopForbidden
	}
	var id any
	if json.Unmarshal(packet.ID, &id) != nil {
		return nil, ErrHermesDesktopForbidden
	}
	switch id.(type) {
	case string, float64:
	default:
		return nil, ErrHermesDesktopForbidden
	}
	if packet.Method == "gateway.ping" {
		packet.Method = "ping"
	}
	switch packet.Method {
	case "ping":
		if len(packet.Params) != 0 {
			return nil, ErrHermesDesktopForbidden
		}
	case "session.create":
		if string(packet.Params["source"]) != `"tool"` || string(packet.Params["close_on_disconnect"]) != "true" {
			return nil, ErrHermesDesktopForbidden
		}
		for key, value := range packet.Params {
			switch key {
			case "source", "close_on_disconnect":
			case "profile":
				if string(value) != `""` {
					return nil, ErrHermesDesktopForbidden
				}
			default:
				return nil, ErrHermesDesktopForbidden
			}
		}
		packet.Method = "ping"
		packet.Params = nil
	default:
		return nil, ErrHermesDesktopForbidden
	}
	return json.Marshal(packet)
}

func hermesDashboardCanonicalPath(value string) bool {
	if value == "" || value[0] != '/' || strings.ContainsAny(value, "%\\?#\r\n\x00") || strings.Contains(value, "//") {
		return false
	}
	canonical := strings.TrimSuffix(value, "/")
	if canonical == "" {
		canonical = "/"
	}
	return path.Clean(value) == canonical
}

func hermesDashboardNumber(value string, maximum int) bool {
	if value == "" || strings.Trim(value, "0123456789") != "" || len(value) > 6 {
		return false
	}
	number, err := strconv.Atoi(value)
	return err == nil && number <= maximum
}
