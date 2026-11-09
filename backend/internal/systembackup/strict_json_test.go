package systembackup

import (
	"strings"
	"testing"
)

func TestArchiveJSONRejectsAmbiguousAndUnknownFields(t *testing.T) {
	for _, raw := range []string{`{"kind":"other","kind":"mysql_rows_v1"}`, `{"kind":"mysql_rows_v1","unknown_policy":"execute"}`} {
		if _, e := ReadSQLSnapshot(strings.NewReader(raw+"\n{\"kind\":\"end\"}\n"), nil, nil); e == nil {
			t.Errorf("accepted ambiguous SQL JSON: %s", raw)
		}
	}
	for _, raw := range []string{`{"x":{"y":1,"y":2}}`, `{"x":1,"x":2}`, `{"x":[{"y":1,"y":2}]}`} {
		var out map[string]any
		if e := strictDecode([]byte(raw), &out); e == nil {
			t.Errorf("accepted duplicate JSON: %s", raw)
		}
	}
}
