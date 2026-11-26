// Package migrationcatalog computes the content-addressed catalog used by the
// system-backup contract. It has no database or deployment dependencies so the
// same algorithm can be tested independently from the migration runner.
package migrationcatalog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const (
	Algorithm = "sha256-length-prefixed-filename-normalized-content-v1"
	domain    = "system-backup-migration-catalog.v1\x00"
)

type Catalog struct {
	Algorithm string
	Filenames []string
	Hash      string
}

// Compute hashes all .sql files directly below directory. Filenames and LF
// normalized contents are framed with unsigned 64-bit big-endian byte lengths,
// matching scripts/system-backup-contract-check.mjs.
func Compute(filesystem fs.FS, directory string) (Catalog, error) {
	if filesystem == nil {
		return Catalog{}, fmt.Errorf("nil migration filesystem")
	}
	entries, err := fs.ReadDir(filesystem, directory)
	if err != nil {
		return Catalog{}, fmt.Errorf("list migration catalog: %w", err)
	}
	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			filenames = append(filenames, entry.Name())
		}
	}
	sort.Strings(filenames)

	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	for _, filename := range filenames {
		frame(hash, []byte(filename))
		content, err := fs.ReadFile(filesystem, path.Join(directory, filename))
		if err != nil {
			return Catalog{}, fmt.Errorf("read migration %s: %w", filename, err)
		}
		normalized := strings.ReplaceAll(strings.ReplaceAll(string(content), "\r\n", "\n"), "\r", "\n")
		frame(hash, []byte(normalized))
	}

	return Catalog{
		Algorithm: Algorithm,
		Filenames: filenames,
		Hash:      hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

type writer interface {
	Write([]byte) (int, error)
}

func frame(destination writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
