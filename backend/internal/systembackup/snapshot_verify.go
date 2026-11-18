package systembackup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// ValidatePublishedSnapshot adds format/coverage validation to authenticated
// decryption. It writes no restored data and needs no source-side connection.
// Successful ciphertext authentication alone cannot prove a usable snapshot.
func ValidatePublishedSnapshot(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, b CaptureBudget) error {
	return validatePublishedSnapshot(ctx, store, artifact, wrapper, b, nil)
}

func validatePublishedSnapshot(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, b CaptureBudget, workspaceEntry func(*tar.Header) error) error {
	if b.MaxBytes < 1<<20 || b.MaxItems < 1 || artifact.Manifest.Payload.Validate() != nil {
		return errors.New("validation_failed")
	}
	var total int64
	for _, p := range artifact.Manifest.Payload.Parts {
		if p.CiphertextBytes > b.MaxBytes-total {
			return errors.New("capacity_exceeded")
		}
		total += p.CiphertextBytes
		if err := ConsumePublishedPart(ctx, store, artifact, p.Name, wrapper, b.MaxBytes, func(r io.Reader) error {
			if p.Name == "mysql" && artifact.Manifest.Payload.Purpose == "team_data" {
				result, e := validateTeamDataSQLSnapshot(r, b)
				if e != nil {
					return e
				}
				if e = requireBusinessSQLInventory(result); e != nil {
					return e
				}
				for _, name := range artifact.Manifest.Payload.ExcludedSQLTables {
					if result.Tables[name] != 0 {
						return errors.New("Team data scope differs from SQL archive")
					}
				}
				return nil
			}
			if p.Name == "workspace" && workspaceEntry != nil {
				return validateFileSnapshot(ctx, r, FileBudget{MaxBytes: b.MaxBytes, MaxEntries: b.MaxItems}, workspaceEntry)
			}
			return ValidateSnapshotPart(ctx, p.Name, r, b)
		}); err != nil {
			return err
		}
	}
	return nil
}

func ValidateSnapshotPart(ctx context.Context, name string, r io.Reader, b CaptureBudget) error {
	if r == nil || b.MaxBytes < 1<<20 || b.MaxItems < 1 {
		return errors.New("validation_failed")
	}
	switch name {
	case "mysql":
		return ValidateBusinessSQLSnapshot(r)
	case "redis":
		return ReadRedisSnapshot(r, RedisBudget{MaxBytes: b.MaxBytes, MaxItems: b.MaxItems, MaxInventoryBytes: min(b.MaxBytes/8, 64<<20)}, nil)
	case "object_storage":
		return ReadObjectSnapshot(ctx, r, ObjectBudget{MaxBytes: b.MaxBytes, MaxItems: b.MaxItems}, nil)
	case "workspace":
		return ValidateFileSnapshot(ctx, r, FileBudget{MaxBytes: b.MaxBytes, MaxEntries: b.MaxItems})
	case "deployment_resources", "secret_bundle":
		return ReadKubernetesHistory(ctx, r, name, ObjectBudget{MaxBytes: b.MaxBytes, MaxItems: b.MaxItems}, nil)
	default:
		return errors.New("validation_failed")
	}
}

// ValidateFileSnapshot applies the restore archive rules without extracting
// plaintext. In particular, links can never turn a parent into a write escape.
func ValidateFileSnapshot(ctx context.Context, r io.Reader, b FileBudget) error {
	return validateFileSnapshot(ctx, r, b, nil)
}

func validateFileSnapshot(ctx context.Context, r io.Reader, b FileBudget, entry func(*tar.Header) error) error {
	if err := b.validate(); err != nil {
		return err
	}
	tr := tar.NewReader(contextReader{ctx, r})
	seen := map[string]byte{}
	parents := map[string]bool{}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !safeArchiveName(h.Name) || len(seen) >= b.MaxEntries || h.Size < 0 || h.Size > b.MaxBytes-total || h.Uid < 0 || h.Gid < 0 || !validArchiveFileMode(h.Typeflag, h.Mode) {
			return fmt.Errorf("file snapshot metadata invalid: %s mode=%o uid=%d gid=%d size=%d entries=%d total=%d", h.Name, h.Mode, h.Uid, h.Gid, h.Size, len(seen), total)
		}
		if _, ok := seen[h.Name]; ok {
			return errors.New("validation_failed")
		}
		for parent := path.Dir(h.Name); parent != "."; parent = path.Dir(parent) {
			if kind, ok := seen[parent]; ok && kind != tar.TypeDir {
				return errors.New("validation_failed")
			}
			parents[parent] = true
		}
		if parents[h.Name] && h.Typeflag != tar.TypeDir {
			return errors.New("validation_failed")
		}
		seen[h.Name] = h.Typeflag
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Size != 0 {
				return errors.New("validation_failed")
			}
		case tar.TypeSymlink:
			if h.Size != 0 || strings.ContainsAny(h.Linkname, "\\:\x00") || path.IsAbs(h.Linkname) || !safeArchiveName(path.Join(path.Dir(h.Name), h.Linkname)) {
				return errors.New("validation_failed")
			}
		case tar.TypeReg:
			n, err := io.Copy(io.Discard, tr)
			if err != nil {
				return err
			}
			if n != h.Size {
				return io.ErrUnexpectedEOF
			}
			total += n
		default:
			return errors.New("validation_failed")
		}
		if entry != nil {
			if err = entry(h); err != nil {
				return err
			}
		}
	}
	var extra [1]byte
	if n, err := io.ReadFull(contextReader{ctx, r}, extra[:]); n != 0 || err != io.EOF {
		return errors.New("validation_failed")
	}
	return nil
}
