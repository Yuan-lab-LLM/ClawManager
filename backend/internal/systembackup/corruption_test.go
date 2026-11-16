package systembackup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func corruptTestFileByte(t *testing.T, path string, offset int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	var value [1]byte
	if _, err := f.ReadAt(value[:], offset); err != nil {
		t.Fatal(err)
	}
	// A fixed replacement can equal the randomized ciphertext already on disk.
	value[0] ^= 1
	if _, err := f.WriteAt(value[:], offset); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptTestFileByteAlwaysChangesContent(t *testing.T) {
	for _, original := range []byte{0, 42, 255} {
		path := filepath.Join(t.TempDir(), "sealed.enc")
		before := []byte{10, original, 20}
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
		corruptTestFileByte(t, path, 1)
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) || after[1] == before[1] || !bytes.Equal(after[:1], before[:1]) || !bytes.Equal(after[2:], before[2:]) {
			t.Fatalf("corruption must change only the target byte: before=%v after=%v", before, after)
		}
	}
}
