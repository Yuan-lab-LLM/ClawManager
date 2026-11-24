//go:build linux

package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

func TestWorkBuddyInstanceWineKeepsMarkerAndConfinesMapping(t *testing.T) {
	_, raw := wineFixture(t)
	var wrapped bytes.Buffer
	tw := tar.NewWriter(&wrapped)
	if e := tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0755}); e != nil {
		t.Fatal(e)
	}
	marker := []byte(`{"schema":1,"instanceId":7,"userId":4,"uid":21000,"gid":21000}`)
	if e := tw.WriteHeader(&tar.Header{Name: ".workbuddy-identity.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(marker))}); e != nil {
		t.Fatal(e)
	}
	if _, e := tw.Write(marker); e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if h.Name == "." {
			h.Name = "home"
		} else {
			h.Name = "home/" + h.Name
		}
		h.PAXRecords = nil
		if e = tw.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if _, e = io.Copy(tw, tr); e != nil {
			t.Fatal(e)
		}
	}
	if e := tw.Close(); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if _, e = RestoreWithLayout(context.Background(), root, bytes.NewReader(wrapped.Bytes()), Limits{100, 1 << 20}, false, WorkBuddyInstanceWineV1); e != nil {
		t.Fatal(e)
	}
	if got, e := root.ReadFile(".workbuddy-identity.json"); e != nil || !bytes.Equal(got, marker) {
		t.Fatal("marker lost", e)
	}
	if got, e := root.Readlink("home/" + wineZ); e != nil || got != "/" {
		t.Fatal("mapping lost", e)
	}
	if _, e = root.ReadFile("home/" + wineZ + "/etc/passwd"); e == nil {
		t.Fatal("mapping escaped root")
	}
	var second bytes.Buffer
	if _, e = ArchiveWithLayout(context.Background(), root, &second, Limits{100, 1 << 20}, WorkBuddyInstanceWineV1); e != nil {
		t.Fatal(e)
	}
	if _, e = ArchiveWithLayout(context.Background(), root, io.Discard, Limits{100, 1 << 20}, WorkBuddyWineV1); e == nil {
		t.Fatal("HOME layout accepted an instance-root Wine mapping")
	}
	if e = root.Remove("home/" + wineZ); e != nil {
		t.Fatal(e)
	}
	if e = root.Symlink("/etc", "home/"+wineZ); e != nil {
		t.Fatal(e)
	}
	if _, e = ArchiveWithLayout(context.Background(), root, io.Discard, Limits{100, 1 << 20}, WorkBuddyInstanceWineV1); e == nil {
		t.Fatal("redirected Wine mapping accepted")
	}
}
