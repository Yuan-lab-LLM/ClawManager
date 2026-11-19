package k8s

import (
	"bytes"
	"clawreef/internal/models"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestOpenCodeForceLeaseProbeRequiresBoundReadOnlyExecutor(t *testing.T) {
	svc, _, _, target := openCodeSchedulingFixture(t)
	backend, ok := any(svc).(interface {
		InspectOpenCodeForceLeaseAbsence(context.Context, *OpenCodeUpgradeSource, models.RuntimePod, []int) (string, error)
	})
	if !ok {
		t.Fatal("fixed read-only force lease probe unavailable")
	}
	uid := "target-pod-uid"
	if _, err := backend.InspectOpenCodeForceLeaseAbsence(context.Background(), target, models.RuntimePod{Namespace: "tenant", DeploymentName: target.Target.Name, PodName: "target-pod", PodUID: &uid}, []int{7}); err == nil {
		t.Fatal("probe without bound executor claimed absence")
	}
}

func TestOpenCodeForceFixedLeaseScriptUsesNativeManagementTraversalAndNeverReadsLease(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("server54 management identity fixture requires root")
	}
	for _, state := range []string{"absent", "present-expired", "symlink-parent", "missing-root", "denied"} {
		t.Run(state, func(t *testing.T) {
			base, err := os.MkdirTemp("/tmp", "cm-force-lease-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(base)
			if err = os.Chown(base, 10001, 10001); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "workspaces")
			parent := filepath.Join(root, ".clawmanager-upgrades")
			leases := filepath.Join(parent, "leases")
			if err = os.MkdirAll(leases, 0700); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{root, parent, leases} {
				if err = os.Chown(p, 10001, 10001); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.Chmod(parent, 0711); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(leases, "instance-7.json")
			secret := []byte(`{"token":"fixture-lease-must-not-print","expires_at":"2000-01-01"}`)
			switch state {
			case "present-expired":
				if err = os.WriteFile(leaf, secret, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink-parent":
				os.RemoveAll(leases)
				if err = os.Symlink(base, leases); err != nil {
					t.Fatal(err)
				}
			case "missing-root":
				root = filepath.Join(base, "missing")
			case "denied":
				if err = os.Chmod(leases, 0000); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("python3", "-I", "-B", "-c", openCodeForceLeaseProbeScript, root, "[7]")
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001}}
			out, err := cmd.CombinedOutput()
			if (err == nil) != (state == "absent") {
				t.Fatalf("state=%s err=%v output=%s", state, err, out)
			}
			if bytes.Contains(out, secret) || bytes.Contains(out, []byte("fixture-lease-must-not-print")) {
				t.Fatal("read-only probe leaked lease contents")
			}
			if state == "present-expired" {
				after, e := os.ReadFile(leaf)
				if e != nil || !bytes.Equal(after, secret) {
					t.Fatal("probe modified an existing expired lease")
				}
			}
		})
	}
}

func TestOpenCodeForceLeaseProbeDropsFrozenRootBootstrapToManagementActor(t *testing.T) {
	svc, client, _, proof := openCodeSchedulingFixture(t)
	ctx := context.Background()
	pod, err := client.CoreV1().Pods("tenant").Get(ctx, "target-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "runtime" {
			pod.Spec.Containers[i].SecurityContext.RunAsUser = &zero
		}
	}
	if _, err = client.CoreV1().Pods("tenant").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.forceProbeExec = func(_ context.Context, p *corev1.Pod, command []string) ([]byte, error) {
		calls++
		prefix := []string{"setpriv", "--reuid=10001", "--regid=10001", "--clear-groups", "--no-new-privs", "python3", "-I", "-B", "-c", openCodeForceLeaseProbeScript, "/workspaces", "[7,8]"}
		if !reflect.DeepEqual(command, prefix) || string(p.UID) != "target-pod-uid" {
			t.Fatal("probe was not fixed/drop-privilege/scoped")
		}
		return []byte(`{"absent":true,"root_device":9,"root_inode":17,"instances":[7,8]}`), nil
	}
	uid := "target-pod-uid"
	digest, err := svc.InspectOpenCodeForceLeaseAbsence(ctx, proof, models.RuntimePod{Namespace: "tenant", DeploymentName: proof.Target.Name, PodName: "target-pod", PodUID: &uid}, []int{8, 7})
	if err != nil || len(digest) != 64 || calls != 1 {
		t.Fatal(digest, err, calls)
	}
}
