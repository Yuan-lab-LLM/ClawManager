package systembackuptrial

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReadBackupSelection(t *testing.T) {
	for _, test := range []struct {
		name, body, mode string
		invalid          bool
	}{
		{name: "original quick backup"},
		{name: "explicit all", body: `{"mode":"all"}`, mode: "all"},
		{name: "live Hermes selection", body: `{"mode":"all","hermes_sessions":{"user_id":20,"instance_id":202,"session_ids":["child"]}}`, mode: "all"},
		{name: "workspace directory", body: `{"mode":"all","workspace_directory":{"user_id":20,"instance_id":202,"relative_dir":"projects/alpha"}}`, mode: "all"},
		{name: "unsafe workspace directory", body: `{"mode":"all","workspace_directory":{"user_id":20,"instance_id":202,"relative_dir":"projects/../alpha"}}`, invalid: true},
		{name: "mixed workspace modes", body: `{"mode":"all","workspace_selection_sha256":"` + strings.Repeat("a", 64) + `","workspace_directory":{"user_id":20,"instance_id":202,"relative_dir":"projects/alpha"}}`, invalid: true},
		{name: "duplicate Hermes selection", body: `{"mode":"all","hermes_sessions":{"user_id":20,"instance_id":202,"session_ids":["child","child"]}}`, invalid: true},
		{name: "mixed Hermes modes", body: `{"mode":"all","hermes_history_sha256":"` + strings.Repeat("a", 64) + `","hermes_sessions":{"user_id":20,"instance_id":202,"session_ids":["child"]}}`, invalid: true},
		{name: "selected owner", body: `{"mode":"selected","instances":[{"id":202,"user_id":20}]}`, mode: "selected"},
		{name: "empty scope", body: `{"mode":"selected"}`, invalid: true},
		{name: "empty object", body: `{}`, invalid: true},
		{name: "all with a filter", body: `{"mode":"all","user_ids":[20]}`, invalid: true},
		{name: "duplicate instance", body: `{"mode":"selected","instances":[{"id":202,"user_id":20},{"id":202,"user_id":30}]}`, invalid: true},
		{name: "unknown field", body: `{"mode":"all","directory":"anything"}`, invalid: true},
		{name: "invalid digest", body: `{"mode":"all","hermes_history_sha256":"invalid"}`, invalid: true},
		{name: "trailing object", body: `{"mode":"all"}{}`, invalid: true},
		{name: "oversize", body: `{"mode":"all"}` + strings.Repeat(" ", 64<<10), invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(test.body))
			selection, err := readBackupSelection(c)
			if test.invalid {
				if err == nil || selection != nil {
					t.Fatal("invalid request was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.mode == "" && selection != nil || test.mode != "" && (selection == nil || selection.Mode != test.mode) {
				t.Fatalf("unexpected backup selection %v", selection)
			}
		})
	}
}
