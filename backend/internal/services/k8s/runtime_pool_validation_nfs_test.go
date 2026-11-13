package k8s

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestNFSValidationInitializesOnlyEphemeralWorkspace(t *testing.T) {
	source := coordinatorStateSource()
	before := source.DeepCopy()
	target, err := buildPoolValidationDeployment(source, RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid"}, "validation", "new", "43")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("validation initialization changed business source")
	}
	for _, volume := range target.Spec.Template.Spec.Volumes {
		if volume.Name == "workspaces" && volume.EmptyDir == nil || volume.NFS != nil || volume.PersistentVolumeClaim != nil || volume.HostPath != nil || volume.CSI != nil {
			t.Fatal("validation workspace can reach business storage")
		}
	}
	var init *corev1.Container
	for i := range target.Spec.Template.Spec.InitContainers {
		candidate := &target.Spec.Template.Spec.InitContainers[i]
		for _, mount := range candidate.VolumeMounts {
			if mount.Name == "workspaces" {
				if mount.MountPath != "/initialize/validation-workspace" || mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
					t.Fatal("validation initialization workspace mount changed", mount)
				}
				init = candidate
			}
		}
	}
	if init == nil {
		t.Fatal("NFS validation never initializes its private workspace root")
	}
	if len(init.Command) != 3 || init.Command[0] != "/bin/sh" || init.Command[1] != "-ec" {
		t.Fatal("unexpected initializer command")
	}
	const commands = "chown 10001:10001 /initialize/validation-workspace\nchmod 0711 /initialize/validation-workspace"
	if !strings.HasSuffix(init.Command[2], commands) {
		t.Fatal("private workspace must be management-owned and traversable without recursive changes")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("executing the rendered ownership commands requires Linux root")
	}
	root := filepath.Join(t.TempDir(), "workspace")
	if err = os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "preserve")
	if err = os.WriteFile(child, []byte("fixture"), 0640); err != nil {
		t.Fatal(err)
	}
	beforeChild, err := os.Stat(child)
	if err != nil {
		t.Fatal(err)
	}
	command := strings.ReplaceAll(commands, "/initialize/validation-workspace", root)
	if output, err := exec.Command("/bin/sh", "-ec", command).CombinedOutput(); err != nil {
		t.Fatalf("rendered workspace initialization failed: %v: %s", err, output)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	owner := info.Sys().(*syscall.Stat_t)
	if owner.Uid != 10001 || owner.Gid != 10001 || info.Mode().Perm() != 0711 {
		t.Fatalf("workspace root remains unsafe: uid=%d gid=%d mode=%o", owner.Uid, owner.Gid, info.Mode().Perm())
	}
	childInfo, err := os.Stat(child)
	if err != nil || childInfo.Sys().(*syscall.Stat_t).Uid != 0 || childInfo.Mode().Perm() != beforeChild.Mode().Perm() {
		t.Fatal("workspace initialization recursively changed child data", err)
	}
}

func TestNFSValidationRefusesUnknownWorkspaceInitializer(t *testing.T) {
	for _, kind := range []string{"missing-init", "changed-command", "missing-workspace"} {
		t.Run(kind, func(t *testing.T) {
			source := coordinatorStateSource()
			switch kind {
			case "missing-init":
				source.Spec.Template.Spec.InitContainers = nil
			case "changed-command":
				source.Spec.Template.Spec.InitContainers[0].Command = []string{"/unexpected"}
			case "missing-workspace":
				source.Spec.Template.Spec.Volumes = source.Spec.Template.Spec.Volumes[1:]
			}
			before := source.DeepCopy()
			if _, err := buildPoolValidationDeployment(source, RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid"}, "validation", "new", "43"); err == nil {
				t.Fatal("unknown NFS workspace initialization was accepted")
			}
			if !reflect.DeepEqual(source, before) {
				t.Fatal("refused validation changed source")
			}
		})
	}
}

func TestNFSValidationInitializerRefusesBusinessWorkspace(t *testing.T) {
	source := coordinatorStateSource()
	before := source.DeepCopy()
	if err := initializeValidationWorkspace(source); err == nil {
		t.Fatal("business workspace was accepted for ownership changes")
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("refused initializer changed business source")
	}
}
