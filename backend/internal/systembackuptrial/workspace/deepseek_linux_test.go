//go:build linux

package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func deepSeekFixture(t *testing.T) (*os.Root, []byte) {
	t.Helper()
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	if e = root.MkdirAll(deepSeekModules+"/@deepseek-ai", 0700); e != nil {
		t.Fatal(e)
	}
	if e = root.MkdirAll(deepSeekModules+"/@tanstack", 0700); e != nil {
		t.Fatal(e)
	}
	for pkg, target := range map[string]string{"yaml": deepSeekImage + "/node_modules/yaml", "@deepseek-ai/dsh": deepSeekImage, "react-dom": deepSeekImage + "/node_modules/@deepseek-ai/dsh-client-ui-trajectory/node_modules/react-dom", "@tanstack/react-virtual": deepSeekImage + "/node_modules/@deepseek-ai/dsh-client-ui-trajectory/node_modules/@tanstack/react-virtual"} {
		if e = root.Symlink(target, deepSeekModules+"/"+pkg); e != nil {
			t.Fatal(e)
		}
	}
	if e = root.WriteFile(".dsh/session.json", []byte("synthetic session"), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if _, e = ArchiveWithLayout(context.Background(), root, &out, Limits{100, 1 << 20}, DeepSeekHarnessV1); e != nil {
		t.Fatal(e)
	}
	return root, out.Bytes()
}

func TestDeepSeekLayoutRoundTripDoesNotTraverseImagePackages(t *testing.T) {
	source, raw := deepSeekFixture(t)
	if _, e := Archive(context.Background(), source, io.Discard, Limits{100, 1 << 20}); !errors.Is(e, ErrArchive) {
		t.Fatal("generic archive accepted image links", e)
	}
	target, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	report, e := RestoreWithLayout(context.Background(), target, bytes.NewReader(raw), Limits{100, 1 << 20}, true, DeepSeekHarnessV1)
	if e != nil {
		t.Fatal(e)
	}
	if report.Bytes != int64(len("synthetic session")) {
		t.Fatal("image package payload traversed", report)
	}
	link, e := target.Readlink(deepSeekModules + "/@deepseek-ai/dsh")
	if e != nil || link != deepSeekImage {
		t.Fatal(link, e)
	}
	if _, e = target.ReadFile(deepSeekModules + "/@deepseek-ai/dsh/package.json"); e == nil {
		t.Fatal("os.Root traversed external package")
	}
	before, _ := source.Stat(deepSeekModules)
	after, _ := target.Stat(deepSeekModules)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("parent mtime lost")
	}
	if _, e = ArchiveWithLayout(context.Background(), target, io.Discard, Limits{100, 1 << 20}, DeepSeekHarnessV1); e != nil {
		t.Fatal("cannot recapture", e)
	}
}

func TestDeepSeekLayoutRejectsChangedTargetsAndPayloadBelowLinks(t *testing.T) {
	_, raw := deepSeekFixture(t)
	for _, attack := range []string{"etc", "traversal", "other-path", "wrong-image-prefix", "wrong-special", "nested-payload", "root-substitution", "unknown-layout", "generic"} {
		t.Run(attack, func(t *testing.T) {
			var out bytes.Buffer
			tw := tar.NewWriter(&out)
			tr := tar.NewReader(bytes.NewReader(raw))
			for {
				h, e := tr.Next()
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				if h.Name == deepSeekModules+"/yaml" {
					switch attack {
					case "etc":
						h.Linkname = "/etc/passwd"
					case "traversal":
						h.Linkname = deepSeekImage + "/node_modules/../../secret"
					case "other-path":
						h.Name = "workspace/yaml"
					case "wrong-image-prefix":
						h.Linkname = "/opt/dsh/node_modules/yaml"
					}
				}
				if h.Name == deepSeekModules+"/react-dom" && attack == "wrong-special" {
					h.Linkname = deepSeekImage + "/node_modules/react-dom"
				}
				if h.Name == ".dsh" && attack == "root-substitution" {
					h.Typeflag = tar.TypeSymlink
					h.Linkname = "elsewhere"
				}
				if e = tw.WriteHeader(h); e != nil {
					t.Fatal(e)
				}
				if _, e = io.Copy(tw, tr); e != nil {
					t.Fatal(e)
				}
			}
			if attack == "nested-payload" {
				if e := tw.WriteHeader(&tar.Header{Name: deepSeekModules + "/yaml/payload", Typeflag: tar.TypeReg, Mode: 0600, Size: 1}); e != nil {
					t.Fatal(e)
				}
				tw.Write([]byte("x"))
			}
			if e := tw.Close(); e != nil {
				t.Fatal(e)
			}
			target, e := os.OpenRoot(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer target.Close()
			layout := DeepSeekHarnessV1
			if attack == "generic" {
				layout = ""
			}
			if attack == "unknown-layout" {
				layout = "deepseek-unknown"
			}
			if _, e = RestoreWithLayout(context.Background(), target, bytes.NewReader(out.Bytes()), Limits{100, 1 << 20}, true, layout); !errors.Is(e, ErrArchive) {
				t.Fatal("unsafe archive accepted", e)
			}
		})
	}
}
