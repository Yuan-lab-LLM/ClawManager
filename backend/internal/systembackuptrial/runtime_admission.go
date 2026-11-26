package systembackuptrial

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	staging "clawreef/internal/systembackuptrial/envelope"
)

const runtimeAdmissionFile = "clawmanager-trial-runtime.admission"

var errRuntimeAdmission = errors.New("trial runtime admission missing or mismatched")

func runtimeAdmissionProof(key []byte, backupID, ciphertextSHA, database, serverUUID, namespace, storageClass, workspaceRoot string) []byte {
	binding, _ := json.Marshal([]string{
		"clawmanager.trial-runtime-admission.v1", backupID, ciphertextSHA, database, serverUUID,
		namespace, storageClass, workspaceRoot,
	})
	mac := hmac.New(sha256.New, key)
	mac.Write(binding)
	return mac.Sum(nil)
}

func readRuntimeAdmissionFile(root *os.Root, expected []byte) (bool, error) {
	info, err := root.Lstat(runtimeAdmissionFile)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() != sha256.Size {
		return false, errRuntimeAdmission
	}
	file, err := root.Open(runtimeAdmissionFile)
	if err != nil {
		return false, errRuntimeAdmission
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return false, errRuntimeAdmission
	}
	proof, err := io.ReadAll(io.LimitReader(file, sha256.Size+1))
	if err != nil || !hmac.Equal(proof, expected) {
		return false, errRuntimeAdmission
	}
	return true, nil
}

func admitTrialRuntime(ctx context.Context, reader, writer *sql.DB, root *os.Root, proof []byte, verifyInitialRestore func() error) error {
	const read = "SELECT proof FROM trial_runtime_admission WHERE id = 1"
	var recorded []byte
	err := reader.QueryRowContext(ctx, read).Scan(&recorded)
	switch {
	case err == nil:
		present, err := readRuntimeAdmissionFile(root, proof)
		if err != nil || !present || !hmac.Equal(recorded, proof) {
			return errRuntimeAdmission
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return errRuntimeAdmission
	}

	present, err := readRuntimeAdmissionFile(root, proof)
	if err != nil {
		return err
	}
	if err := verifyInitialRestore(); err != nil {
		return err
	}
	if !present {
		// Publish the volume proof first. If startup stops here, the next attempt
		// repeats the initial restore checks before completing the database proof.
		candidate := "trial-admission-" + newID()
		if _, err := staging.WriteUnpublishedFile(ctx, root, candidate, func(w io.Writer) error {
			_, err := w.Write(proof)
			return err
		}); err != nil {
			return errRuntimeAdmission
		}
		defer root.Remove(candidate)
		if err := root.Link(candidate, runtimeAdmissionFile); err != nil && !errors.Is(err, fs.ErrExist) {
			return errRuntimeAdmission
		}
		if valid, err := readRuntimeAdmissionFile(root, proof); err != nil || !valid {
			return errRuntimeAdmission
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return errRuntimeAdmission
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		return errRuntimeAdmission
	}

	// Reuse the row only if it independently reads back as the same proof.
	// This covers concurrent first starts and a lost INSERT response.
	_, _ = writer.ExecContext(ctx, "INSERT INTO trial_runtime_admission (id, proof) VALUES (1, ?)", proof)
	if err := reader.QueryRowContext(ctx, read).Scan(&recorded); err != nil || !hmac.Equal(recorded, proof) {
		return errRuntimeAdmission
	}
	return nil
}
