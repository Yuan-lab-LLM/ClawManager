package runtimeinventory

import (
	"context"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
)

type objectSource struct {
	list  func(context.Context) ([]runtime.Object, string, error)
	watch func(context.Context, metav1.ListOptions) (watch.Interface, error)
	proof func(context.Context) ([]metav1.PartialObjectMetadata, string, error)
}

// Start uses separate resumable watches, never per-capability API calls. Watch
// requests expire every two seconds; even a silent broken transport loses its
// authority after three seconds. Normal renewal resumes the last version rather
// than repeatedly listing the full instance set.
func Start(ctx context.Context, kube kubernetes.Interface, dynamicClient dynamic.Interface, metadataClient metadata.Interface, namespace string, podCIDRs, controls []string) (*Inventory, error) {
	i, err := newInventory(podCIDRs, controls)
	if err != nil {
		return nil, err
	}
	if kube == nil || dynamicClient == nil || metadataClient == nil || namespace == "" {
		return nil, errUnavailable
	}
	cms := kube.CoreV1().ConfigMaps(namespace)
	pods := kube.CoreV1().Pods(namespace)
	nodes := kube.CoreV1().Nodes()
	services := dynamicClient.Resource(schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "servicecidrs"})
	type stream struct {
		store  *objectStore
		source objectSource
	}
	streams := []stream{
		{&i.assignments, objectSource{
			list: func(ctx context.Context) ([]runtime.Object, string, error) {
				list, err := cms.List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
				if err != nil {
					return nil, "", err
				}
				items := make([]runtime.Object, len(list.Items))
				for n := range list.Items {
					items[n] = &list.Items[n]
				}
				return items, list.ResourceVersion, nil
			},
			watch: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = "clawmanager.io/purpose=lite-instance"
				return cms.Watch(ctx, options)
			},
		}},
		{&i.pods, objectSource{
			list: func(ctx context.Context) ([]runtime.Object, string, error) {
				list, err := pods.List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/runtime-protocol=container-slots-v1"})
				if err != nil {
					return nil, "", err
				}
				items := make([]runtime.Object, len(list.Items))
				for n := range list.Items {
					items[n] = &list.Items[n]
				}
				return items, list.ResourceVersion, nil
			},
			watch: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = "clawmanager.io/runtime-protocol=container-slots-v1"
				return pods.Watch(ctx, options)
			},
		}},
		{&i.nodes, objectSource{
			list: func(ctx context.Context) ([]runtime.Object, string, error) {
				list, err := nodes.List(ctx, metav1.ListOptions{})
				if err != nil {
					return nil, "", err
				}
				items := make([]runtime.Object, len(list.Items))
				for n := range list.Items {
					items[n] = &list.Items[n]
				}
				return items, list.ResourceVersion, nil
			},
			watch: nodes.Watch,
		}},
		{&i.services, objectSource{
			list: func(ctx context.Context) ([]runtime.Object, string, error) {
				list, err := services.List(ctx, metav1.ListOptions{})
				if err != nil {
					return nil, "", err
				}
				items := make([]runtime.Object, len(list.Items))
				for n := range list.Items {
					items[n] = &list.Items[n]
				}
				return items, list.GetResourceVersion(), nil
			},
			watch: services.Watch,
		}},
	}
	proofs := []struct {
		resource            schema.GroupVersionResource
		namespace, selector string
	}{
		{schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, namespace, "clawmanager.io/purpose=lite-instance"},
		{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, namespace, "clawmanager.io/runtime-protocol=container-slots-v1"},
		{schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, "", ""},
		{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "servicecidrs"}, "", ""},
	}
	for n, proof := range proofs {
		streams[n].source.proof = metadataProof(metadataClient.Resource(proof.resource).Namespace(proof.namespace), proof.selector)
	}
	ctx, i.cancel = context.WithCancel(ctx)
	i.done = make(chan struct{})
	var group sync.WaitGroup
	for _, stream := range streams {
		group.Add(1)
		go func() { defer group.Done(); runSource(ctx, stream.store, stream.source) }()
	}
	go func() { group.Wait(); close(i.done) }()
	return i, nil
}

func (i *Inventory) Close() {
	if i.cancel != nil {
		i.cancel()
		<-i.done
	}
}

func (i *Inventory) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready := true
		for _, store := range []*objectStore{&i.assignments, &i.pods, &i.nodes, &i.services} {
			if !store.isHealthy() {
				ready = false
				break
			}
		}
		if ready {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runSource(ctx context.Context, store *objectStore, source objectSource) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); runWatch(ctx, store, source) }()
	runProof(ctx, store, source.proof)
	cancel()
	<-done
	store.unhealthy()
}

func runWatch(ctx context.Context, store *objectStore, source objectSource) {
	defer store.unhealthy()
	defer func() { store.mu.Lock(); store.connected = false; store.mu.Unlock() }()
	version := ""
	listed := false
	for ctx.Err() == nil {
		if !listed {
			listCtx, cancel := context.WithTimeout(ctx, maxObservationAge)
			items, rv, err := source.list(listCtx)
			cancel()
			if err != nil || rv == "" {
				store.unhealthy()
				if !retry(ctx) {
					return
				}
				continue
			}
			store.replace(items)
			version, listed = rv, true
		}
		watchCtx, cancel := context.WithTimeout(ctx, maxObservationAge)
		timeout := int64(2)
		stream, err := source.watch(watchCtx, metav1.ListOptions{ResourceVersion: version, TimeoutSeconds: &timeout, AllowWatchBookmarks: true})
		if err != nil || stream == nil {
			cancel()
			store.unhealthy()
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				listed = false
			}
			if !retry(ctx) {
				return
			}
			continue
		}
		store.mu.Lock()
		store.connected = true
		store.mu.Unlock()
		started := time.Now()
		failed := false
	read:
		for {
			select {
			case <-watchCtx.Done():
				failed = true
				break read
			case event, open := <-stream.ResultChan():
				if !open {
					// A normal server timeout takes two seconds. Early EOF is a
					// disconnected stream, not a fresh observation of its contents.
					failed = time.Since(started) < 1500*time.Millisecond
					break read
				}
				if event.Type == watch.Error {
					err := apierrors.FromObject(event.Object)
					if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
						listed = false
					}
					failed = true
					break read
				}
				metadata, err := meta.Accessor(event.Object)
				if err != nil || metadata.GetResourceVersion() == "" {
					failed = true
					break read
				}
				store.mu.Lock()
				switch event.Type {
				case watch.Added, watch.Modified:
					store.objects[metadata.GetName()] = event.Object.DeepCopyObject()
					delete(store.deleted, metadata.GetName())
				case watch.Deleted:
					delete(store.objects, metadata.GetName())
					store.deleted[metadata.GetName()] = metadata.GetResourceVersion()
				case watch.Bookmark:
				default:
					failed = true
				}
				if !failed {
					version = metadata.GetResourceVersion()
				}
				store.mu.Unlock()
				if failed {
					break read
				}
			}
		}
		stream.Stop()
		cancel()
		store.mu.Lock()
		store.connected = false
		store.mu.Unlock()
		if failed {
			store.unhealthy()
			if !retry(ctx) {
				return
			}
		}
	}
}

func retry(ctx context.Context) bool {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
