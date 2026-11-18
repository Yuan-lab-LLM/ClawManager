package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	"strings"
)

type stoppedRuntimeAssignmentProvider interface {
	StoppedRuntimeAssignment(context.Context, int) (*k8s.RuntimeAssignment, error)
}

// A released SQL binding is not source evidence. Use the persisted confirmed
// stop's owner/generation/Pod UID, the exact historical Pod, and that Pod's
// immutable image. A newer ordinary pool cannot stand in for an older instance.
func (s *RuntimeUpgradeService) fillStoppedSourceVersions(ctx context.Context, candidates []runtimeUpgradeCandidate, sources, live map[string]string, pods []models.RuntimePod, inspect func(context.Context, string, map[string]string, bool) (*OpenClawTargetClassification, error)) error {
	cache := map[string]*OpenClawTargetClassification{}
	for index := range candidates {
		c := &candidates[index]
		if c.InstanceStatus != "stopped" || c.RuntimePodID != nil || strings.TrimSpace(c.SourceVersion) != "" {
			continue
		}
		reader, ok := s.deployments.(stoppedRuntimeAssignmentProvider)
		if !ok {
			return fmt.Errorf("instance %d has no persistent stopped source evidence", c.InstanceID)
		}
		a, err := reader.StoppedRuntimeAssignment(ctx, c.InstanceID)
		if err != nil {
			return fmt.Errorf("instance %d stopped source evidence: %w", c.InstanceID, err)
		}
		if a == nil || a.InstanceID != c.InstanceID || a.UserID != c.UserID || a.Generation != c.RuntimeGeneration || a.RuntimeType != RuntimeTypeOpenClaw || a.PodUID == "" || a.GatewayID == "" {
			return fmt.Errorf("instance %d stopped source identity changed", c.InstanceID)
		}
		var source *models.RuntimePod
		for i := range pods {
			if stringValue(pods[i].PodUID) == a.PodUID {
				if source != nil {
					return fmt.Errorf("stopped source Pod UID is ambiguous")
				}
				source = &pods[i]
			}
		}
		if source == nil || source.RuntimeType != RuntimeTypeOpenClaw || source.Namespace == "" || source.DeploymentName == "" || stringValue(source.AgentEndpoint) != a.Endpoint {
			return fmt.Errorf("instance %d stopped source Pod is not verifiable", c.InstanceID)
		}
		image, err := immutableRuntimeImage(source.ImageRef, stringValue(source.ImageDigest))
		if err != nil {
			return err
		}
		key := source.Namespace + "/" + source.DeploymentName
		if live[key] != image {
			return fmt.Errorf("instance %d stopped source image differs from the live rollback pool", c.InstanceID)
		}
		metadata := cache[image]
		if metadata == nil {
			metadata, err = inspect(ctx, image, map[string]string{key: image}, false)
			if err != nil {
				return fmt.Errorf("inspect stopped source image: %w", err)
			}
			cache[image] = metadata
		}
		if metadata == nil || metadata.ImageDigest != imageDigestFromReference(image) {
			return fmt.Errorf("stopped source image digest evidence is inconsistent")
		}
		version := strings.TrimSpace(metadata.RuntimeVersion)
		if _, ok := parseOpenClawNumericVersion(version); !ok {
			return fmt.Errorf("stopped source image has no verified OpenClaw version")
		}
		if reported := resolvedSourceOpenClawVersion(*source); reported != "" && reported != version {
			return fmt.Errorf("stopped source image version conflicts with its Pod metadata")
		}
		c.SourceVersion = version
		c.SourceVersionImages = []string{image}
		c.SourcePodUID = a.PodUID
		c.SourceDeployment = source.DeploymentName
		c.SourceImageDigest = metadata.ImageDigest
		id := source.ID
		c.RuntimePodID = &id
		c.GatewayID = a.GatewayID
		c.Generation = a.Generation
		c.BindingState = "stopped"
		c.AgentEndpoint = a.Endpoint
		c.Capabilities = source.Capabilities()
		sources[key] = image
	}
	return nil
}
