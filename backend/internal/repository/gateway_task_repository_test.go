package repository

import (
	"clawreef/internal/models"
	"strings"
	"testing"
)

func TestGatewayTaskIdentityIsScopedAndUnambiguous(t *testing.T) {
	base := models.GatewayTaskIdentity{InstanceID: 1, UserID: 2, Generation: 3, SessionID: "session", TaskID: "turn"}
	key, err := GatewayTaskKey(base)
	if err != nil || len(key) != 64 {
		t.Fatalf("%s %v", key, err)
	}
	for _, change := range []func(*models.GatewayTaskIdentity){
		func(x *models.GatewayTaskIdentity) { x.InstanceID++ }, func(x *models.GatewayTaskIdentity) { x.UserID++ },
		func(x *models.GatewayTaskIdentity) { x.Generation++ }, func(x *models.GatewayTaskIdentity) { x.SessionID += "x" }, func(x *models.GatewayTaskIdentity) { x.TaskID += "x" },
	} {
		other := base
		change(&other)
		got, err := GatewayTaskKey(other)
		if err != nil || got == key {
			t.Fatalf("collision: %+v %v", other, err)
		}
	}
	for _, id := range []string{"", " main", "a\n", strings.Repeat("x", 192), string([]byte{0xff})} {
		other := base
		other.TaskID = id
		if _, err := GatewayTaskKey(other); err == nil {
			t.Fatalf("accepted invalid task %q", id)
		}
	}
}
