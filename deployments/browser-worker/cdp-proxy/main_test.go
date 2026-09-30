package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRewriteCDPResponseUsesExternalHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://upstream/json/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), externalHostKey, "clawbrowser-11:9222"))
	resp := &http.Response{
		Request: req,
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"webSocketDebuggerUrl":"ws://127.0.0.1:9223/devtools/browser/test"}`,
		)),
	}

	if err := rewriteCDPResponse(resp); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); !strings.Contains(got, "ws://clawbrowser-11:9222/devtools/browser/test") {
		t.Fatalf("rewritten body = %s", got)
	}
}

func TestRewriteCDPResponseLeavesNonJSONUntouched(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://upstream/", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `ws://127.0.0.1:9223/devtools/browser/test`
	resp := &http.Response{
		Request: req,
		Header:  http.Header{"Content-Type": []string{"text/plain"}},
		Body:    io.NopCloser(strings.NewReader(body)),
	}

	if err := rewriteCDPResponse(resp); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body changed: %q", got)
	}
}
