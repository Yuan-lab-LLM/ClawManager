package systembackup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type KubernetesSelection struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
	Name     string `json:"name"`
	UID      string `json:"uid"`
}
type KubernetesRecord struct {
	Kind         string               `json:"kind"`
	Namespace    string               `json:"namespace,omitempty"`
	NamespaceUID string               `json:"namespace_uid,omitempty"`
	Part         string               `json:"part,omitempty"`
	Selection    *KubernetesSelection `json:"selection,omitempty"`
	Object       json.RawMessage      `json:"object,omitempty"`
	Count        int64                `json:"count,omitempty"`
	SHA256       string               `json:"sha256,omitempty"`
}

var backupResourceKinds = map[schema.GroupVersionResource]string{
	{Version: "v1", Resource: "configmaps"}:                                       "ConfigMap",
	{Version: "v1", Resource: "services"}:                                         "Service",
	{Version: "v1", Resource: "serviceaccounts"}:                                  "ServiceAccount",
	{Version: "v1", Resource: "persistentvolumeclaims"}:                           "PersistentVolumeClaim",
	{Version: "v1", Resource: "pods"}:                                             "Pod",
	{Version: "v1", Resource: "secrets"}:                                          "Secret",
	{Group: "apps", Version: "v1", Resource: "deployments"}:                       "Deployment",
	{Group: "apps", Version: "v1", Resource: "statefulsets"}:                      "StatefulSet",
	{Group: "apps", Version: "v1", Resource: "daemonsets"}:                        "DaemonSet",
	{Group: "batch", Version: "v1", Resource: "jobs"}:                             "Job",
	{Group: "batch", Version: "v1", Resource: "cronjobs"}:                         "CronJob",
	{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}:      "NetworkPolicy",
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}:            "Ingress",
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}:        "Role",
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}: "RoleBinding",
}

func (s KubernetesSelection) gvr() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: s.Group, Version: s.Version, Resource: s.Resource}
}
func (s KubernetesSelection) identity() string {
	return s.Group + "/" + s.Version + "/" + s.Resource + "/" + s.Name
}
func (s KubernetesSelection) valid(part string) bool {
	_, ok := backupResourceKinds[s.gvr()]
	return ok && s.Name != "" && s.UID != "" && ((part == "secret_bundle" && s.Resource == "secrets") || (part == "deployment_resources" && s.Resource != "secrets"))
}

// CaptureKubernetesResources is GET-only and reads a server-frozen inventory.
// Separate clients/service accounts must supply deployment and Secret parts.
// Resource UID drift is fatal; this never applies manifests or copies access
// credentials into a live restored installation.
func CaptureKubernetesResources(ctx context.Context, c dynamic.Interface, w io.Writer, namespace, namespaceUID, part string, selection []KubernetesSelection, b ObjectBudget) error {
	if c == nil || namespace == "" || namespaceUID == "" || b.MaxBytes <= 0 || b.MaxItems <= 0 || len(selection) > b.MaxItems || (part != "deployment_resources" && part != "secret_bundle") {
		return errors.New("invalid Kubernetes capture configuration")
	}
	// Namespace identity is verified by the controller's independent inventory.
	// Capture accounts need only GET on the listed namespaced objects.
	selection = append([]KubernetesSelection(nil), selection...)
	sort.Slice(selection, func(i, j int) bool { return selection[i].identity() < selection[j].identity() })
	whole := sha256.New()
	var total int64
	emit := func(v KubernetesRecord, seal bool) error {
		raw, e := json.Marshal(v)
		if e != nil {
			return e
		}
		raw = append(raw, '\n')
		total += int64(len(raw))
		if total > b.MaxBytes || len(raw) > maxRowBytes {
			return errors.New("capacity_exceeded")
		}
		if !seal {
			whole.Write(raw)
		}
		return writeFull(w, raw)
	}
	if e := emit(KubernetesRecord{Kind: "kubernetes_history_v1", Namespace: namespace, NamespaceUID: namespaceUID, Part: part}, false); e != nil {
		return e
	}
	for i, item := range selection {
		if !item.valid(part) || (i > 0 && item.identity() == selection[i-1].identity()) {
			return errors.New("invalid Kubernetes inventory")
		}
		object, e := c.Resource(item.gvr()).Namespace(namespace).Get(ctx, item.Name, metav1.GetOptions{})
		if e != nil {
			return errors.New("Kubernetes source unavailable")
		}
		if string(object.GetUID()) != item.UID || object.GetNamespace() != namespace || object.GetName() != item.Name || object.GetKind() != backupResourceKinds[item.gvr()] || object.GetAPIVersion() != item.gvr().GroupVersion().String() {
			return errors.New("Kubernetes source identity changed")
		}
		raw, e := json.Marshal(object.Object)
		if e != nil {
			return e
		}
		if e = emit(KubernetesRecord{Kind: "resource", Selection: &item, Object: raw}, false); e != nil {
			return e
		}
	}
	return emit(KubernetesRecord{Kind: "end", Count: int64(len(selection)), SHA256: hex.EncodeToString(whole.Sum(nil))}, true)
}

// ReadKubernetesHistory yields historical objects to a data-only evidence sink.
// P0 intentionally has no Apply/Restore client: old pods, schedules, tokens and
// service accounts must not become active merely because data was restored.
func ReadKubernetesHistory(ctx context.Context, r io.Reader, part string, b ObjectBudget, onResource func(KubernetesSelection, json.RawMessage) error) error {
	if b.MaxBytes <= 0 || b.MaxItems <= 0 || (part != "deployment_resources" && part != "secret_bundle") {
		return errors.New("invalid resource budget")
	}
	scan := bufio.NewScanner(io.LimitReader(r, b.MaxBytes+1))
	scan.Buffer(make([]byte, 64<<10), maxRowBytes)
	whole := sha256.New()
	var total, count int64
	started, ended := false, false
	namespace, previous := "", ""
	for scan.Scan() {
		if e := ctx.Err(); e != nil {
			return e
		}
		raw := scan.Bytes()
		total += int64(len(raw)) + 1
		if total > b.MaxBytes || ended {
			return errors.New("resource archive exceeds bounds")
		}
		var record KubernetesRecord
		if e := strictDecode(raw, &record); e != nil {
			return errors.New("invalid resource record")
		}
		if !started {
			if record.Kind != "kubernetes_history_v1" || record.Namespace == "" || record.NamespaceUID == "" || record.Part != part {
				return errors.New("invalid resource header")
			}
			namespace = record.Namespace
			started = true
		} else {
			switch record.Kind {
			case "resource":
				s := record.Selection
				if s == nil || !s.valid(part) || s.identity() <= previous {
					return errors.New("invalid resource inventory")
				}
				previous = s.identity()
				count++
				if count > int64(b.MaxItems) {
					return errors.New("capacity_exceeded")
				}
				var object unstructured.Unstructured
				if e := json.Unmarshal(record.Object, &object.Object); e != nil {
					return e
				}
				if string(object.GetUID()) != s.UID || object.GetNamespace() != namespace || object.GetName() != s.Name || object.GetKind() != backupResourceKinds[s.gvr()] || object.GetAPIVersion() != s.gvr().GroupVersion().String() {
					return errors.New("resource identity mismatch")
				}
				if onResource != nil {
					if e := onResource(*s, record.Object); e != nil {
						return e
					}
				}
			case "end":
				if count != record.Count || record.SHA256 != hex.EncodeToString(whole.Sum(nil)) {
					return errors.New("resource checksum mismatch")
				}
				ended = true
			default:
				return errors.New("unknown resource record")
			}
		}
		if record.Kind != "end" {
			canonical, e := json.Marshal(record)
			if e != nil {
				return e
			}
			whole.Write(append(canonical, '\n'))
		}
	}
	if e := scan.Err(); e != nil {
		return e
	}
	if !ended {
		return errors.New("incomplete resource archive")
	}
	return nil
}
