package systembackuptrial

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"

	staging "clawreef/internal/systembackuptrial/envelope"

	"github.com/gin-gonic/gin"
)

const (
	transferMediaType  = "application/x-tar"
	handoffKind        = "clawmanager.trial-restore-handoff.v1"
	maxTrialCiphertext = maxTrialPlaintext + 1<<20
	maxTransferSize    = maxTrialCiphertext + 32<<10
)

var errTransfer = errors.New("trial artifact package is invalid")
var errTransferConflict = errors.New("trial artifact already exists with different bytes")

type transferPlacement struct {
	Cluster       string `json:"cluster"`
	Namespace     string `json:"namespace"`
	ArtifactClaim string `json:"artifact_claim"`
}

func deliveryPlacement(p trialPlacement) transferPlacement {
	return transferPlacement{Cluster: p.Cluster, Namespace: p.Namespace, ArtifactClaim: p.ArtifactClaim}
}

type deliveryHandoff struct {
	Kind             string            `json:"kind"`
	BackupID         string            `json:"backup_id"`
	CiphertextSHA256 string            `json:"ciphertext_sha256"`
	Source           transferPlacement `json:"source"`
	Target           transferPlacement `json:"target"`
}

func newDeliveryHandoff(p trialEnvironment, id, digest string) deliveryHandoff {
	return deliveryHandoff{
		Kind: handoffKind, BackupID: id, CiphertextSHA256: digest,
		Source: deliveryPlacement(p.Source), Target: deliveryPlacement(p.Target),
	}
}

func (h deliveryHandoff) valid(p trialEnvironment) bool {
	return taskID.MatchString(h.BackupID) && ciphertextDigest.MatchString(h.CiphertextSHA256) &&
		h == newDeliveryHandoff(p, h.BackupID, h.CiphertextSHA256)
}

func (h deliveryHandoff) matchesReceipt(r staging.Receipt) bool {
	return r.Name == binding(h.BackupID).Path && r.SHA256 == h.CiphertextSHA256 && r.Size > 0 && r.Size <= maxTrialCiphertext
}

func (s *Service) handleExport(c *gin.Context) {
	id := c.Param("id")
	digests, _ := c.GetQueryArray("expected_ciphertext_sha256")
	if !taskID.MatchString(id) || len(digests) != 1 || !ciphertextDigest.MatchString(digests[0]) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_trial_artifact_identity"})
		return
	}
	ctx := c.Request.Context()
	environment := s.environment(ctx, false)
	if environment.Profile == nil || environment.missing("source") != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "trial_environment_source"})
		return
	}
	root, err := s.readerRoot()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "restore_artifact_configuration"})
		return
	}
	defer root.Close()
	store := storage{reader: root}
	meta, raw, err := store.metadataBytes(id)
	handoff := newDeliveryHandoff(*environment.Profile, id, digests[0])
	if err != nil || !decodeDeliveryJSON(raw, &meta) || !handoff.matchesReceipt(meta.Receipt) {
		c.JSON(http.StatusConflict, gin.H{"code": "restore_artifact_changed"})
		return
	}
	ciphertext, err := staging.OpenFile(ctx, root, meta.Receipt)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "restore_artifact_readback"})
		return
	}
	defer ciphertext.Close()
	c.Header("Content-Disposition", `attachment; filename="backup-trial-`+id+`.tar"`)
	c.Header("Content-Type", transferMediaType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	_ = writeDelivery(c.Writer, handoff, meta.Receipt, raw, ciphertext)
}

func writeDelivery(dst io.Writer, handoff deliveryHandoff, receipt staging.Receipt, raw []byte, ciphertext io.Reader) error {
	bundle := tar.NewWriter(dst)
	data, err := json.Marshal(handoff)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"handoff.json", data},
		{handoff.BackupID + ".receipt", raw},
	} {
		if err := bundle.WriteHeader(&tar.Header{Name: item.name, Mode: 0400, Size: int64(len(item.data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return err
		}
		if _, err := bundle.Write(item.data); err != nil {
			return err
		}
	}
	if err := bundle.WriteHeader(&tar.Header{Name: receipt.Name, Mode: 0400, Size: receipt.Size, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	if _, err := io.CopyN(bundle, ciphertext, receipt.Size); err != nil {
		return err
	}
	return bundle.Close()
}

func checkedDeliveryEntry(bundle *tar.Reader, name string, limit int64) (int64, error) {
	entry, err := bundle.Next()
	if err != nil || entry.Name != name || entry.Typeflag != tar.TypeReg || entry.Format != tar.FormatUSTAR ||
		entry.Size <= 0 || entry.Size > limit {
		return 0, errTransfer
	}
	return entry.Size, nil
}

func decodeDeliveryJSON(data []byte, result any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(result) == nil && decoder.Decode(new(any)) == io.EOF
}

type deliveryReader struct {
	io.Reader
	count int64
}

func (r *deliveryReader) Read(dst []byte) (int, error) {
	n, err := r.Reader.Read(dst)
	r.count += int64(n)
	return n, err
}

func tarEntrySize(size int64) int64 {
	return 512 + ((size + 511) / 512 * 512)
}

func receiveDelivery(ctx context.Context, root *os.Root, expected trialEnvironment, input io.Reader) (deliveryHandoff, error) {
	var handoff deliveryHandoff
	stream := &deliveryReader{Reader: input}
	bundle := tar.NewReader(stream)
	if _, err := checkedDeliveryEntry(bundle, "handoff.json", 16<<10); err != nil {
		return handoff, err
	}
	manifest, err := io.ReadAll(bundle)
	if err != nil || !decodeDeliveryJSON(manifest, &handoff) || !handoff.valid(expected) {
		return handoff, errTransfer
	}
	receiptName := handoff.BackupID + ".receipt"
	if _, err := checkedDeliveryEntry(bundle, receiptName, 2048); err != nil {
		return handoff, err
	}
	raw, err := io.ReadAll(bundle)
	var meta artifact
	if err != nil || !decodeDeliveryJSON(raw, &meta) || !handoff.matchesReceipt(meta.Receipt) {
		return handoff, errTransfer
	}
	size, err := checkedDeliveryEntry(bundle, meta.Receipt.Name, maxTrialCiphertext)
	if err != nil || size != meta.Receipt.Size {
		return handoff, errTransfer
	}
	stageDir := "delivery-" + newID()
	if err := root.Mkdir(stageDir, 0700); err != nil {
		return handoff, err
	}
	defer root.RemoveAll(stageDir)
	stage, err := root.OpenRoot(stageDir)
	if err != nil {
		return handoff, err
	}
	defer stage.Close()
	actual, err := staging.WriteUnpublishedFile(ctx, stage, meta.Receipt.Name, func(w io.Writer) error {
		_, err := io.Copy(w, bundle)
		return err
	})
	if err != nil || actual != meta.Receipt {
		return handoff, errTransfer
	}
	if _, err := bundle.Next(); err != io.EOF ||
		stream.count != tarEntrySize(int64(len(manifest)))+tarEntrySize(int64(len(raw)))+tarEntrySize(size)+1024 {
		return handoff, errTransfer
	}
	var extra [1]byte
	if _, err := io.ReadFull(input, extra[:]); err != io.EOF {
		return handoff, errTransfer
	}
	record, err := staging.WriteUnpublishedFile(ctx, stage, receiptName, func(w io.Writer) error {
		_, err := w.Write(raw)
		return err
	})
	if err != nil {
		return handoff, err
	}
	if _, err := root.Lstat(receiptName); err == nil {
		existing, err := staging.OpenFile(ctx, root, record)
		if err != nil {
			return handoff, errTransferConflict
		}
		existing.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return handoff, err
	}
	for _, name := range []string{actual.Name, record.Name} {
		if err := stage.Chmod(name, 0440); err != nil {
			return handoff, err
		}
	}
	if err := linkDelivery(ctx, root, stageDir, actual); err != nil {
		return handoff, err
	}
	// The restore reader opens the receipt first. Publish it only after the
	// original ciphertext is durably visible on the separate read-only mount.
	return handoff, linkDelivery(ctx, root, stageDir, record)
}

func linkDelivery(ctx context.Context, root *os.Root, stageDir string, receipt staging.Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Link(stageDir+"/"+receipt.Name, receipt.Name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		existing, err := staging.OpenFile(ctx, root, receipt)
		if err != nil {
			return errTransferConflict
		}
		existing.Close()
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
