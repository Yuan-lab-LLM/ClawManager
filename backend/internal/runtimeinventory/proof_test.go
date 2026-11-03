package runtimeinventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

func TestMetadataProofUsesConsistentPaginatedMetadataOnlyLists(t *testing.T) {
	var badVersion atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "1000" || r.URL.Query().Get("resourceVersion") != "" || r.URL.Query().Get("labelSelector") != "purpose=instance" {
			t.Error("proof lost consistent bounded selector")
		}
		if r.Header.Get("Accept") == "application/json" {
			t.Error("proof requested full assignment data")
		}
		list := metav1.PartialObjectMetadataList{TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "PartialObjectMetadataList"}, ListMeta: metav1.ListMeta{ResourceVersion: "100"}}
		if r.URL.Query().Get("continue") == "" {
			list.Continue = "page-two"
		} else if badVersion.Load() {
			list.ResourceVersion = "101"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}))
	defer server.Close()
	client, err := metadata.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	proof := metadataProof(client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("test"), "purpose=instance")
	if _, version, err := proof(context.Background()); err != nil || version != "100" {
		t.Fatalf("metadata pages: %v", err)
	}
	badVersion.Store(true)
	if _, _, err := proof(context.Background()); err == nil {
		t.Fatal("mixed snapshot pages accepted")
	}
}

func TestProofAccountsForNewerEventsAndDeletionReceipts(t *testing.T) {
	object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: "original", ResourceVersion: "20"}}
	var store objectStore
	store.replace([]runtime.Object{object})
	proof := []metav1.PartialObjectMetadata{{ObjectMeta: object.ObjectMeta}}
	if !matchesProof(&store, proof, "25") {
		t.Fatal("equal data rejected")
	}
	object.ResourceVersion = "26"
	store.objects["one"] = object.DeepCopy()
	if !matchesProof(&store, proof, "25") {
		t.Fatal("newer ordered event rejected")
	}
	delete(store.objects, "one")
	if matchesProof(&store, proof, "25") {
		t.Fatal("missing object accepted without deletion receipt")
	}
	store.deleted["one"] = "26"
	if !matchesProof(&store, proof, "25") {
		t.Fatal("post-snapshot deletion rejected")
	}
	store.deleted["one"] = "24"
	if matchesProof(&store, proof, "25") {
		t.Fatal("old deletion hides newer snapshot object")
	}
	store.objects["one"] = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: "recreated", ResourceVersion: "21"}}
	if matchesProof(&store, proof, "25") {
		t.Fatal("old different UID accepted")
	}
	store.objects["one"] = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "one", UID: "recreated", ResourceVersion: "26"}}
	if !matchesProof(&store, proof, "25") {
		t.Fatal("post-snapshot recreation rejected")
	}
	if !versionAtLeast("2345678901234567890123456789012345678901", "345678901234567890123456789012345678901") {
		t.Fatal("version comparison assumed fixed integer size")
	}
	for _, value := range []string{"0", "01", "-1", "1x", ""} {
		if versionAtLeast(value, "1") {
			t.Fatal("noncanonical resource version accepted")
		}
	}
}
