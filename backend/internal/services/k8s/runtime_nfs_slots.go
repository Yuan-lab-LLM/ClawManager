package k8s

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// configureNFSRootlessSlots initializes only local emptyDirs. The NFS root must
// already be provisioned for management UID 10001; never recursively chown it.
func configureNFSRootlessSlots(d *appsv1.Deployment) {
	pod := &d.Spec.Template.Spec
	yes, no := true, false
	rootUID := int64(0)
	pod.HostUsers, pod.ShareProcessNamespace = &yes, &no
	d.Labels["clawmanager.io/sandbox-profile"] = RuntimeNFSRootlessProfile
	d.Spec.Template.Labels["clawmanager.io/sandbox-profile"] = RuntimeNFSRootlessProfile

	init := corev1.Container{
		Name: "runtime-local-state", Image: pod.Containers[0].Image,
		ImagePullPolicy: pod.Containers[0].ImagePullPolicy,
		SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUID, RunAsGroup: &rootUID, Privileged: &no, AllowPrivilegeEscalation: &no,
			Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"CHOWN", "DAC_OVERRIDE", "FOWNER"}},
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
	}
	var commands []string
	prepare := func(name, target, mode string) {
		init.VolumeMounts = append(init.VolumeMounts, corev1.VolumeMount{Name: name, MountPath: target})
		commands = append(commands, "chown 10001:10001 "+target, "chmod "+mode+" "+target)
	}
	prepare(runtimeSlotSocketVolume, "/initialize/sockets", "0700")
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "runtime-agent-tls" {
			source := pod.Volumes[i].DeepCopy()
			source.Name = "runtime-agent-tls-source"
			pod.Volumes[i].VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}
			pod.Volumes = append(pod.Volumes, *source)
			break
		}
	}
	prepare("runtime-agent-tls", "/initialize/tls", "0700")
	init.VolumeMounts = append(init.VolumeMounts, corev1.VolumeMount{Name: "runtime-agent-tls-source", MountPath: "/initialize/tls-source", ReadOnly: true})
	commands = append(commands,
		"cp /initialize/tls-source/tls.crt /initialize/tls-source/tls.key /initialize/tls/",
		"chown 10001:10001 /initialize/tls/tls.crt /initialize/tls/tls.key",
		"chmod 0400 /initialize/tls/tls.crt /initialize/tls/tls.key")
	for i := range pod.Containers {
		c := &pod.Containers[i]
		upsertEnvVar(c, "RUNTIME_SANDBOX_PROFILE", RuntimeNFSRootlessProfile)
		// This runtime does not populate effective capabilities for OCI nonroot
		// users. A fixed trusted bootstrap establishes the management identity and
		// ambient SETUID/GID before execing tini; user commands never pass here.
		c.SecurityContext.RunAsUser, c.SecurityContext.RunAsGroup = &rootUID, &rootUID
		c.SecurityContext.Capabilities = &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SETUID", "SETGID"}}
		c.Command = append([]string{"/usr/bin/setpriv", "--reuid=10001", "--regid=10001", "--clear-groups", "--inh-caps=+setuid,+setgid", "--ambient-caps=+setuid,+setgid", "--"}, c.Command...)
		stateName, statePath := "runtime-coordinator-state", "/var/lib/clawmanager-coordinator"
		if i > 0 {
			stateName, statePath = c.Name+"-state", "/var/lib/clawmanager-slot"
			proc := corev1.DefaultProcMount
			c.SecurityContext.ProcMount = &proc
			c.TerminationMessagePath = "/dev/termination-log"
			c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
			upsertEnvVar(c, "RUNTIME_SLOT_TERMINATION_MESSAGE_PATH", c.TerminationMessagePath)
		} else {
			pod.Volumes = append(pod.Volumes, corev1.Volume{Name: stateName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: stateName, MountPath: statePath})
		}
		upsertEnvVar(c, "RUNTIME_AGENT_DATA_DIR", statePath)
		prepare(stateName, fmt.Sprintf("/initialize/state-%d", i), "0700")
		for j, path := range []string{"/run/clawmanager-network-public", "/run/clawmanager-proxy-policy"} {
			name := fmt.Sprintf("%s-public-%d", c.Name, j)
			pod.Volumes = append(pod.Volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}})
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: name, MountPath: path})
			prepare(name, fmt.Sprintf("/initialize/public-%d-%d", i, j), "0755")
		}
	}
	init.Command = []string{"/bin/sh", "-ec", strings.Join(commands, "\n")}
	pod.InitContainers = append(pod.InitContainers, init)
}
