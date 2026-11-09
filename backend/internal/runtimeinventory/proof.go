package runtimeinventory

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/metadata"
)

// A watch being connected says nothing about whether its buffered events have
// caught up. Consistent metadata-only lists provide an independent data barrier
// without downloading every assignment's config on each freshness check.
func metadataProof(client metadata.ResourceInterface, selector string) func(context.Context) ([]metav1.PartialObjectMetadata, string, error) {
	return func(ctx context.Context) ([]metav1.PartialObjectMetadata, string, error) {
		options := metav1.ListOptions{LabelSelector: selector, Limit: 1000}
		var objects []metav1.PartialObjectMetadata
		version := ""
		for {
			// Empty resourceVersion requests the most recent consistent snapshot.
			// Subsequent pages retain that same snapshot through Continue.
			list, err := client.List(ctx, options)
			if err != nil || list == nil {
				return nil, "", errUnavailable
			}
			if !decimalVersion(list.ResourceVersion) || (version != "" && version != list.ResourceVersion) {
				return nil, "", errUnavailable
			}
			version = list.ResourceVersion
			objects = append(objects, list.Items...)
			if list.Continue == "" {
				return objects, version, ctx.Err()
			}
			if list.Continue == options.Continue {
				return nil, "", errUnavailable
			}
			options.Continue = list.Continue
		}
	}
}

func runProof(ctx context.Context, store *objectStore, proof func(context.Context) ([]metav1.PartialObjectMetadata, string, error)) {
	defer store.unhealthy()
	for ctx.Err() == nil {
		started := time.Now()
		proofCtx, cancel := context.WithTimeout(ctx, time.Second)
		var objects []metav1.PartialObjectMetadata
		var version string
		var err error
		if proof == nil {
			err = errUnavailable
		} else {
			objects, version, err = proof(proofCtx)
		}
		valid := false
		if err == nil {
			ticker := time.NewTicker(10 * time.Millisecond)
		wait:
			for proofCtx.Err() == nil {
				store.mu.Lock()
				if store.connected && matchesProof(store, objects, version) {
					store.available, store.observed = true, started
					// Once a consistent snapshot omits a deleted object, its old
					// tombstone is no longer needed for future proof comparisons.
					for name, rv := range store.deleted {
						if versionAtLeast(version, rv) {
							delete(store.deleted, name)
						}
					}
					valid = true
				}
				store.mu.Unlock()
				if valid {
					break
				}
				select {
				case <-proofCtx.Done():
					break wait
				case <-ticker.C:
				}
			}
			ticker.Stop()
		}
		cancel()
		if !valid {
			store.unhealthy()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Kubernetes 1.35 guarantees decimal ordering within a resource type. Compare
// arbitrary precision strings, never resource versions of different types.
// https://kubernetes.io/docs/reference/using-api/api-concepts/#resource-versions
func decimalVersion(value string) bool {
	if len(value) == 0 || value[0] < '1' || value[0] > '9' {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func versionAtLeast(a, b string) bool {
	if !decimalVersion(a) || !decimalVersion(b) {
		return false
	}
	return len(a) > len(b) || (len(a) == len(b) && a >= b)
}

// Caller holds store.mu. Newer cached changes are safe only with their own
// numeric version proof. Missing objects need a post-snapshot delete receipt.
func matchesProof(store *objectStore, objects []metav1.PartialObjectMetadata, version string) bool {
	if !decimalVersion(version) {
		return false
	}
	seen := make(map[string]struct{}, len(objects))
	for _, expected := range objects {
		if expected.Name == "" || expected.UID == "" || !versionAtLeast(version, expected.ResourceVersion) {
			return false
		}
		if _, duplicate := seen[expected.Name]; duplicate {
			return false
		}
		seen[expected.Name] = struct{}{}
		object := store.objects[expected.Name]
		if object == nil {
			rv := store.deleted[expected.Name]
			if rv == version || !versionAtLeast(rv, version) {
				return false
			}
			continue
		}
		actual, err := meta.Accessor(object)
		if err != nil || !versionAtLeast(actual.GetResourceVersion(), expected.ResourceVersion) {
			return false
		}
		if actual.GetUID() != expected.UID && (actual.GetResourceVersion() == version || !versionAtLeast(actual.GetResourceVersion(), version)) {
			return false
		}
	}
	for name, object := range store.objects {
		if _, exists := seen[name]; exists {
			continue
		}
		actual, err := meta.Accessor(object)
		if err != nil || actual.GetResourceVersion() == version || !versionAtLeast(actual.GetResourceVersion(), version) {
			return false
		}
	}
	return true
}
