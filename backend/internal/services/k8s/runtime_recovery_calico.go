package k8s

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"net"
	"strings"
)

type recoveryCalicoAffinity struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		CIDR    string `json:"cidr"`
		Node    string `json:"node"`
		State   string `json:"state"`
		Deleted string `json:"deleted"`
		Type    string `json:"type"`
	} `json:"spec"`
}

// Some Calico installations allocate blocks outside Node.spec.podCIDRs. Only
// retained, uniquely attributed host allocations can supply that missing link.
// No CRD, no RBAC, or an unfamiliar allocation format means proof unavailable.
func readCalicoRecoveryNode(ctx context.Context, client kubernetes.Interface, source models.RuntimePod, nodes []corev1.Node) (*corev1.Node, string, error) {
	rest := client.CoreV1().RESTClient()
	typed, typedREST := rest.(*restclient.RESTClient)
	if rest == nil || (typedREST && typed == nil) {
		return nil, "", fmt.Errorf("source CNI allocation cannot be verified")
	}
	raw, err := rest.Get().AbsPath("/apis/crd.projectcalico.org/v1/blockaffinities").DoRaw(ctx)
	if err != nil {
		return nil, "", err
	}
	var list struct {
		Items []recoveryCalicoAffinity `json:"items"`
	}
	if err = json.Unmarshal(raw, &list); err != nil {
		return nil, "", err
	}
	return resolveCalicoRecoveryNode(source, nodes, list.Items)
}

func resolveCalicoRecoveryNode(source models.RuntimePod, nodes []corev1.Node, blocks []recoveryCalicoAffinity) (*corev1.Node, string, error) {
	fail := func() (*corev1.Node, string, error) {
		return nil, "", fmt.Errorf("source Calico host allocation is unproven or ambiguous")
	}
	if source.PodIP == nil || source.NodeName == nil || source.CreatedAt.IsZero() {
		return fail()
	}
	ip := net.ParseIP(*source.PodIP)
	if ip == nil || ip.To4() == nil {
		return fail()
	}
	var match *recoveryCalicoAffinity
	for i := range blocks {
		b := &blocks[i]
		base, network, err := net.ParseCIDR(b.Spec.CIDR)
		if err != nil {
			return fail()
		}
		if !network.Contains(ip) {
			continue
		}
		if match != nil || !base.Equal(network.IP) || network.String() != b.Spec.CIDR {
			return fail()
		}
		match = b
	}
	if match == nil {
		return fail()
	}
	b := match
	expectedName := b.Spec.Node + "-" + strings.NewReplacer(".", "-", "/", "-").Replace(b.Spec.CIDR)
	if b.Name != expectedName || b.UID == "" || b.DeletionTimestamp != nil || b.CreationTimestamp.IsZero() || b.CreationTimestamp.Time.After(source.CreatedAt) || b.Spec.Deleted != "false" || b.Spec.State != "confirmed" || b.Spec.Type != "host" || b.Spec.Node != *source.NodeName {
		return fail()
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Name == b.Spec.Node {
			raw, _ := json.Marshal([]string{b.Name, string(b.UID), b.Spec.CIDR, n.Name, string(n.UID)})
			return n, string(raw), nil
		}
	}
	return fail()
}
