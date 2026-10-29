// Package runtimeinventory holds shared, short-lived Kubernetes observations for
// protected runtimes. A stale or disconnected observation never permits access.
package runtimeinventory

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var errUnavailable = errors.New("protected runtime inventory is unavailable")

const maxObservationAge = 3 * time.Second

type objectStore struct {
	mu        sync.RWMutex
	objects   map[string]runtime.Object
	observed  time.Time
	available bool
	connected bool
	deleted   map[string]string
}

func (s *objectStore) replace(objects []runtime.Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects = make(map[string]runtime.Object, len(objects))
	s.deleted = make(map[string]string)
	for _, object := range objects {
		if m, err := meta.Accessor(object); err == nil {
			s.objects[m.GetName()] = object.DeepCopyObject()
		}
	}
}

func (s *objectStore) healthy(now time.Time) {
	s.mu.Lock()
	s.available = true
	s.observed = now
	s.mu.Unlock()
}
func (s *objectStore) unhealthy() { s.mu.Lock(); s.available = false; s.mu.Unlock() }

func (s *objectStore) get(name string) (runtime.Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.available || time.Since(s.observed) > maxObservationAge || s.objects[name] == nil {
		return nil, errUnavailable
	}
	return s.objects[name].DeepCopyObject(), nil
}

func (s *objectStore) isHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.available && time.Since(s.observed) <= maxObservationAge
}

func (s *objectStore) snapshot() (map[string]runtime.Object, error) {
	return s.selectObjects(func(string, runtime.Object) bool { return true })
}

func (s *objectStore) selectObjects(selectObject func(string, runtime.Object) bool) (map[string]runtime.Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.available || time.Since(s.observed) > maxObservationAge {
		return nil, errUnavailable
	}
	result := make(map[string]runtime.Object)
	for name, object := range s.objects {
		if selectObject(name, object) {
			result[name] = object.DeepCopyObject()
		}
	}
	return result, nil
}

// Inventory is shared by all protected assignments and all proxy requests.
// Its configured Pod ranges must come from the actual CNI configuration; Node
// spec.podCIDRs can describe different ranges and are not a substitute.
type Inventory struct {
	assignments, pods, nodes, services objectStore
	podRanges                          []netip.Prefix
	controlAddresses                   []netip.Addr
	cancel                             context.CancelFunc
	done                               chan struct{}
}

func newInventory(podCIDRs, controls []string) (*Inventory, error) {
	i := &Inventory{}
	if len(podCIDRs) == 0 {
		return nil, errors.New("actual cluster Pod CIDRs are required")
	}
	for _, value := range podCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || prefix != prefix.Masked() || prefix.Addr().Is4In6() {
			return nil, errors.New("invalid cluster Pod CIDR")
		}
		i.podRanges = append(i.podRanges, prefix)
	}
	for _, value := range controls {
		address, err := netip.ParseAddr(value)
		if err != nil || address.Zone() != "" {
			return nil, errors.New("literal cluster control addresses are required")
		}
		i.controlAddresses = append(i.controlAddresses, address.Unmap())
	}
	return i, nil
}

func (i *Inventory) Assignment(ctx context.Context, id int) (*corev1.ConfigMap, []corev1.Pod, error) {
	if err := i.checkHealthy(ctx); err != nil {
		return nil, nil, err
	}
	name := fmt.Sprintf("lite-instance-%d", id)
	assignment, err := i.assignments.get(name)
	if err != nil {
		return nil, nil, err
	}
	cm, ok := assignment.(*corev1.ConfigMap)
	if !ok {
		return nil, nil, errUnavailable
	}
	endpoint, err := url.Parse(cm.Data["endpoint"])
	if err != nil || endpoint.Hostname() == "" {
		return nil, nil, errUnavailable
	}
	podObjects, err := i.pods.selectObjects(func(_ string, object runtime.Object) bool {
		pod, ok := object.(*corev1.Pod)
		return ok && pod.Status.PodIP == endpoint.Hostname()
	})
	if err != nil {
		return nil, nil, err
	}
	pods := make([]corev1.Pod, 0, len(podObjects))
	for _, object := range podObjects {
		pod, ok := object.(*corev1.Pod)
		if !ok {
			return nil, nil, errUnavailable
		}
		pods = append(pods, *pod)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return cm, pods, nil
}

// Deny runs on the resolved literal destination, after private exceptions and
// immediately before dialing. An exception cannot override this boundary.
func (i *Inventory) Deny(ctx context.Context, address netip.Addr) (bool, error) {
	prefixes, err := i.DeniedPrefixes(ctx)
	if err != nil {
		return false, err
	}
	address = address.Unmap()
	if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsLinkLocalUnicast() || address.IsLoopback() {
		return true, nil
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true, nil
		}
	}
	return false, ctx.Err()
}

// DeniedPrefixes exports the same fresh cluster/control boundary to trusted
// named execution services. It does not grant business traffic any exception.
func (i *Inventory) DeniedPrefixes(ctx context.Context) ([]netip.Prefix, error) {
	if err := i.checkHealthy(ctx); err != nil {
		return nil, err
	}
	nodes, err := i.nodes.snapshot()
	if err != nil || len(nodes) == 0 {
		return nil, errUnavailable
	}
	services, err := i.services.snapshot()
	if err != nil || len(services) == 0 {
		return nil, errUnavailable
	}
	prefixes := append([]netip.Prefix(nil), i.podRanges...)
	addAddress := func(address netip.Addr) {
		address = address.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
	}
	for _, address := range i.controlAddresses {
		addAddress(address)
	}
	for _, value := range []string{"100.100.100.200", "168.63.129.16", "fd00:ec2::254"} {
		addAddress(netip.MustParseAddr(value))
	}
	prefixes = append(prefixes, netip.MustParsePrefix("169.254.0.0/16"))
	for _, object := range nodes {
		node, ok := object.(*corev1.Node)
		if !ok {
			return nil, errUnavailable
		}
		for _, candidate := range node.Status.Addresses {
			if candidate.Type != corev1.NodeInternalIP && candidate.Type != corev1.NodeExternalIP {
				continue
			}
			address, err := netip.ParseAddr(candidate.Address)
			if err != nil || address.Zone() != "" {
				return nil, errUnavailable
			}
			addAddress(address)
		}
	}
	for _, object := range services {
		service, ok := object.(*unstructured.Unstructured)
		if !ok {
			return nil, errUnavailable
		}
		cidrs, found, err := unstructured.NestedStringSlice(service.Object, "spec", "cidrs")
		if err != nil || !found || len(cidrs) == 0 {
			return nil, errUnavailable
		}
		for _, value := range cidrs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, errUnavailable
			}
			prefixes = append(prefixes, prefix)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prefixes, nil
}

func (i *Inventory) checkHealthy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, store := range []*objectStore{&i.assignments, &i.pods, &i.nodes, &i.services} {
		if !store.isHealthy() {
			return errUnavailable
		}
	}
	return nil
}
