package systembackuptrial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	staging "clawreef/internal/systembackuptrial/envelope"
)

func deliveryFixture(t *testing.T, profile trialEnvironment, id, message string) ([]byte, staging.Receipt, []byte) {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	receipt, err := staging.SealFile(t.Context(), root, binding(id).Path, taskKey(make([]byte, 32), id), binding(id), func(w io.Writer) error {
		_, err := io.WriteString(w, message)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := staging.OpenFile(t.Context(), root, receipt)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	raw, err := json.Marshal(artifact{Receipt: receipt})
	if err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := writeDelivery(&bundle, newDeliveryHandoff(profile, id, receipt.SHA256), receipt, raw, file); err != nil {
		t.Fatal(err)
	}
	return bundle.Bytes(), receipt, raw
}

func TestTrialDeliveryPublishesOriginalPairWithRetry(t *testing.T) {
	id := strings.Repeat("1", 32)
	profile := trialEnvironment{
		Side: "target", Source: trialPlacement{Cluster: "a", Namespace: "source-ns", ArtifactClaim: "source-archive"},
		Target: trialPlacement{Cluster: "b", Namespace: "cm-drill-target", ArtifactClaim: "target-archive"},
	}
	bundle, receipt, raw := deliveryFixture(t, profile, id, "first sealed fixture")
	bad, _, _ := deliveryFixture(t, profile, id, "different sealed fixture")
	target, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	receiver := artifactImporter(target, profile)
	send := func(payload []byte, contentType string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/delivery", bytes.NewReader(payload))
		req.Header.Set("Content-Type", contentType)
		res := httptest.NewRecorder()
		receiver.ServeHTTP(res, req)
		return res.Code
	}
	if status := send(bundle, "application/zip"); status != http.StatusBadRequest {
		t.Fatalf("unexpected media type status %d", status)
	}
	for index := 0; index < 2; index++ {
		if status := send(bundle, transferMediaType); status != http.StatusOK {
			t.Fatalf("delivery %d failed with HTTP %d", index, status)
		}
	}
	if status := send(bad, transferMediaType); status != http.StatusConflict {
		t.Fatalf("conflicting delivery returned HTTP %d", status)
	}
	got, err := target.ReadFile(id + ".receipt")
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("receiver did not preserve the receipt bytes")
	}
	var plaintext bytes.Buffer
	if err := (storage{reader: target}).open(t.Context(), receipt, taskKey(make([]byte, 32), id), binding(id), &plaintext, 1024); err != nil ||
		plaintext.String() != "first sealed fixture" {
		t.Fatalf("the independent restore reader could not open the original ciphertext: %v", err)
	}
}

func TestTrialDeliveryRejectsInvalidPackagesBeforePublishing(t *testing.T) {
	id := strings.Repeat("2", 32)
	profile := trialEnvironment{
		Side: "target", Source: trialPlacement{Cluster: "a", Namespace: "source-ns", ArtifactClaim: "source-archive"},
		Target: trialPlacement{Cluster: "b", Namespace: "cm-drill-target", ArtifactClaim: "target-archive"},
	}
	original, _, _ := deliveryFixture(t, profile, id, "sealed fixture")
	otherPlacement := profile
	otherPlacement.Target.ArtifactClaim = "other-archive"
	wrongPlacement, _, _ := deliveryFixture(t, otherPlacement, id, "sealed fixture")
	corrupt := bytes.Clone(original)
	// The third member follows two small metadata members.
	corrupt[5*512] ^= 1
	for name, payload := range map[string][]byte{
		"wrong target": wrongPlacement, "corrupted ciphertext": corrupt,
		"missing terminator": original[:len(original)-1024], "trailing bytes": append(bytes.Clone(original), 1),
	} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir()
			target, err := os.OpenRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			if _, err := receiveDelivery(context.Background(), target, profile, bytes.NewReader(payload)); !errors.Is(err, errTransfer) {
				t.Fatalf("bad delivery returned %v", err)
			}
			if files, err := os.ReadDir(path); err != nil || len(files) != 0 {
				t.Fatalf("rejected delivery left files: %v, %v", files, err)
			}
		})
	}
}
