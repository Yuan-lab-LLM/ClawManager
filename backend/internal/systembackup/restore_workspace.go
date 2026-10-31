package systembackup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// PrepareRestoreWorkspace reuses the owned single-node volume lifecycle. The
// source namespace UID must be derived from the authenticated source inventory.
// It provisions a fresh directory, not a business app or historical resources.
// SQL/Redis/object identities, network isolation and revocation remain separate
// prerequisites; this workspace alone never proves a safe completed drill.
func (d LocalStageVolumes) PrepareRestoreWorkspace(ctx context.Context, s Store, l Leadership, t Task, r StageVolumeRequest) (StageVolume, bool, error) {
	var zero StageVolume
	if t.Type != Drill || r.Phase != "restore" || r.Validate() != nil {
		return zero, false, errors.New("isolation_invalid")
	}
	v, err := s.PrepareStageVolume(ctx, l, t, r)
	if err != nil {
		return v, false, err
	}
	v, ready, err := d.Ensure(ctx, s, l, t, v)
	if err != nil || !ready {
		return v, false, err
	}
	err = s.withStageDirectoryFence(ctx, l, t, v, func() error {
		root, e := openStageRoot(d.MountDirectory)
		if e != nil {
			return e
		}
		defer root.Close()
		expected, e := stageOwner(s.InstallationID, v)
		if e != nil {
			return e
		}
		if e = verifyStageOwner(root, v, expected); e != nil {
			return e
		}
		name := filepath.Join(v.ID, "data")
		if e = root.Mkdir(name, 0700); e != nil && !errors.Is(e, os.ErrExist) {
			return e
		}
		stat, e := root.Lstat(name)
		if e != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
			return errors.New("isolation_invalid")
		}
		f, e := root.Open(name)
		if e != nil {
			return e
		}
		defer f.Close()
		entries, e := f.ReadDir(1)
		// ReadDir returns EOF for an empty directory; only its absence of
		// entries is acceptable. Never erase a prior partial restore on retry.
		if len(entries) != 0 {
			return errors.New("state_conflict")
		}
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		inner, e := root.OpenRoot(v.ID)
		if e != nil {
			return e
		}
		defer inner.Close()
		return syncDirectory(inner)
	})
	return v, err == nil, err
}
