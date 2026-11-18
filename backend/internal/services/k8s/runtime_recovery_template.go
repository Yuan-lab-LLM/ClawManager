package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const runtimeTemplateHashBinary = "/usr/local/bin/cm-runtime-template-hash-v1.35"
const runtimeTemplateMaxBytes = 1 << 20

// Controller-owned metadata is trusted against malicious administrators. The
// original controller hash detects accidental edits, including storage changes.
// Use a version-pinned helper because typed API additions change DeepHashObject.
func verifyRetainedRuntimeTemplate(ctx context.Context, client kubernetes.Interface, rs *appsv1.ReplicaSet, dep *appsv1.Deployment) error {
	if dep.Status.CollisionCount != nil {
		return fmt.Errorf("original Deployment collision count cannot be reconstructed")
	}
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return err
	}
	if version.GitVersion != "v1.35.0" {
		return fmt.Errorf("source template hash requires verified controller v1.35.0")
	}
	raw, err := client.AppsV1().RESTClient().Get().Namespace(rs.Namespace).Resource("replicasets").Name(rs.Name).DoRaw(ctx)
	if err != nil {
		return err
	}
	template, err := matchingRawRuntimeTemplate(raw, rs)
	if err != nil {
		return err
	}
	info, err := os.Lstat(runtimeTemplateHashBinary)
	if err != nil {
		return fmt.Errorf("pinned template hash helper unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("pinned template hash helper is not root-owned and protected")
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	hash, err := runRuntimeTemplateHashHelper(bounded, runtimeTemplateHashBinary, template)
	if err != nil {
		return err
	}
	return verifyRuntimeTemplateHash(rs, dep, hash)
}

func matchingRawRuntimeTemplate(raw []byte, rs *appsv1.ReplicaSet) ([]byte, error) {
	if len(raw) > runtimeTemplateMaxBytes {
		return nil, fmt.Errorf("source ReplicaSet exceeds proof bound")
	}
	var object struct {
		Metadata metav1.ObjectMeta `json:"metadata"`
		Spec     struct {
			Template json.RawMessage `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("invalid raw source ReplicaSet")
	}
	if object.Metadata.UID == "" || object.Metadata.ResourceVersion == "" || object.Metadata.UID != rs.UID || object.Metadata.ResourceVersion != rs.ResourceVersion || object.Metadata.Name != rs.Name || object.Metadata.Namespace != rs.Namespace || len(object.Spec.Template) == 0 {
		return nil, fmt.Errorf("source ReplicaSet changed between metadata and raw-template reads")
	}
	return object.Spec.Template, nil
}

type runtimeHashOutput struct{ data bytes.Buffer }

func (b *runtimeHashOutput) Write(p []byte) (int, error) {
	if b.data.Len()+len(p) > 256 {
		return 0, fmt.Errorf("template helper output exceeds bound")
	}
	return b.data.Write(p)
}
func runRuntimeTemplateHashHelper(ctx context.Context, binary string, raw []byte) (string, error) {
	if len(raw) > runtimeTemplateMaxBytes {
		return "", fmt.Errorf("template input exceeds bound")
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = []string{}
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	var output runtimeHashOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("template hash helper failed")
	}
	var result struct {
		APIVersion string `json:"api_version"`
		Hash       string `json:"hash"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output.data.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("template hash helper returned invalid output")
	}
	if err := decoder.Decode(new(any)); err != io.EOF || result.APIVersion != "v1.35.0" || result.Hash == "" || len(result.Hash) > 32 {
		return "", fmt.Errorf("template hash helper identity or output is invalid")
	}
	return result.Hash, nil
}
func verifyRuntimeTemplateHash(rs *appsv1.ReplicaSet, dep *appsv1.Deployment, hash string) error {
	if dep.Status.CollisionCount != nil {
		return fmt.Errorf("original Deployment collision count cannot be reconstructed")
	}
	if hash != rs.Labels["pod-template-hash"] || hash != rs.Spec.Template.Labels["pod-template-hash"] {
		return fmt.Errorf("retained source template hash does not match original controller hash")
	}
	return nil
}
