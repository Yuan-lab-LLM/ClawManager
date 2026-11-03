package runtimeinventory

import (
	"context"
	"net/netip"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func readyInventory(t *testing.T) *Inventory {
	t.Helper()
	i, err := newInventory([]string{"10.244.0.0/16"}, []string{"10.130.81.54"})
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []*objectStore{&i.assignments, &i.pods, &i.nodes, &i.services} {
		store.replace(nil)
		store.healthy(time.Now())
	}
	return i
}

func TestInventoryHardDenyAndUnavailable(t *testing.T) {
	i := readyInventory(t)
	i.nodes.replace([]runtime.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "public-node"}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "8.8.4.4"}}}}})
	i.services.replace([]runtime.Object{&unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "kubernetes"}, "spec": map[string]interface{}{"cidrs": []interface{}{"10.96.0.0/12"}}}}})
	for _, ip := range []string{"10.244.235.224", "10.96.0.1", "10.130.81.54", "8.8.4.4", "169.254.169.254", "127.0.0.1", "::1", "::ffff:169.254.169.254", "fe80::1", "ff02::1"} {
		denied, err := i.Deny(context.Background(), netip.MustParseAddr(ip))
		if err != nil || !denied {
			t.Errorf("hard deny missed %s: %v", ip, err)
		}
	}
	for _, ip := range []string{"1.1.1.1", "192.168.40.3"} {
		denied, err := i.Deny(context.Background(), netip.MustParseAddr(ip))
		if err != nil || denied {
			t.Errorf("unrelated target denied %s: %v", ip, err)
		}
	}
	i.nodes.unhealthy()
	if _, err := i.Deny(context.Background(), netip.MustParseAddr("1.1.1.1")); err == nil {
		t.Fatal("unavailable inventory allowed public dial")
	}
}

func TestInventoryAssignmentCopiesAndFreshness(t *testing.T) {
	i := readyInventory(t)
	i.assignments.replace([]runtime.Object{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7"}, Data: map[string]string{"generation": "3", "endpoint": "https://10.244.0.2:19090"}}})
	i.pods.replace([]runtime.Object{&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "one"}, Status: corev1.PodStatus{PodIP: "10.244.0.2"}}})
	cm, pods, err := i.Assignment(context.Background(), 7)
	if err != nil || len(pods) != 1 {
		t.Fatalf("assignment: %v", err)
	}
	cm.Data["generation"] = "spoof"
	pods[0].Status.PodIP = "spoof"
	cm, pods, err = i.Assignment(context.Background(), 7)
	if err != nil || cm.Data["generation"] != "3" || pods[0].Status.PodIP != "10.244.0.2" {
		t.Fatal("caller mutated authoritative snapshot")
	}
	i.assignments.healthy(time.Now().Add(-4 * time.Second))
	if _, _, err := i.Assignment(context.Background(), 7); err == nil {
		t.Fatal("stale watch snapshot accepted")
	}
	i.assignments.healthy(time.Now())
	i.pods.unhealthy()
	if _, _, err := i.Assignment(context.Background(), 7); err == nil {
		t.Fatal("disconnected Pod watch accepted")
	}
}

func TestInventoryRequiresActualConfiguredPodRanges(t *testing.T) {
	for _, ranges := range [][]string{nil, {"typo"}, {"0.0.0.0/0"}, {"10.244.0.1/16"}} {
		if _, err := newInventory(ranges, nil); err == nil {
			t.Fatalf("accepted unsafe/ambiguous ranges: %v", ranges)
		}
	}
}

func TestInventoryExportsSameProtectedRangesForNamedWorkers(t *testing.T) {
	i := readyInventory(t)
	i.nodes.replace([]runtime.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "8.8.4.4"}}}}})
	i.services.replace([]runtime.Object{&unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "service"}, "spec": map[string]interface{}{"cidrs": []interface{}{"10.96.0.0/12"}}}}})
	ranges, err := i.DeniedPrefixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"10.244.1.2", "10.96.0.1", "8.8.4.4", "10.130.81.54", "169.254.169.254", "168.63.129.16"} {
		found := false
		for _, prefix := range ranges {
			if prefix.Contains(netip.MustParseAddr(ip)) {
				found = true
			}
		}
		if !found {
			t.Fatalf("named worker could bypass protected address %s", ip)
		}
		denied, err := i.Deny(context.Background(), netip.MustParseAddr(ip))
		if err != nil || !denied {
			t.Fatal("proxy/workerdenyviewsdisagree")
		}
	}
	i.nodes.unhealthy()
	if _, err := i.DeniedPrefixes(context.Background()); err == nil {
		t.Fatal("unavailableinventoryexportedpermissivepolicy")
	}
}
