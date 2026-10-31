package systembackup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type StageDirectoryOwner struct {
	Schema      string `json:"schema"`
	Origin      string `json:"origin"`
	TaskID      string `json:"task_id"`
	ResourceID  string `json:"resource_id"`
	RequestHash string `json:"request_hash"`
}

func stageOwner(origin string, v StageVolume) ([]byte, error) {
	if !refPattern.MatchString(origin) || !publicIDPattern.MatchString(v.TaskID) || !stageResourceIDPattern.MatchString(v.ID) || !validHash(v.Hash) {
		return nil, errors.New("validation_failed")
	}
	return canonicalJSON(StageDirectoryOwner{Schema: "system-backup-stage-owner.v1", Origin: origin, TaskID: v.TaskID, ResourceID: v.ID, RequestHash: v.Hash})
}
func openStageRoot(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("cleanup_unsafe_target")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cleanup_unsafe_target")
	}
	return os.OpenRoot(directory)
}
func verifyStageOwner(root *os.Root, v StageVolume, expected []byte) error {
	info, err := root.Lstat(v.ID)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup_unsafe_target")
	}
	name := filepath.Join(v.ID, ".backup-stage-owner")
	info, err = root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return errors.New("cleanup_unsafe_target")
	}
	f, err := root.Open(name)
	if err != nil {
		return errors.New("cleanup_unsafe_target")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(len(expected))+1))
	if err != nil || !bytes.Equal(raw, expected) {
		return errors.New("cleanup_unsafe_target")
	}
	return nil
}

func PrepareStageDirectory(directory, origin string, v StageVolume) error {
	expected, err := stageOwner(origin, v)
	if err != nil {
		return err
	}
	root, err := openStageRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Mkdir(v.ID, 0700); errors.Is(err, os.ErrExist) {
		return verifyStageOwner(root, v, expected)
	} else if err != nil {
		return err
	}
	f, err := root.OpenFile(filepath.Join(v.ID, ".backup-stage-owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(expected)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = stageDirectoryPermissions(root, v.ID); err != nil {
		return err
	}
	inner, err := root.OpenRoot(v.ID)
	if err != nil {
		return err
	}
	err = syncDirectory(inner)
	inner.Close()
	if err != nil {
		return err
	}
	return syncDirectory(root)
}

// Call only after all original Pods and volume objects have been confirmed
// absent. A missing owner marker or foreign directory is never auto-deleted.
func RemoveStageDirectory(directory, origin string, v StageVolume) error {
	expected, err := stageOwner(origin, v)
	if err != nil {
		return err
	}
	root, err := openStageRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err = root.Lstat(v.ID); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err = verifyStageOwner(root, v, expected); err != nil {
		return err
	}
	if err = root.RemoveAll(v.ID); err != nil {
		return err
	}
	return syncDirectory(root)
}
