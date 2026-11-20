package repository

import (
	"fmt"
	"testing"

	"clawreef/internal/models"
)

func TestRuntimeTaskInspectionScopeAndBounds(t *testing.T) {
	scope := models.GatewayTaskIdentity{InstanceID: 8, UserID: 7, Generation: 3}
	request := []models.GatewayTaskIdentity{{InstanceID: 999, UserID: 999, Generation: 2, SessionID: "s", TaskID: "t"}, {Generation: 3, SessionID: "s", TaskID: "t"}}
	owned, err := PrepareRuntimeTaskInspection(scope, request)
	if err != nil || len(owned) != 2 {
		t.Fatal(err)
	}
	for _, task := range owned {
		if task.InstanceID != 8 || task.UserID != 7 {
			t.Fatal("untrusted task scope")
		}
	}
	if request[0].InstanceID != 999 {
		t.Fatal("mutated caller input")
	}
	for _, batch := range [][]models.GatewayTaskIdentity{nil, {}, {request[0], request[0]}, {{Generation: 4, SessionID: "s", TaskID: "t"}}, {{Generation: -1, SessionID: "s", TaskID: "t"}}, {{Generation: 2, SessionID: "", TaskID: "t"}}} {
		if _, err := PrepareRuntimeTaskInspection(scope, batch); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	batch := make([]models.GatewayTaskIdentity, MaxRuntimeTaskInspection+1)
	for i := range batch {
		batch[i] = models.GatewayTaskIdentity{Generation: 2, SessionID: "s", TaskID: fmt.Sprintf("t%d", i)}
	}
	if _, err := PrepareRuntimeTaskInspection(scope, batch[:MaxRuntimeTaskInspection]); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRuntimeTaskInspection(scope, batch); err == nil {
		t.Fatal("oversized batch accepted")
	}
	scope.UserID = 0
	if _, err := PrepareRuntimeTaskInspection(scope, request); err == nil {
		t.Fatal("unbound owner accepted")
	}
}
