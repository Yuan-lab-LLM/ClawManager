// Pinned compatibility helper for the Kubernetes v1.35.0 Deployment controller.
// Algorithm: kubernetes/pkg/controller/controller_utils.go ComputeHash and
// kubernetes/pkg/util/hash/hash.go DeepHashObject, with nil collisionCount.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/dump"
	"k8s.io/apimachinery/pkg/util/rand"
	"os"
)

const maxTemplateBytes = 1 << 20

func hashTemplate(raw []byte) (string, error) {
	if len(raw) > maxTemplateBytes {
		return "", fmt.Errorf("template exceeds bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var template corev1.PodTemplateSpec
	if err := decoder.Decode(&template); err != nil {
		return "", fmt.Errorf("invalid template")
	}
	if len(template.Spec.Containers) == 0 {
		return "", fmt.Errorf("template containers missing")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("trailing template data")
	}
	delete(template.Labels, "pod-template-hash")
	hasher := fnv.New32a()
	fmt.Fprint(hasher, dump.ForHash(template))
	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32())), nil
}
func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, maxTemplateBytes+1))
	if err != nil {
		os.Exit(1)
	}
	hash, err := hashTemplate(raw)
	if err != nil {
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(struct {
		APIVersion string `json:"api_version"`
		Hash       string `json:"hash"`
	}{"v1.35.0", hash}); err != nil {
		os.Exit(1)
	}
}
