package systembackuptrial

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

const trialMaterialOwner = "clawmanager.io/trial-material-namespace"
const trialCredentialBinding = "clawmanager.io/trial-credential-binding"
const trialKeyDigest = "clawmanager.io/trial-key-sha256"

type trialPreparationResources interface {
	coreclient.SecretsGetter
	coreclient.PersistentVolumeClaimsGetter
}

// PrepareEnvironment runs in the registered platform Job using a separate
// service account and a SQL owner credential mounted only into that Job.
func (s *Service) PrepareEnvironment(ctx context.Context) error {
	p, err := s.environmentProfile()
	if err != nil {
		return errors.New("trial preparation profile invalid")
	}
	local, err := readSetting("/var/run/secrets/kubernetes.io/serviceaccount/namespace", 128)
	if err != nil || strings.TrimSpace(string(local)) != p.local().Namespace {
		return errors.New("trial preparation namespace mismatch")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("trial preparation Kubernetes identity unavailable")
	}
	resources, err := coreclient.NewForConfig(config)
	if err != nil {
		return errors.New("trial preparation Kubernetes client unavailable")
	}
	return s.prepareEnvironment(ctx, p, resources)
}

func (s *Service) prepareEnvironment(ctx context.Context, p trialEnvironment, resources trialPreparationResources) error {
	local := p.local()
	secrets := resources.Secrets(local.Namespace)
	if local.KeySecret == local.SourceReadSecret || local.KeySecret == local.RestoreReadSecret ||
		local.KeySecret == local.RestoreWriteSecret || local.KeySecret == local.RuntimeSecret {
		return errors.New("trial preparation requires a dedicated key Secret")
	}
	claims := resources.PersistentVolumeClaims(local.Namespace)
	if !boundTrialClaim(ctx, claims, local.ArtifactClaim, local.ArtifactStorageClass) ||
		(local.WorkspaceClaim != "" && (local.WorkspaceClaim == local.ArtifactClaim || !boundTrialClaim(ctx, claims, local.WorkspaceClaim, local.WorkspaceStorageClass))) {
		return errors.New("trial preparation PVC binding mismatch")
	}
	key, err := ensureTrialKey(ctx, p, resources)
	if err != nil {
		return err
	}
	defer clear(key)
	if err := s.prepareStorage(ctx, p, key); err != nil {
		return err
	}
	return s.prepareMySQL(ctx, p, secrets)
}

func boundTrialClaim(ctx context.Context, claims coreclient.PersistentVolumeClaimInterface, name, storageClass string) bool {
	claim, err := claims.Get(ctx, name, meta.GetOptions{})
	return err == nil && claim.Status.Phase == core.ClaimBound && claim.DeletionTimestamp == nil && claim.Spec.VolumeName != "" &&
		(storageClass == "" || (claim.Spec.StorageClassName != nil && *claim.Spec.StorageClassName == storageClass))
}

func ownedTrialSecret(ctx context.Context, secrets coreclient.SecretInterface, namespace, name, field string) (*core.Secret, error) {
	secret, err := secrets.Get(ctx, name, meta.GetOptions{})
	if err != nil || secret.Namespace != namespace || secret.Name != name || secret.UID == "" || secret.DeletionTimestamp != nil ||
		secret.Type != core.SecretTypeOpaque || secret.Annotations[trialMaterialOwner] != namespace ||
		(len(secret.Data) > 0 && (len(secret.Data) != 1 || len(secret.Data[field]) == 0)) {
		return nil, errors.New("trial preparation Secret ownership or shape mismatch")
	}
	return secret, nil
}

func storeTrialSecret(ctx context.Context, secrets coreclient.SecretInterface, secret *core.Secret, field string, data []byte) error {
	if secret.Immutable != nil && *secret.Immutable {
		return errors.New("trial preparation Secret is immutable")
	}
	secret.Data = map[string][]byte{field: data}
	updated, err := secrets.Update(ctx, secret, meta.UpdateOptions{})
	if err != nil || updated.UID != secret.UID || !bytes.Equal(updated.Data[field], data) {
		return errors.New("trial preparation Secret could not be saved")
	}
	return nil
}

func ensureTrialKey(ctx context.Context, p trialEnvironment, resources trialPreparationResources) ([]byte, error) {
	local := p.local()
	secrets := resources.Secrets(local.Namespace)
	current, err := ownedTrialSecret(ctx, secrets, local.Namespace, local.KeySecret, "key")
	if err != nil {
		return nil, err
	}
	sealed := current.Data["key"]
	if !validTrialKeyMarker(current) {
		return nil, errors.New("trial preparation stored key no longer matches its binding")
	}
	if p.Side == "target" && p.Source.Cluster == p.Target.Cluster && current.Annotations[trialKeyDigest] == "" {
		source, err := ownedTrialSecret(ctx, resources.Secrets(p.Source.Namespace), p.Source.Namespace, p.Source.KeySecret, "key")
		if err != nil || len(source.Data["key"]) != 64 || !validTrialKeyMarker(source) || (len(sealed) > 0 && !bytes.Equal(sealed, source.Data["key"])) {
			return nil, errors.New("trial preparation source and target key binding mismatch")
		}
		sealed = source.Data["key"]
	} else if p.Side == "source" && len(sealed) == 0 {
		var generated [32]byte
		if _, err := rand.Read(generated[:]); err != nil {
			return nil, errors.New("trial preparation key generation failed")
		}
		sealed = make([]byte, 64)
		hex.Encode(sealed, generated[:])
		clear(generated[:])
	}
	key, err := hex.DecodeString(string(sealed))
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, errors.New("trial preparation key unavailable")
	}
	if current.Annotations[trialKeyDigest] == "" {
		current.Annotations[trialKeyDigest] = trialKeyHash(sealed)
		if err := storeTrialSecret(ctx, secrets, current, "key", sealed); err != nil {
			clear(key)
			return nil, err
		}
	}
	return key, nil
}

func validTrialKeyMarker(secret *core.Secret) bool {
	digest := secret.Annotations[trialKeyDigest]
	return digest == "" || (len(secret.Data["key"]) == 64 && trialKeyHash(secret.Data["key"]) == digest)
}

func trialKeyHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) prepareStorage(ctx context.Context, p trialEnvironment, key []byte) error {
	if p.Side == "source" {
		volume, err := s.storage()
		if err != nil {
			return errors.New("trial preparation archive mounts unavailable")
		}
		defer volume.close()
		if err := volume.probe(ctx, key); err != nil {
			return errors.New("trial preparation archive readback failed")
		}
	} else {
		reader, err := s.readerRoot()
		if err != nil {
			return errors.New("trial preparation import mount unavailable")
		}
		defer reader.Close()
		if readOnly, err := restoreMountReadOnly(reader); err != nil || !readOnly {
			return errors.New("trial preparation requires a read-only import mount")
		}
	}
	if p.local().WorkspaceClaim == "" {
		return nil
	}
	directory := s.value("PREPARE_WORKSPACE_DIR")
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || filepath.Dir(directory) == directory {
		return errors.New("trial preparation workspace mount unavailable")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errors.New("trial preparation workspace mount unavailable")
	}
	defer root.Close()
	if p.Side == "source" {
		if readOnly, err := restoreMountReadOnly(root); err != nil || !readOnly {
			return errors.New("trial preparation requires a read-only source workspace")
		}
	} else if err := checkWorkspacePublish(root); err != nil {
		return errors.New("trial preparation workspace publication failed")
	}
	return nil
}
