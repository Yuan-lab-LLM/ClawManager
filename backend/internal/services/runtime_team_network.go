package services

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

func runtimeTeamID(instance *models.Instance) (int, error) {
	if instance == nil {
		return 0, errManagedRedis
	}
	if instance.EnvironmentOverridesJSON == nil {
		return 0, nil
	}
	var env map[string]string
	if len(*instance.EnvironmentOverridesJSON) > 1<<20 || json.Unmarshal([]byte(*instance.EnvironmentOverridesJSON), &env) != nil {
		return 0, errManagedRedis
	}
	if env["CLAWMANAGER_TEAM_ID"] == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(env["CLAWMANAGER_TEAM_ID"])
	if err != nil || id < 1 {
		return 0, errManagedRedis
	}
	return id, nil
}

func (a *RuntimeSecurityAuthority) checkTeamNetwork(ctx context.Context, instance *models.Instance) error {
	id, err := runtimeTeamID(instance)
	if err != nil {
		return errManagedRedis
	}
	if a.teamNetwork != nil {
		return a.teamNetwork(ctx, instance)
	}
	if id != 0 {
		return errManagedRedis
	}
	return ctx.Err()
}

type managedTeamAssociationSource interface {
	GetRuntimeMemberByInstanceContext(context.Context, int) (*models.TeamMember, error)
}

// ConfigureManagedTeamRedisNetwork must run before publishing the authority.
// Historical master passwords cannot be made secret by editing their copies.
// Protected Teams therefore require an upstream unreachable by business egress,
// before execution or any capability is authorized, even without a Redis call.
func ConfigureManagedTeamRedisNetwork(m *ManagedTeamRedis, a *RuntimeSecurityAuthority) error {
	if m == nil || a == nil || a.kube == nil || m.networkProtection == nil || m.protectedTarget != nil || a.teamNetwork != nil || len(validation.IsDNS1123Label(a.namespace)) != 0 {
		return errManagedRedis
	}
	associations, ok := m.teams.(managedTeamAssociationSource)
	if !ok {
		return errManagedRedis
	}
	m.protectedTarget = func(ctx context.Context, bus *redisBus) (string, error) {
		return managedRedisProtectedTarget(ctx, a.kube, a.namespace, bus, m.networkProtection)
	}
	a.teamNetwork = func(ctx context.Context, instance *models.Instance) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		// Team membership is an independent SQL relation. Removing or editing an
		// environment override cannot turn an existing Team member into a bypass.
		member, err := associations.GetRuntimeMemberByInstanceContext(ctx, instance.ID)
		teamID, idErr := runtimeTeamID(instance)
		if err != nil || idErr != nil {
			return errManagedRedis
		}
		if member == nil {
			if teamID != 0 {
				return errManagedRedis
			}
			return ctx.Err()
		}
		if member.InstanceID == nil || *member.InstanceID != instance.ID || member.UserID != instance.UserID || member.TeamID != teamID {
			return errManagedRedis
		}
		team, err := m.authorizeTeam(ctx, instance, 0)
		if err != nil {
			return errManagedRedis
		}
		bus, err := m.resolve(ctx, team)
		if err != nil || bus == nil {
			return errManagedRedis
		}
		_, err = m.protectedTarget(ctx, bus)
		return err
	}
	return nil
}

// Resolve cluster Service names through the authenticated API, never arbitrary
// DNS. This binds admission and privileged dialing to the same literal target;
// DNS rebinding cannot make a later Redis dial use a public endpoint.
func managedRedisProtectedTarget(ctx context.Context, client kubernetes.Interface, namespace string, bus *redisBus, deny func(context.Context, netip.Addr) error) (string, error) {
	if ctx.Err() != nil || bus == nil || deny == nil {
		return "", errManagedRedis
	}
	host, port, err := net.SplitHostPort(bus.address)
	if err != nil {
		return "", errManagedRedis
	}
	address, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		labels := strings.Split(strings.TrimSuffix(strings.ToLower(host), "."), ".")
		name, ns := labels[0], namespace
		switch len(labels) {
		case 1:
		case 2:
			ns = labels[1]
		case 3:
			if labels[2] != "svc" {
				return "", errManagedRedis
			}
			ns = labels[1]
		case 5:
			if strings.Join(labels[2:], ".") != "svc.cluster.local" {
				return "", errManagedRedis
			}
			ns = labels[1]
		default:
			return "", errManagedRedis
		}
		if client == nil || len(validation.IsDNS1123Label(name)) != 0 || len(validation.IsDNS1123Label(ns)) != 0 {
			return "", errManagedRedis
		}
		service, err := client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil || service.DeletionTimestamp != nil || service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.ExternalName != "" || len(service.Spec.ExternalIPs) != 0 || len(service.Status.LoadBalancer.Ingress) != 0 {
			return "", errManagedRedis
		}
		address, err = netip.ParseAddr(service.Spec.ClusterIP)
		if err != nil {
			return "", errManagedRedis
		}
	}
	address = address.Unmap()
	if !address.IsValid() || address.Zone() != "" || deny(ctx, address) != nil || ctx.Err() != nil {
		return "", errManagedRedis
	}
	return net.JoinHostPort(address.String(), port), nil
}
