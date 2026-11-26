package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	staging "clawreef/internal/systembackuptrial/envelope"
)

func TestEncryptedWorkspaceRoundTripAcrossAgentTypes(t *testing.T) {
	for _, agent := range []string{"openclaw", "hermes", "opencode", "deepseek-harness", "workbuddy", "codex", "claude-code"} {
		t.Run(agent, func(t *testing.T) {
			source := t.TempDir()
			dest := t.TempDir()
			files := map[string][]byte{"home/." + agent + "/config.json": []byte(`{"model":"synthetic"}`), "home/." + agent + "/sessions/history.sqlite-wal": {0, 1, 255}, "home/." + agent + "/skills/tool/SKILL.md": []byte("fixture skill"), "home/." + agent + "/tasks.json": []byte(`{"enabled":true}`), "project/report.txt": []byte("fixture file")}
			for n, b := range files {
				p := filepath.Join(source, filepath.FromSlash(n))
				if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(p, b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			r, e := os.OpenRoot(source)
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			d, e := os.OpenRoot(dest)
			if e != nil {
				t.Fatal(e)
			}
			defer d.Close()
			key := make([]byte, 32)
			binding := staging.Binding{BackupID: "fixture", Attempt: "one", Path: "workspace/" + agent, CaptureContextHash: string(bytes.Repeat([]byte{'a'}, 64)), EnvelopeID: "fixture-only"}
			var cipher bytes.Buffer
			limits := Limits{100, 1 << 20}
			e = staging.Seal(context.Background(), &cipher, key, binding, func(w io.Writer) error { _, e := Archive(context.Background(), r, w, limits); return e })
			if e != nil {
				t.Fatal(e)
			}
			// Test-only in-memory quarantine; production chains an inert restore sink.
			var plain bytes.Buffer
			if e = staging.Open(context.Background(), &cipher, &plain, key, binding, 1<<20); e != nil {
				t.Fatal(e)
			}
			if _, e = Restore(context.Background(), d, &plain, limits, false); e != nil {
				t.Fatal(e)
			}
			for n, want := range files {
				got, e := d.ReadFile(n)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatalf("lost %s", n)
				}
			}
			if _, e = Restore(context.Background(), d, bytes.NewReader(nil), limits, false); e == nil {
				t.Fatal("nonempty target accepted")
			}
		})
	}
}
func TestRestoreRejectsTraversalLinksDuplicatesAndCapacity(t *testing.T) {
	for _, kind := range []string{"traversal", "absolute", "backslash", "link-escape", "hard-link", "device", "duplicate", "symlink-parent", "capacity", "setuid"} {
		t.Run(kind, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			h := &tar.Header{Name: "data", Mode: 0600, Size: 1, Typeflag: tar.TypeReg}
			limits := Limits{10, 10}
			switch kind {
			case "traversal":
				h.Name = "../outside"
			case "absolute":
				h.Name = "/outside"
			case "backslash":
				h.Name = `..\outside`
			case "link-escape":
				h.Typeflag = tar.TypeSymlink
				h.Size = 0
				h.Linkname = "../outside"
			case "hard-link":
				h.Typeflag = tar.TypeLink
				h.Size = 0
				h.Linkname = "x"
			case "device":
				h.Typeflag = tar.TypeChar
				h.Size = 0
			case "capacity":
				limits.MaxBytes = 0
			case "setuid":
				h.Mode = 04755
			case "symlink-parent":
				h.Typeflag = tar.TypeSymlink
				h.Size = 0
				h.Linkname = "target"
			}
			tw.WriteHeader(h)
			if h.Size > 0 {
				tw.Write([]byte("x"))
			}
			if kind == "duplicate" {
				tw.WriteHeader(h)
				tw.Write([]byte("y"))
			}
			if kind == "symlink-parent" {
				tw.WriteHeader(&tar.Header{Name: "data/file", Mode: 0600, Size: 1})
				tw.Write([]byte("x"))
			}
			tw.Close()
			r, e := os.OpenRoot(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			if _, e = Restore(context.Background(), r, &buf, limits, false); e == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
