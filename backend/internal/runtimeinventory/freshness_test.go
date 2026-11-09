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

func TestSuccessfulReconnectAndHistoricalBacklogCannotRenewStaleData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store objectStore
	var current atomic.Int32
	current.Store(10)
	streams := make(chan *watch.RaceFreeFakeWatcher, 3)
	newObject := func(rv string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: types.UID("same"), ResourceVersion: rv}}
	}
	source := objectSource{
		list: func(context.Context) ([]runtime.Object, string, error) {
			return []runtime.Object{newObject("10")}, "10", nil
		},
		watch: func(ctx context.Context, _ metav1.ListOptions) (watch.Interface, error) {
			stream := watch.NewRaceFreeFake()
			streams <- stream
			return stream, nil
		},
		proof: func(context.Context) ([]metav1.PartialObjectMetadata, string, error) {
			rv := "10"
			if current.Load() == 20 {
				rv = "20"
			}
			return []metav1.PartialObjectMetadata{{ObjectMeta: newObject(rv).ObjectMeta}}, rv, nil
		},
	}
	done := make(chan struct{})
	go func() { defer close(done); runSource(ctx, &store, source) }()
	first := <-streams
	waitStore(t, &store, true)
	current.Store(20)
	store.mu.Lock()
	store.observed = time.Now().Add(-4 * time.Second)
	store.mu.Unlock()
	first.Stop()
	second := <-streams
	time.Sleep(50 * time.Millisecond)
	if store.isHealthy() {
		t.Fatal("successful Watch headers restored indefinitely old assignment")
	}
	second.Modify(newObject("11"))
	time.Sleep(50 * time.Millisecond)
	if store.isHealthy() {
		t.Fatal("historical backlog event refreshed old assignment")
	}
	second.Modify(newObject("20"))
	waitStore(t, &store, true)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proof/watch teardown did not join")
	}
}

func TestAssignmentRequiresAllAuthoritativeInventorySources(t *testing.T) {
	i := readyInventory(t)
	i.assignments.replace([]runtime.Object{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7"}, Data: map[string]string{"endpoint": "https://10.244.0.2:19090"}}})
	i.nodes.unhealthy()
	if _, _, err := i.Assignment(context.Background(), 7); err == nil {
		t.Fatal("node source loss left model/tunnel capability current")
	}
}
