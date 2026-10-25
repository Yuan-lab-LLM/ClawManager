package services

import (
	"encoding/json"
	"net/http"
	"testing"
)

func desktopTestLogin(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost || r.Header.Get("Origin") != "http://clawmanager.internal:9001" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		t.Error("login must use fixed internal Origin without browser credentials")
	}
	var body map[string]string
	if json.NewDecoder(r.Body).Decode(&body) != nil || body["provider"] != "basic" || body["username"] != "clawmanager" || body["password"] != "managed-Hermes-password" || body["next"] != "/chat" {
		t.Error("login did not use managed instance credentials")
	}
	w.Header().Set("Content-Type", "application/json")
	http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private-upstream-cookie", Path: "/", HttpOnly: true})
	_, _ = w.Write([]byte(`{"ok":true}`))
}
