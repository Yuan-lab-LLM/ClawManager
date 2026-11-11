package services

import (
	"clawreef/internal/models"
	"strings"
	"testing"
)

func TestStoppedSourceProofRequiresEveryIdentityEcho(t *testing.T) {
	want := stoppedSourceProofScope{InstanceID: 155, UserID: 1, Generation: 7, RuntimeType: "opencode", PodUID: "source", Slot: "slot-0", Nonce: strings.Repeat("a", 64)}
	for _, change := range []string{"none", "nonce", "generation", "instance", "user", "runtime", "pod", "slot", "observer", "digest", "confirmed"} {
		t.Run(change, func(t *testing.T) {
			got := stoppedSourceProof{stoppedSourceProofScope: want, Confirmed: true, ObserverPodUID: "observer", SourceRecordDigest: strings.Repeat("b", 64)}
			switch change {
			case "nonce":
				got.Nonce = strings.Repeat("c", 64)
			case "generation":
				got.Generation++
			case "instance":
				got.InstanceID++
			case "user":
				got.UserID++
			case "runtime":
				got.RuntimeType = "openclaw"
			case "pod":
				got.PodUID = "different"
			case "slot":
				got.Slot = "slot-1"
			case "observer":
				got.ObserverPodUID = "other"
			case "digest":
				got.SourceRecordDigest = "bad"
			case "confirmed":
				got.Confirmed = false
			}
			err := got.validate(want, "observer")
			if (err == nil) != (change == "none") {
				t.Fatalf("proof validation: %v", err)
			}
		})
	}
}

func TestStoppedRecoveryRequiresPendingExactSuccessor(t *testing.T) {
	for _, change := range []string{"none", "state", "generation", "gateway", "binding-id", "owner"} {
		t.Run(change, func(t *testing.T) {
			instance := &models.Instance{ID: 155, UserID: 1, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 8}
			binding := &models.InstanceRuntimeBinding{InstanceID: 155, RuntimeType: "opencode", Generation: 8, State: "creating", GatewayID: "pending-155-8", RuntimePodID: 153}
			req := StoppedSourceRecoveryRequest{SuccessorGeneration: 8, SourcePodUID: "old", ObserverRuntimePodID: 153}
			switch change {
			case "state":
				binding.State = "running"
			case "generation":
				instance.RuntimeGeneration++
			case "gateway":
				binding.GatewayID = "gw-155-8"
			case "binding-id":
				binding.RuntimePodID = 0
			case "owner":
				instance.UserID = 0
			}
			err := validateStoppedRecoveryReservation(instance, binding, req)
			if (err == nil) != (change == "none") {
				t.Fatalf("reservation validation: %v", err)
			}
		})
	}
}
