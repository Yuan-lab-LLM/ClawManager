package services

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestRewriteOpenCodeFileSearchDirectory(t *testing.T) {
	query := url.Values{
		"query":               {"mcpserver"},
		"location[directory]": {"/workspaces/opencode/user-1/instance-7/home"},
	}

	if !rewriteOpenCodeFileSearchDirectory("/api/fs/find", "/workspaces/opencode/user-1/instance-7", query) {
		t.Fatal("expected OpenCode HOME search to be rewritten")
	}
	if got, want := query.Get("location[directory]"), "/workspaces/opencode/user-1/instance-7"; got != want {
		t.Fatalf("rewritten directory = %q, want %q", got, want)
	}
	if got := query.Get("query"); got != "mcpserver" {
		t.Fatalf("search query changed to %q", got)
	}
}

func TestRewriteOpenCodeFileSearchDirectoryLeavesProjectDirectoryAlone(t *testing.T) {
	query := url.Values{
		"location[directory]": {"/workspaces/opencode/user-1/instance-7/workspace/project-a"},
	}

	if rewriteOpenCodeFileSearchDirectory("/api/fs/find", "/workspaces/opencode/user-1/instance-7", query) {
		t.Fatal("did not expect a project-directory search to be rewritten")
	}
	if got, want := query.Get("location[directory]"), "/workspaces/opencode/user-1/instance-7/workspace/project-a"; got != want {
		t.Fatalf("directory = %q, want %q", got, want)
	}
}

func TestRewriteOpenCodeFindFileDirectory(t *testing.T) {
	query := url.Values{
		"query":     {"mcp"},
		"dirs":      {"true"},
		"directory": {"/workspaces/opencode/user-1/instance-7/home"},
	}

	if !rewriteOpenCodeFileSearchDirectory("/find/file", "/workspaces/opencode/user-1/instance-7", query) {
		t.Fatal("expected OpenCode /find/file HOME search to be rewritten")
	}
	if got, want := query.Get("directory"), "/workspaces/opencode/user-1/instance-7"; got != want {
		t.Fatalf("rewritten directory = %q, want %q", got, want)
	}
}

func TestRewriteOpenCodeFileSearchResponseAddsProjectRootAndHidesHome(t *testing.T) {
	body := []byte(`{"location":{"directory":"/workspaces/opencode/user-1/instance-2"},"data":[{"path":"mcpserver/server/","type":"directory"},{"path":"mcpserver/baidu_map/","type":"directory"},{"path":"home/mcpserver/server/","type":"directory"}]}`)

	modified, changed := rewriteOpenCodeFileSearchResponse("/api/fs/find", body)
	if !changed {
		t.Fatal("expected OpenCode directory results to be rewritten")
	}
	var payload openCodeFileSearchPayload
	if err := json.Unmarshal(modified, &payload); err != nil {
		t.Fatalf("decode modified response: %v", err)
	}
	want := []openCodeFileSearchItem{
		{Path: "mcpserver/", Type: "directory"},
		{Path: "mcpserver/server/", Type: "directory"},
		{Path: "mcpserver/baidu_map/", Type: "directory"},
	}
	if len(payload.Data) != len(want) {
		t.Fatalf("result count = %d, want %d: %#v", len(payload.Data), len(want), payload.Data)
	}
	for i := range want {
		if payload.Data[i] != want[i] {
			t.Fatalf("result[%d] = %#v, want %#v", i, payload.Data[i], want[i])
		}
	}
}

func TestRewriteOpenCodeFindFileResponseAddsProjectRootAndHidesHome(t *testing.T) {
	body := []byte(`["mcpserver/server/mcps_server.py","mcpserver/baidu_map/map.py","mcpserver/server/","mcpserver/baidu_map/","home/mcpserver/server/"]`)

	modified, changed := rewriteOpenCodeFileSearchResponse("/find/file", body)
	if !changed {
		t.Fatal("expected OpenCode /find/file results to be rewritten")
	}
	var got []string
	if err := json.Unmarshal(modified, &got); err != nil {
		t.Fatalf("decode modified response: %v", err)
	}
	want := []string{"mcpserver/", "mcpserver/server/", "mcpserver/baidu_map/"}
	if len(got) != len(want) {
		t.Fatalf("result count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
