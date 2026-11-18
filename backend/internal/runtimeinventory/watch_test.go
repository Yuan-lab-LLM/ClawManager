package runtimeinventory

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

func waitStore(t *testing.T, store *objectStore, valid bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := store.snapshot()
		if (err == nil) == valid {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("watch validity did not become %v", valid)
}

func TestWatchResumesVersionWithoutPerRequestListsAndDisconnectsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store objectStore
	var lists atomic.Int32
	first, second := watch.NewRaceFreeFake(), watch.NewRaceFreeFake()
	watchers := make(chan watch.Interface, 2)
	watchers <- first
	watchers <- second
	versions := make(chan string, 2)
	source := objectSource{
		list: func(context.Context) ([]runtime.Object, string, error) {
			lists.Add(1)
			return []runtime.Object{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: types.UID("same"), ResourceVersion: "10"}}}, "10", nil
		},
		proof: func(context.Context) ([]metav1.PartialObjectMetadata, string, error) {
			return []metav1.PartialObjectMetadata{{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: types.UID("same"), ResourceVersion: "10"}}}, "10", nil
		},
		watch: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			select {
			case w := <-watchers:
				if options.TimeoutSeconds == nil || *options.TimeoutSeconds != 2 || !options.AllowWatchBookmarks {
					t.Error("watch must be bounded and resumable")
				}
				versions <- options.ResourceVersion
				return w, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	done := make(chan struct{})
	go func() { defer close(done); runSource(ctx, &store, source) }()
	if version := <-versions; version != "10" {
		t.Fatalf("initial version %s", version)
	}
	waitStore(t, &store, true)
	first.Modify(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: types.UID("same"), ResourceVersion: "11"}})
	first.Stop()
	select {
	case version := <-versions:
		if version != "11" {
			t.Fatalf("resume version %s", version)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not resume")
	}
	if lists.Load() != 1 {
		t.Fatal("normal watch renewal listed all assignments again")
	}
	waitStore(t, &store, true)
	for n := 0; n < 100; n++ {
		if _, err := store.snapshot(); err != nil {
			t.Fatal(err)
		}
	}
	if lists.Load() != 1 {
		t.Fatal("snapshot performed Kubernetes I/O")
	}
	second.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 503})
	waitStore(t, &store, false)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch cleanup did not join")
	}
}

func TestUnexpectedWatchEOFInvalidatesUntilReconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store objectStore
	stream := watch.NewRaceFreeFake()
	first := true
	source := objectSource{
		proof: func(context.Context) ([]metav1.PartialObjectMetadata, string, error) { return nil, "1", nil },
		list:  func(context.Context) ([]runtime.Object, string, error) { return nil, "1", nil },
		watch: func(ctx context.Context, _ metav1.ListOptions) (watch.Interface, error) {
			if first {
				first = false
				return stream, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	done := make(chan struct{})
	go func() { defer close(done); runSource(ctx, &store, source) }()
	waitStore(t, &store, true)
	stream.Stop()
	waitStore(t, &store, false)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disconnected watch did not join")
	}
}
