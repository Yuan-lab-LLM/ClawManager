package k8s

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const HermesWebRolloutPhase = "hermes_web_image_rollout"

// HermesWebDeploymentService keeps the optional Web contract out of other
// runtimes and legacy Hermes. The returned environment is control-plane owned.
type HermesWebDeploymentService interface {
	HermesWebEnvironment(context.Context, string, string) (map[string]string, error)
	RolloutHermesWebImage(context.Context, string, string, string, int, int) error
	HermesWebRolloutReady(context.Context, string, string) (bool, error)
}

func (s *runtimeDeploymentService) HermesWebRolloutReady(ctx context.Context, namespace, name string) (bool, error) {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == desired && d.Status.Replicas == desired && d.Status.AvailableReplicas == desired, nil
}

func (s *runtimeDeploymentService) HermesWebEnvironment(ctx context.Context, namespace, name string) (map[string]string, error) {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if d.Labels["clawmanager.io/runtime-type"] != "hermes" {
		return nil, fmt.Errorf("Hermes Web configuration requires a Hermes deployment")
	}
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "runtime" {
			return s.hermesWebEnvironment(ctx, namespace, c)
		}
	}
	return nil, fmt.Errorf("Hermes runtime container is missing")
}

func (s *runtimeDeploymentService) hermesWebEnvironment(ctx context.Context, namespace string, container corev1.Container) (map[string]string, error) {
	values := map[string]string{}
	for _, e := range container.Env {
		if e.Name == "CLAWMANAGER_CONTROL_UI_ORIGIN" || e.Name == "CLAWMANAGER_TRUSTED_PROXY_CIDRS" || e.Name == "CLAWMANAGER_BACKEND_URL" {
			if e.ValueFrom != nil {
				return nil, fmt.Errorf("Hermes Web trust %s uses an unresolved valueFrom", e.Name)
			}
			values[e.Name] = strings.TrimSpace(e.Value)
		}
	}
	origin := values["CLAWMANAGER_CONTROL_UI_ORIGIN"]
	if origin == "" {
		origin = values["CLAWMANAGER_BACKEND_URL"]
	}
	u, err := url.Parse(origin)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, fmt.Errorf("invalid_control_ui_origin: expected an explicit tenant Service origin")
	}
	suffix := "." + namespace + ".svc.cluster.local"
	if !strings.HasSuffix(u.Hostname(), suffix) {
		return nil, fmt.Errorf("invalid_control_ui_origin: Service must belong to runtime namespace")
	}
	serviceName := strings.TrimSuffix(u.Hostname(), suffix)
	if serviceName == "" || strings.Contains(serviceName, ".") {
		return nil, fmt.Errorf("invalid_control_ui_origin: invalid Service name")
	}
	proxies := values["CLAWMANAGER_TRUSTED_PROXY_CIDRS"]
	// Discovery values are refreshed, not treated as operator-pinned policy.
	for _, e := range container.Env {
		if e.Name == "CLAWMANAGER_HERMES_PROXY_SOURCE" && e.Value == "service-endpoints" {
			proxies = ""
		}
	}
	source := "explicit"
	if proxies == "" {
		endpoints, err := s.client.CoreV1().Endpoints(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("resolve Hermes control-plane endpoints: %w", err)
		}
		unique := map[string]bool{}
		for _, subset := range endpoints.Subsets {
			for _, address := range subset.Addresses {
				if address.TargetRef == nil || address.TargetRef.Kind != "Pod" || (address.TargetRef.Namespace != "" && address.TargetRef.Namespace != namespace) {
					continue
				}
				if ip := net.ParseIP(address.IP); ip != nil {
					unique[ip.String()] = true
				}
			}
		}
		var addresses []string
		for address := range unique {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		proxies, source = strings.Join(addresses, ","), "service-endpoints"
	}
	if proxies == "" {
		return nil, fmt.Errorf("invalid_trusted_proxies: no ready control-plane Pod endpoints")
	}
	for _, value := range strings.Split(proxies, ",") {
		value = strings.TrimSpace(value)
		if net.ParseIP(value) != nil {
			continue
		}
		_, subnet, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid_trusted_proxies: invalid address")
		}
		ones, _ := subnet.Mask.Size()
		if ones == 0 {
			return nil, fmt.Errorf("invalid_trusted_proxies: unbounded network is forbidden")
		}
	}
	return map[string]string{"CLAWMANAGER_CONTROL_UI_ORIGIN": origin, "CLAWMANAGER_TRUSTED_PROXY_CIDRS": proxies, "CLAWMANAGER_HERMES_PROXY_SOURCE": source, "CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED": "true"}, nil
}

func (s *runtimeDeploymentService) RolloutHermesWebImage(ctx context.Context, namespace, name, image string, unavailable, surge int) error {
	return s.rolloutImage(ctx, namespace, name, image, "", unavailable, surge, true)
}

func (s *runtimeDeploymentService) HermesGatewayEnvironment(ctx context.Context, namespace, name string) (map[string]string, error) {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name != "runtime" {
			continue
		}
		for _, e := range c.Env {
			if e.Name == "CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED" && e.Value == "true" {
				if d.Labels["clawmanager.io/runtime-type"] != "hermes" {
					return nil, fmt.Errorf("Hermes Web deployment identity mismatch")
				}
				return s.hermesWebEnvironment(ctx, namespace, c)
			}
		}
	}
	return nil, nil
}
