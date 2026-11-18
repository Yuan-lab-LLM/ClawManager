package systembackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var teamDataPodGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
var teamDataSecretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
var teamDataServiceGVR = schema.GroupVersionResource{Version: "v1", Resource: "services"}
var teamDataPolicyGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}

type teamDataRedis struct{ address, admin, writer, reader string }

func teamDataRedisConfig(r teamDataRedis, id string) string {
	prefix := "cm:restore-history:" + id + ":*"
	return fmt.Sprintf("bind 0.0.0.0\nport 6379\nprotected-mode yes\nsave \"\"\nappendonly no\ndatabases 1\nuser default off\nuser admin on >%s ~* +@all\nuser writer on >%s ~%s +select +restore +dbsize +info +ping\nuser reader on >%s ~%s +select +dump +type +pttl +dbsize +info +ping +eval_ro\n", r.admin, r.writer, prefix, r.reader, prefix)
}

func teamDataRedisName(id string) string { return "team-data-redis-" + id[4:24] }

func revokeTeamRedisWriter(ctx context.Context, admin *RedisClient) error {
	if _, e := admin.command(ctx, []byte("ACL"), []byte("DELUSER"), []byte("writer")); e != nil {
		return e
	}
	u, e := admin.command(ctx, []byte("ACL"), []byte("GETUSER"), []byte("writer"))
	if e != nil {
		return e
	}
	if u != nil {
		return errors.New("Redis restore writer still active")
	}
	return nil
}
func teamDataLabels(id string) map[string]string {
	return map[string]string{"app": teamDataRedisName(id), "clawmanager.io/team-data-run": id}
}

func (s *TeamDataService) createTeamRedis(ctx context.Context, j TeamDataJob) (teamDataRedis, error) {
	var out teamDataRedis
	name := teamDataRedisName(j.ID)
	labels := teamDataLabels(j.ID)
	for _, dst := range []*string{&out.admin, &out.writer, &out.reader} {
		v, e := RandomID("")
		if e != nil {
			return out, e
		}
		*dst = v
	}
	conf := teamDataRedisConfig(out, j.ID)
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.cfg.Namespace, Labels: labels}, Data: map[string][]byte{"redis.conf": []byte(conf), "admin": []byte(out.admin), "writer": []byte(out.writer), "reader": []byte(out.reader)}}
	if e := s.createTeamDataObject(ctx, teamDataSecretGVR, j.ID, secret); e != nil {
		return out, e
	}
	// Retrieve the original generated credentials after an ambiguous create.
	stored, e := s.kube.Resource(teamDataSecretGVR).Namespace(s.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
	if e != nil {
		return out, e
	}
	var actual corev1.Secret
	if e = runtime.DefaultUnstructuredConverter.FromUnstructured(stored.Object, &actual); e != nil {
		return out, e
	}
	out.admin = string(actual.Data["admin"])
	out.writer = string(actual.Data["writer"])
	out.reader = string(actual.Data["reader"])
	if len(out.admin) != 32 || len(out.writer) != 32 || len(out.reader) != 32 {
		return out, errors.New("evidence_incomplete")
	}
	no := false
	uid := int64(999)
	mode := int32(0444)
	pod := &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.cfg.Namespace, Labels: labels}, Spec: corev1.PodSpec{AutomountServiceAccountToken: &no, RestartPolicy: corev1.RestartPolicyNever, SecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid}, Containers: []corev1.Container{{Name: "redis", Image: s.cfg.RedisImage, Command: []string{"redis-server", "/run/redis/redis.conf"}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: boolPointer(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Ports: []corev1.ContainerPort{{Name: "redis", ContainerPort: 6379}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi"), corev1.ResourceCPU: resource.MustParse("50m")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi"), corev1.ResourceCPU: resource.MustParse("1")}}, ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(6379)}}, PeriodSeconds: 1}, VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/run/redis", ReadOnly: true}, {Name: "data", MountPath: "/data"}}}}, Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name, DefaultMode: &mode}}}, {Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}}
	policy := &networkingv1.NetworkPolicy{TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.cfg.Namespace, Labels: labels}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: labels}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "clawmanager-app"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Port: intstrPointer(intstr.FromInt32(6379))}}}}, Egress: []networkingv1.NetworkPolicyEgressRule{}}}
	if e = s.createTeamDataObject(ctx, teamDataPolicyGVR, j.ID, policy); e != nil {
		return out, e
	}
	if e = s.createTeamDataObject(ctx, teamDataPodGVR, j.ID, pod); e != nil {
		return out, e
	}
	svc := &corev1.Service{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.cfg.Namespace, Labels: labels}, Spec: corev1.ServiceSpec{Selector: labels, Ports: []corev1.ServicePort{{Name: "redis", Port: 6379, TargetPort: intstr.FromInt32(6379)}}}}
	if e = s.createTeamDataObject(ctx, teamDataServiceGVR, j.ID, svc); e != nil {
		return out, e
	}
	out.address = name + "." + s.cfg.Namespace + ".svc:6379"
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		p, e := s.kube.Resource(teamDataPodGVR).Namespace(s.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
		if e != nil {
			return out, e
		}
		var actual corev1.Pod
		if e = runtime.DefaultUnstructuredConverter.FromUnstructured(p.Object, &actual); e != nil {
			return out, e
		}
		if actual.Status.Phase == corev1.PodFailed {
			return out, errors.New("restore Redis pod failed")
		}
		for _, c := range actual.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return out, nil
			}
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return out, errors.New("restore Redis readiness deadline exceeded")
}
func boolPointer(v bool) *bool                               { return &v }
func intstrPointer(v intstr.IntOrString) *intstr.IntOrString { return &v }

func (s *TeamDataService) createTeamDataObject(ctx context.Context, gvr schema.GroupVersionResource, id string, object any) error {
	raw, e := json.Marshal(object)
	if e != nil {
		return e
	}
	var obj map[string]any
	if e = json.Unmarshal(raw, &obj); e != nil {
		return e
	}
	u := &unstructured.Unstructured{Object: obj}
	client := s.kube.Resource(gvr).Namespace(s.cfg.Namespace)
	_, e = client.Create(ctx, u, metav1.CreateOptions{})
	if e == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(e) {
		return e
	}
	actual, e := client.Get(ctx, u.GetName(), metav1.GetOptions{})
	if e != nil {
		return e
	}
	if actual.GetLabels()["clawmanager.io/team-data-run"] != id || actual.GetDeletionTimestamp() != nil {
		return errors.New("restore resource identity mismatch")
	}
	return nil
}

func connectTeamRedis(ctx context.Context, r teamDataRedis, user string) (*RedisClient, error) {
	password := r.admin
	if user == "writer" {
		password = r.writer
	} else if user == "reader" {
		password = r.reader
	}
	// Pod readiness can precede Service endpoints and DNS propagation. This is
	// only for the newly provisioned copy; source capture is never retried here.
	ready, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		client, e := ConnectRedis(ready, RedisOptions{Address: r.address, Username: user, Password: password, MaxReplyBytes: 64 << 20})
		if e == nil {
			return client, nil
		}
		if e.Error() != "Redis unavailable" {
			return nil, e
		}
		select {
		case <-ready.Done():
			return nil, errors.New("restore Redis service unavailable")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *TeamDataService) deleteTeamRedis(ctx context.Context, id string) error {
	name := teamDataRedisName(id)
	for _, gvr := range []schema.GroupVersionResource{teamDataPodGVR, teamDataServiceGVR, teamDataSecretGVR, teamDataPolicyGVR} {
		client := s.kube.Resource(gvr).Namespace(s.cfg.Namespace)
		obj, e := client.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			continue
		}
		if e != nil {
			return e
		}
		if obj.GetLabels()["clawmanager.io/team-data-run"] != id {
			return errors.New("restore resource identity mismatch")
		}
		uid := obj.GetUID()
		grace := int64(1)
		if e = client.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, GracePeriodSeconds: &grace}); e != nil && !apierrors.IsNotFound(e) {
			return e
		}
		if gvr == teamDataPodGVR {
			for i := 0; i < 60; i++ {
				_, e = client.Get(ctx, name, metav1.GetOptions{})
				if apierrors.IsNotFound(e) {
					break
				}
				if e != nil {
					return e
				}
				if i == 59 {
					return errors.New("restore pod still stopping")
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
		}
	}
	return nil
}
