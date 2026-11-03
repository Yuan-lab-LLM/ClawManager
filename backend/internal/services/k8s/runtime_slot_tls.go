package k8s

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const runtimeSlotTLSSecretName = "clawmanager-lite-agent-tls"
const runtimeSlotTLSServerName = "lite-agent.clawmanager.internal"
const runtimeSlotTLSDirectory = "/var/run/clawmanager-agent-tls"

// EnsureRuntimeSlotTLS creates the namespace's management identity once. Invalid
// or expired existing material fails closed; rotation needs a coordinated rollout.
func EnsureRuntimeSlotTLS(ctx context.Context, kube kubernetes.Interface, namespace string) error {
	if kube == nil || strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("slot TLS requires Kubernetes connectivity and a namespace")
	}
	secrets := kube.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, runtimeSlotTLSSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		secret, err = newRuntimeSlotTLSSecret(namespace, time.Now())
		if err != nil {
			return err
		}
		secret, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, runtimeSlotTLSSecretName, metav1.GetOptions{})
		}
	}
	if err != nil {
		return err
	}
	_, err = validatedRuntimeSlotTLS(secret, time.Now())
	return err
}

// RuntimeSlotTLSConfig trusts only this namespace's private CA, and verifies the
// fixed management DNS identity even though requests connect to current Pod IPs.
func RuntimeSlotTLSConfig(ctx context.Context, kube kubernetes.Interface, namespace string) (*tls.Config, error) {
	if kube == nil || strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("slot TLS requires Kubernetes connectivity and a namespace")
	}
	secret, err := kube.CoreV1().Secrets(namespace).Get(ctx, runtimeSlotTLSSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	roots, err := validatedRuntimeSlotTLS(secret, time.Now())
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: runtimeSlotTLSServerName}, nil
}

func validatedRuntimeSlotTLS(secret *corev1.Secret, now time.Time) (*x509.CertPool, error) {
	if secret == nil || secret.Name != runtimeSlotTLSSecretName || secret.DeletionTimestamp != nil || secret.Type != corev1.SecretTypeTLS {
		return nil, fmt.Errorf("invalid runtime slot TLS Secret identity")
	}
	block, rest := pem.Decode(secret.Data["ca.crt"])
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, fmt.Errorf("invalid runtime slot CA certificate")
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(ca.NotBefore) || !now.Before(ca.NotAfter) {
		return nil, fmt.Errorf("runtime slot CA is invalid or expired")
	}
	if err := ca.CheckSignatureFrom(ca); err != nil {
		return nil, fmt.Errorf("runtime slot CA must be self-signed")
	}
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("runtime slot server keypair is invalid")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("runtime slot server certificate is invalid")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		certificate, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid runtime slot server chain")
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: runtimeSlotTLSServerName, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, fmt.Errorf("runtime slot server certificate trust or validity check failed")
	}
	return roots, nil
}

func newRuntimeSlotTLSSecret(namespace string, now time.Time) (*corev1.Secret, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial := func() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
	caSerial, err := serial()
	if err != nil {
		return nil, err
	}
	serverSerial, err := serial()
	if err != nil {
		return nil, err
	}
	ca := &x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "ClawManager Lite management CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	server := &x509.Certificate{SerialNumber: serverSerial, Subject: pkix.Name{CommonName: runtimeSlotTLSServerName}, DNSNames: []string{runtimeSlotTLSServerName}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return nil, err
	}
	// The CA signing key is deliberately not persisted or mounted in runtime Pods.
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: runtimeSlotTLSSecretName, Namespace: namespace}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{
		"ca.crt":                pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}),
	}}, nil
}

func validateRuntimeSlotTLSDeployment(deployment *appsv1.Deployment) error {
	pod := deployment.Spec.Template.Spec
	if len(pod.Containers) == 0 {
		return fmt.Errorf("runtime pool has no coordinator")
	}
	coordinator := pod.Containers[0]
	for key, want := range map[string]string{"RUNTIME_AGENT_TLS_CERT_FILE": runtimeSlotTLSDirectory + "/tls.crt", "RUNTIME_AGENT_TLS_KEY_FILE": runtimeSlotTLSDirectory + "/tls.key"} {
		found := false
		for _, env := range coordinator.Env {
			if env.Name == key && env.Value == want && env.ValueFrom == nil {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("serving %s requires a coordinated TLS rollout", deployment.Name)
		}
	}
	volumes := map[string]bool{}
	for _, volume := range pod.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == runtimeSlotTLSSecretName {
			volumes[volume.Name] = true
		}
	}
	mounted := false
	for _, mount := range coordinator.VolumeMounts {
		if volumes[mount.Name] && mount.ReadOnly && mount.MountPath == runtimeSlotTLSDirectory {
			mounted = true
		}
	}
	// Earlier controlled rollouts removed /readyz on activation. TLS is owned
	// by the verified listener env and private Secret mount, not by a kubelet
	// readiness probe. Adopt those existing pools without restarting writers;
	// authenticated management requests still require the namespace CA and DNS.
	if !mounted || (coordinator.ReadinessProbe != nil && (coordinator.ReadinessProbe.HTTPGet == nil || coordinator.ReadinessProbe.HTTPGet.Scheme != corev1.URISchemeHTTPS)) {
		return fmt.Errorf("serving %s requires a coordinated TLS rollout", deployment.Name)
	}
	for _, slot := range pod.Containers[1:] {
		for _, env := range slot.Env {
			if strings.HasPrefix(env.Name, "RUNTIME_AGENT_TLS_") {
				return fmt.Errorf("slot container must not receive coordinator TLS configuration")
			}
		}
		for _, mount := range slot.VolumeMounts {
			if volumes[mount.Name] || mount.MountPath == runtimeSlotTLSDirectory {
				return fmt.Errorf("slot container must not mount coordinator TLS material")
			}
		}
	}
	return nil
}
