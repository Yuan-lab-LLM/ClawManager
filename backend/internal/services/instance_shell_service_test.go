package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestInstanceShellServiceRejectsOpenCodeLiteTUI(t *testing.T) {
	service := &InstanceShellService{}
	instance := &models.Instance{
		ID:           42,
		Type:         RuntimeTypeOpenCode,
		RuntimeType:  RuntimeBackendGateway,
		InstanceMode: InstanceModeLite,
	}

	_, _, _, err := service.execTarget(context.Background(), instance)
	if err == nil || !strings.Contains(err.Error(), "only available for shell instances") {
		t.Fatalf("execTarget() error = %v, want shell-only rejection", err)
	}
}
