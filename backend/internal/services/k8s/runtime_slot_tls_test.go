package k8s

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRuntimeSlotTLSCreatesPinnedTrustAndReusesKeypair(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	if err := EnsureRuntimeSlotTLS(ctx, kube, "runtime"); err != nil {
		t.Fatal(err)
	}
	before, err := kube.CoreV1().Secrets("runtime").Get(ctx, "clawmanager-lite-agent-tls", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Type != corev1.SecretTypeTLS {
		t.Fatal("TLS Secret type missing")
	}
	pair, err := tls.X509KeyPair(before.Data[corev1.TLSCertKey], before.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		t.Fatal(err)
	}
	client, err := RuntimeSlotTLSConfig(ctx, kube, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if client.InsecureSkipVerify || client.RootCAs == nil || client.ServerName != "lite-agent.clawmanager.internal" || client.MinVersion != tls.VersionTLS13 {
		t.Fatal("slot client did not require pinned TLS1.3 trust")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: client.RootCAs, DNSName: client.ServerName}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRuntimeSlotTLS(ctx, kube, "runtime"); err != nil {
		t.Fatal(err)
	}
	after, _ := kube.CoreV1().Secrets("runtime").Get(ctx, before.Name, metav1.GetOptions{})
	for _, key := range []string{"ca.crt", corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
		if !bytes.Equal(before.Data[key], after.Data[key]) {
			t.Fatal("backend restart rotated active slot TLS identity")
		}
	}
	other, err := newRuntimeSlotTLSSecret("other", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(other.Data[corev1.TLSCertKey])
	forged, _ := x509.ParseCertificate(block.Bytes)
	if _, err := forged.Verify(x509.VerifyOptions{Roots: client.RootCAs, DNSName: client.ServerName}); err == nil {
		t.Fatal("untrusted server certificate accepted")
	}
}

func TestRuntimeSlotTLSRejectsExpiredOrCorruptIdentity(t *testing.T) {
	for name, change := range map[string]func(*corev1.Secret){
		"CA and server expired": func(secret *corev1.Secret) {
			old, err := newRuntimeSlotTLSSecret("runtime", time.Now().AddDate(-11, 0, 0))
			if err != nil {
				t.Fatal(err)
			}
			secret.Data = old.Data
		},
		"server expired": func(secret *corev1.Secret) {
			old, err := newRuntimeSlotTLSSecret("runtime", time.Now().AddDate(-2, 0, 0))
			if err != nil {
				t.Fatal(err)
			}
			secret.Data = old.Data
		},
		"key mismatch": func(secret *corev1.Secret) {
			other, _ := newRuntimeSlotTLSSecret("runtime", time.Now())
			secret.Data[corev1.TLSPrivateKeyKey] = other.Data[corev1.TLSPrivateKeyKey]
		},
		"corrupt CA": func(secret *corev1.Secret) { secret.Data["ca.crt"] = []byte("not a certificate") },
	} {
		t.Run(name, func(t *testing.T) {
			secret, err := newRuntimeSlotTLSSecret("runtime", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			change(secret)
			kube := fake.NewSimpleClientset(secret)
			if err := EnsureRuntimeSlotTLS(context.Background(), kube, "runtime"); err == nil {
				t.Fatal("unsafe existing identity silently reused or rotated")
			}
			if _, err := RuntimeSlotTLSConfig(context.Background(), kube, "runtime"); err == nil {
				t.Fatal("unsafe identity became trusted")
			}
		})
	}
}

func TestRuntimeSlotTLSMountIsCoordinatorOnly(t *testing.T) {
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "openclaw", Image: "image", SlotCount: 2, AgentTokenSecretName: "tokens"})
	coordinator := d.Spec.Template.Spec.Containers[0]
	requireEnv(t, coordinator, "RUNTIME_AGENT_TLS_CERT_FILE", "/var/run/clawmanager-agent-tls/tls.crt")
	requireEnv(t, coordinator, "RUNTIME_AGENT_TLS_KEY_FILE", "/var/run/clawmanager-agent-tls/tls.key")
	if coordinator.ReadinessProbe.HTTPGet.Scheme != corev1.URISchemeHTTPS {
		t.Fatal("coordinator readiness uses plaintext HTTP")
	}
	volume := ""
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == "clawmanager-lite-agent-tls" {
			volume = v.Name
		}
	}
	if volume == "" {
		t.Fatal("server TLS Secret not mounted")
	}
	mounted := false
	for _, mount := range coordinator.VolumeMounts {
		if mount.Name == volume {
			mounted = mount.ReadOnly && mount.MountPath == "/var/run/clawmanager-agent-tls"
		}
	}
	if !mounted {
		t.Fatal("server private key mount must be read-only on coordinator")
	}
	for _, slot := range d.Spec.Template.Spec.Containers[1:] {
		requireEnvAbsent(t, slot, "RUNTIME_AGENT_TLS_CERT_FILE")
		requireEnvAbsent(t, slot, "RUNTIME_AGENT_TLS_KEY_FILE")
		for _, mount := range slot.VolumeMounts {
			if mount.Name == volume {
				t.Fatal("server private key exposed to slot container")
			}
		}
	}
}

func TestRuntimeSlotTLSRejectsUnsafeServingDeployment(t *testing.T) {
	for name, change := range map[string]func(*corev1.PodSpec){
		"plaintext probe":         func(pod *corev1.PodSpec) { pod.Containers[0].ReadinessProbe.HTTPGet.Scheme = corev1.URISchemeHTTP },
		"missing certificate env": func(pod *corev1.PodSpec) { removeEnvVar(&pod.Containers[0], "RUNTIME_AGENT_TLS_CERT_FILE") },
		"key mount in slot": func(pod *corev1.PodSpec) {
			for _, mount := range pod.Containers[0].VolumeMounts {
				if mount.MountPath == "/var/run/clawmanager-agent-tls" {
					pod.Containers[1].VolumeMounts = append(pod.Containers[1].VolumeMounts, mount)
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "openclaw", Image: "image", SlotCount: 1, AgentTokenSecretName: "tokens"})
			if err := validateRuntimeSlotTLSDeployment(deployment); err != nil {
				t.Fatal(err)
			}
			change(&deployment.Spec.Template.Spec)
			if err := validateRuntimeSlotTLSDeployment(deployment); err == nil {
				t.Fatal("unsafe active pool silently adopted")
			}
		})
	}
}

func TestRuntimeSlotTLSHandshakeRejectsImpersonatorBeforeControlData(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	if err := EnsureRuntimeSlotTLS(ctx, kube, "runtime"); err != nil {
		t.Fatal(err)
	}
	trust, err := RuntimeSlotTLSConfig(ctx, kube, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	authentic, err := kube.CoreV1().Secrets("runtime").Get(ctx, "clawmanager-lite-agent-tls", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	forged, err := newRuntimeSlotTLSSecret("forged", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]*corev1.Secret{"authentic": authentic, "forged": forged} {
		t.Run(name, func(t *testing.T) {
			pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
			if err != nil {
				t.Fatal(err)
			}
			clientWire, serverWire := net.Pipe()
			defer clientWire.Close()
			defer serverWire.Close()
			deadline := time.Now().Add(2 * time.Second)
			_ = clientWire.SetDeadline(deadline)
			_ = serverWire.SetDeadline(deadline)
			type received struct {
				data    string
				version uint16
			}
			done := make(chan received, 1)
			const control = "fixture-management-control"
			go func() {
				server := tls.Server(serverWire, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}})
				data := make([]byte, len(control))
				n, _ := io.ReadFull(server, data)
				done <- received{data: string(data[:n]), version: server.ConnectionState().Version}
			}()
			client := tls.Client(clientWire, trust.Clone())
			handshakeErr := client.HandshakeContext(ctx)
			if name == "authentic" {
				if handshakeErr != nil {
					t.Fatal(handshakeErr)
				}
				if _, err := client.Write([]byte(control)); err != nil {
					t.Fatal(err)
				}
			} else if handshakeErr == nil {
				t.Fatal("forged management listener authenticated")
			}
			_ = clientWire.Close()
			select {
			case got := <-done:
				if name == "authentic" {
					if got.data != control || got.version != tls.VersionTLS13 {
						t.Fatal("authentic TLS1.3 connection failed")
					}
				} else if got.data != "" {
					t.Fatal("control credential reached an unauthenticated listener")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("in-memory TLS peer did not finish")
			}
		})
	}
}
