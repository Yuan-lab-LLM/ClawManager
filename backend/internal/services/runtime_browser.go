package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
)

type RuntimeBrowserConfigSource interface {
	GetContext(context.Context, int) (*models.InstanceBrowserWorker, error)
}

type RuntimeBrowserAdapter struct {
	instances runtimeSecurityInstanceSource
	configs   RuntimeBrowserConfigSource
	worker    *BrowserWorkerService
}

// BoundRuntimeBrowser holds a fixed management-only target/credential snapshot.
// It never accepts a business URL or exposes the worker key in its target URL.
type BoundRuntimeBrowser struct {
	adapter          *RuntimeBrowserAdapter
	identity         runtimeauth.Identity
	workerGeneration int
	target           url.URL
	credential       string
	workerPodUID     string
}

var errRuntimeBrowser = errors.New("managed browser unavailable")

func NewRuntimeBrowserAdapter(instances runtimeSecurityInstanceSource, configs RuntimeBrowserConfigSource, worker *BrowserWorkerService) (*RuntimeBrowserAdapter, error) {
	if instances == nil || configs == nil || worker == nil {
		return nil, errRuntimeBrowser
	}
	return &RuntimeBrowserAdapter{instances: instances, configs: configs, worker: worker}, nil
}

type runtimeBrowserSnapshot struct {
	generation int
	credential string
	host       string
	podUID     string
}

func (a *RuntimeBrowserAdapter) current(ctx context.Context, id runtimeauth.Identity) (runtimeBrowserSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return runtimeBrowserSnapshot{}, err
	}
	instance, err := a.instances.GetByIDContext(ctx, id.InstanceID)
	if err != nil || instance == nil || instance.ID != id.InstanceID || instance.UserID != id.UserID || instance.RuntimeGeneration != id.Generation || instance.RuntimeSecurityProfile != RuntimeSecurityProfile || instance.Type != RuntimeTypeOpenClaw || instance.InstanceMode != InstanceModeLite || instance.RuntimeType != "gateway" || instance.Status != "running" || id.InstanceID < 1 || id.UserID < 1 || id.Generation < 1 || id.PodUID == "" {
		return runtimeBrowserSnapshot{}, errRuntimeBrowser
	}
	config, err := a.configs.GetContext(ctx, id.InstanceID)
	if err != nil || config == nil || config.InstanceID != id.InstanceID || !config.Enabled || config.Generation != config.ObservedGeneration || config.Status != models.BrowserWorkerStatusReady {
		return runtimeBrowserSnapshot{}, errRuntimeBrowser
	}
	current, err := a.worker.protectedBrowserIdentity(ctx, instance)
	if err != nil || current != id {
		return runtimeBrowserSnapshot{}, errRuntimeBrowser
	}
	pod, err := a.worker.protectedNetworkSnapshot(ctx, instance)
	if err != nil {
		return runtimeBrowserSnapshot{}, errRuntimeBrowser
	}
	credential, err := a.worker.AuthToken(ctx, id.InstanceID)
	if err != nil || len(credential) < 32 || len(credential) > 4096 || ctx.Err() != nil {
		return runtimeBrowserSnapshot{}, errRuntimeBrowser
	}
	return runtimeBrowserSnapshot{config.Generation, credential, net.JoinHostPort(pod.Status.PodIP, "9222"), string(pod.UID)}, nil
}

// Resolve is called only after services-capability verification. Check must be
// called with that capability's revalidation for every long-lived operation.
func (a *RuntimeBrowserAdapter) Resolve(ctx context.Context, id runtimeauth.Identity) (*BoundRuntimeBrowser, error) {
	snapshot, err := a.current(ctx, id)
	if err != nil {
		return nil, err
	}
	return &BoundRuntimeBrowser{adapter: a, identity: id, workerGeneration: snapshot.generation, credential: snapshot.credential, workerPodUID: snapshot.podUID, target: url.URL{Scheme: "http", Host: snapshot.host}}, nil
}

func (b *BoundRuntimeBrowser) TargetURL() *url.URL { copy := b.target; return &copy }
func (b *BoundRuntimeBrowser) Authenticate(request *http.Request) {
	request.SetBasicAuth("openclaw", b.credential)
}
func (b *BoundRuntimeBrowser) Check(ctx context.Context) error {
	snapshot, err := b.adapter.current(ctx, b.identity)
	if err != nil || snapshot.generation != b.workerGeneration || snapshot.host != b.target.Host || snapshot.podUID != b.workerPodUID || subtle.ConstantTimeCompare([]byte(snapshot.credential), []byte(b.credential)) != 1 {
		return errRuntimeBrowser
	}
	return nil
}
