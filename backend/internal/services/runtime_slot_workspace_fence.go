package services

import (
	"context"
	"fmt"

	"clawreef/internal/runtimeauth"
)

// Configure before exposing the client. The callback must atomically fence new
// backend file writes and reject unresolved writes for the pinned generation;
// an elapsed deadline or missing process cannot resolve an orphan write intent.
func ConfigureSlotWorkspaceWriteFence(client RuntimeAgentClient, fence func(context.Context, runtimeauth.Identity) error) error {
	c, ok := client.(*slotRuntimeAgentClient)
	if !ok || fence == nil {
		return fmt.Errorf("invalid slot workspace write fence")
	}
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.workspaceWriteFence != nil {
		return fmt.Errorf("slot workspace write fence already configured")
	}
	c.workspaceWriteFence = fence
	return nil
}

func (c *slotRuntimeAgentClient) requireWorkspaceWriteFence(ctx context.Context, identity runtimeauth.Identity) error {
	c.admissionMu.RLock()
	fence := c.workspaceWriteFence
	c.admissionMu.RUnlock()
	if fence == nil {
		return fmt.Errorf("NFS lifecycle requires a durable management workspace write fence")
	}
	return fence(ctx, identity)
}

func ConfigureSlotWorkspaceWriteActivation(client RuntimeAgentClient, activate func(context.Context, runtimeauth.Identity) error) error {
	c, ok := client.(*slotRuntimeAgentClient)
	if !ok || activate == nil {
		return fmt.Errorf("invalid slot workspace activation")
	}
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.workspaceWriteActivation != nil {
		return fmt.Errorf("slot workspace activation already configured")
	}
	c.workspaceWriteActivation = activate
	return nil
}

func (c *slotRuntimeAgentClient) requireWorkspaceWriteActivation(ctx context.Context, identity runtimeauth.Identity) error {
	c.admissionMu.RLock()
	activate := c.workspaceWriteActivation
	c.admissionMu.RUnlock()
	if activate == nil {
		return fmt.Errorf("NFS startup requires durable management workspace activation")
	}
	return activate(ctx, identity)
}

func ConfigureSlotExternalWriterCleanup(client RuntimeAgentClient, cleanup func(context.Context, runtimeauth.Identity) error) error {
	c, ok := client.(*slotRuntimeAgentClient)
	if !ok || cleanup == nil {
		return fmt.Errorf("invalid external Runtime writer cleanup")
	}
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.workspaceExternalStop != nil {
		return fmt.Errorf("external Runtime writer cleanup already configured")
	}
	c.workspaceExternalStop = cleanup
	return nil
}
func (c *slotRuntimeAgentClient) requireExternalWriterCleanup(ctx context.Context, identity runtimeauth.Identity) error {
	c.admissionMu.RLock()
	cleanup := c.workspaceExternalStop
	c.admissionMu.RUnlock()
	if cleanup == nil {
		return fmt.Errorf("NFS lifecycle requires positive external writer termination")
	}
	return cleanup(ctx, identity)
}
