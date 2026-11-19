package k8s

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

type OpenCodeForceLeaseProbe interface {
	InspectOpenCodeForceLeaseAbsence(context.Context, *OpenCodeUpgradeSource, models.RuntimePod, []int) (string, error)
}

// Fixed metadata-only program. Client inputs never select a script, directory,
// executable or lease contents. Any present lease, even expired, is a refusal.
const openCodeForceLeaseProbeScript = `import json,os,stat,sys
fds=[]
try:
 if os.geteuid()!=10001: raise ValueError()
 root=sys.argv[1];ids=json.loads(sys.argv[2])
 if not root.startswith('/') or root=='/' or os.path.normpath(root)!=root or not ids or any(type(i)!=int or i<=0 for i in ids):raise ValueError()
 flags=os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW
 fd=os.open('/',flags);fds.append(fd)
 for part in root.strip('/').split('/'):
  fd=os.open(part,flags,dir_fd=fd);fds.append(fd)
 base=os.fstat(fd)
 if not stat.S_ISDIR(base.st_mode):raise ValueError()
 missing=False
 for part in ('.clawmanager-upgrades','leases'):
  try:nxt=os.open(part,flags,dir_fd=fd)
  except FileNotFoundError:missing=True;break
  fds.append(nxt);fd=nxt;st=os.fstat(fd)
  if st.st_uid!=10001 or st.st_mode&(0o022 if part=='.clawmanager-upgrades' else 0o077):raise ValueError()
 if not missing:
  for i in ids:
   try:os.stat('instance-'+str(i)+'.json',dir_fd=fd,follow_symlinks=False)
   except FileNotFoundError:continue
   raise ValueError()
 print(json.dumps({'absent':True,'root_device':base.st_dev,'root_inode':base.st_ino,'instances':ids},sort_keys=True))
except Exception:
 print('{"absent":false}');sys.exit(1)
finally:
 for fd in reversed(fds):os.close(fd)
`

func (s *runtimeDeploymentService) InspectOpenCodeForceLeaseAbsence(ctx context.Context, proof *OpenCodeUpgradeSource, pod models.RuntimePod, ids []int) (string, error) {
	if proof == nil || pod.PodUID == nil || *pod.PodUID == "" || pod.Namespace != proof.Target.Namespace || pod.DeploymentName != proof.Target.Name || len(ids) == 0 {
		return "", fmt.Errorf("OpenCode lease probe scope incomplete")
	}
	current, err := s.client.CoreV1().Pods(pod.Namespace).Get(ctx, pod.PodName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if string(current.UID) != *pod.PodUID || current.DeletionTimestamp != nil {
		return "", fmt.Errorf("OpenCode lease probe Pod UID changed")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, current.Namespace, current.Spec)
	if err != nil {
		return "", err
	}
	if storage != proof.StorageIdentity {
		return "", fmt.Errorf("OpenCode lease probe is not on frozen storage")
	}
	root := ""
	management := false
	bootstrap := false
	for _, container := range current.Spec.Containers {
		if container.Image != proof.Target.Image {
			return "", fmt.Errorf("OpenCode lease probe image changed")
		}
		if container.Name != "runtime" {
			continue
		}
		management = container.SecurityContext != nil && container.SecurityContext.RunAsUser != nil && (*container.SecurityContext.RunAsUser == 10001 || *container.SecurityContext.RunAsUser == 0)
		bootstrap = management && *container.SecurityContext.RunAsUser == 0
		for _, e := range container.Env {
			if e.Name == "RUNTIME_WORKSPACE_ROOT" && e.ValueFrom == nil {
				root = e.Value
			}
		}
		mounted := false
		for _, m := range container.VolumeMounts {
			if m.Name == runtimeWorkspaceVolume && m.MountPath == root && m.SubPath == "" && m.SubPathExpr == "" {
				mounted = true
			}
		}
		if !mounted {
			return "", fmt.Errorf("OpenCode lease probe root is not the frozen workspace mount")
		}
	}
	if !management || !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return "", fmt.Errorf("OpenCode lease probe management/root identity unavailable")
	}
	ids = append([]int(nil), ids...)
	sort.Ints(ids)
	for i, id := range ids {
		if id <= 0 || i > 0 && ids[i-1] == id {
			return "", fmt.Errorf("OpenCode lease probe instance scope invalid")
		}
	}
	data, _ := json.Marshal(ids)
	command := []string{"python3", "-I", "-B", "-c", openCodeForceLeaseProbeScript, root, string(data)}
	if bootstrap {
		command = append([]string{"setpriv", "--reuid=10001", "--regid=10001", "--clear-groups", "--no-new-privs"}, command...)
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := s.runOpenCodeForceProbe(deadline, current, command)
	if err != nil {
		return "", fmt.Errorf("OpenCode lease absence could not be proved; existing or unknown leases remain protected")
	}
	var result struct {
		Absent     bool   `json:"absent"`
		RootDevice uint64 `json:"root_device"`
		RootInode  uint64 `json:"root_inode"`
		Instances  []int  `json:"instances"`
	}
	if len(output) > 4096 || json.Unmarshal(output, &result) != nil || !result.Absent || result.RootInode == 0 || fmt.Sprint(result.Instances) != fmt.Sprint(ids) {
		return "", fmt.Errorf("OpenCode lease probe refused or returned an unknown result")
	}
	after, err := s.client.CoreV1().Pods(pod.Namespace).Get(ctx, pod.PodName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if after.UID != current.UID || after.DeletionTimestamp != nil {
		return "", fmt.Errorf("OpenCode lease probe Pod changed during inspection")
	}
	hash := sha256.Sum256(output)
	return hex.EncodeToString(hash[:]), nil
}
func (s *runtimeDeploymentService) runOpenCodeForceProbe(ctx context.Context, pod *corev1.Pod, command []string) ([]byte, error) {
	if s.forceProbeExec != nil {
		return s.forceProbeExec(ctx, pod, command)
	}
	if s.execConfig == nil || s.client.CoreV1().RESTClient() == nil {
		return nil, fmt.Errorf("OpenCode fixed metadata probe unavailable")
	}
	req := s.client.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{Container: "runtime", Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(s.execConfig, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &forceProbeWriter{target: &output, remaining: 4096}, Stderr: io.Discard})
	return output.Bytes(), err
}

type forceProbeWriter struct {
	target    io.Writer
	remaining int
}

func (w *forceProbeWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("bounded OpenCode probe output exceeded")
	}
	n, e := w.target.Write(p)
	w.remaining -= n
	return n, e
}
