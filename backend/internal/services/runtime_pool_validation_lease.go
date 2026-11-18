package services

import (
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// ConfigurePoolValidationLeases runs before serving HTTP. This authority reads
// the real rollout checkpoint and never creates a synthetic business identity.
func (a *RuntimeSecurityAuthority) ConfigurePoolValidationLeases(s *RuntimeScheduler) error {
	if a == nil || s == nil || s.rolloutRepo == nil || s.runtimeNamespace != a.namespace || a.validationScheduler != nil {
		return fmt.Errorf("invalid isolated validation lease authority")
	}
	a.validationScheduler = s
	return nil
}
func (a *RuntimeSecurityAuthority) issuePoolValidationLease(ctx context.Context, id runtimeauth.Identity, peer netip.Addr) (RuntimeSecurityLease, error) {
	fail := func() (RuntimeSecurityLease, error) { return RuntimeSecurityLease{}, errRuntimeSecurity }
	if id.InstanceID != poolProbeInstanceID || id.UserID != 1 || id.Generation != 1 || id.PodUID == "" || !peer.IsValid() || peer.Zone() != "" || a.validationScheduler == nil {
		return fail()
	}
	s := a.validationScheduler
	pods, err := a.kube.CoreV1().Pods(a.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/pool-role=image-validation"})
	if err != nil {
		return fail()
	}
	for _, pod := range pods.Items {
		if string(pod.UID) != id.PodUID {
			continue
		}
		address, e := netip.ParseAddr(pod.Status.PodIP)
		if e != nil || address.Unmap() != peer.Unmap() {
			return fail()
		}
		operation := pod.Labels["clawmanager.io/upgrade-id"]
		rolloutID, e := strconv.ParseInt(operation, 10, 64)
		if e != nil || rolloutID < 1 || strconv.FormatInt(rolloutID, 10) != operation {
			return fail()
		}
		r, e := s.rolloutRepo.GetByID(ctx, rolloutID)
		if e != nil || r == nil || r.RuntimeType != RuntimeTypeOpenClaw || r.Status != "running" || r.PreflightID != nil || (r.Phase != RuntimeUpgradePhaseEmptyPoolReset && r.Phase != "requested") {
			return fail()
		}
		p, e := s.loadPoolReplacement(ctx, rolloutID)
		if e != nil || p == nil || p.Stage != "validate" || p.ProbeIndex < 0 || p.ProbeIndex >= len(p.Targets) || p.ProbePodUID != id.PodUID || p.ProbeDeploymentUID == "" || p.StageStarted.IsZero() || time.Since(p.StageStarted) > 5*time.Minute || p.StageStarted.After(time.Now().Add(time.Minute)) {
			return fail()
		}
		source := p.Targets[p.ProbeIndex]
		var sources map[string]string
		if source.Namespace != a.namespace || source.UID == "" || json.Unmarshal([]byte(stringValue(r.SourceImagesJSON)), &sources) != nil || sources[source.Namespace+"/"+source.Name] != source.Image {
			return fail()
		}
		scope := k8s.RuntimePoolValidationScope{Namespace: a.namespace, DeploymentName: replacementProbeName(r.ID, p.ProbeIndex), DeploymentUID: p.ProbeDeploymentUID, SourceName: source.Name, SourceUID: source.UID, Operation: operation, PodUID: id.PodUID, Endpoint: "https://" + net.JoinHostPort(pod.Status.PodIP, "19090"), ImageRef: r.TargetImageRef, WorkspaceRoot: s.workspaceRoot}
		if _, e = k8s.VerifyPoolValidationScope(ctx, a.kube, scope); e != nil {
			return fail()
		}
		if ctx.Err() != nil {
			return fail()
		}
		lease := RuntimeSecurityLease{InstanceID: id.InstanceID, UserID: id.UserID, Generation: id.Generation, PodUID: id.PodUID, ExpiresAt: time.Now().UTC().Truncate(time.Second).Add(15 * time.Minute)}
		// The native broker requires a printable v1-prefixed credential to start.
		// Four segments are deliberately invalid for every real runtime capability.
		// A separate derived key/domain prevents accidental capability convergence.
		derive := hmac.New(sha256.New, a.key)
		derive.Write([]byte("clawmanager/validation-startup-deny-all/v1"))
		key := derive.Sum(nil)
		for _, entry := range []struct {
			audience string
			token    *string
		}{{runtimeauth.AudienceModel, &lease.ModelToken}, {runtimeauth.AudienceEgress, &lease.EgressToken}, {runtimeauth.AudienceServices, &lease.ServicesToken}} {
			mac := hmac.New(sha256.New, key)
			fmt.Fprintf(mac, "%s\x00%d\x00%d\x00%d\x00%s\x00%d\x00%s", operation, id.InstanceID, id.UserID, id.Generation, id.PodUID, lease.ExpiresAt.Unix(), entry.audience)
			*entry.token = "v1.validation-denied." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + entry.audience
		}
		return lease, nil
	}
	return fail()
}
