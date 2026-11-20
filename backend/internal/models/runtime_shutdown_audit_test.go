package models

import (
	"strings"
	"testing"
)

func TestRuntimeShutdownObservationValidation(t *testing.T) {
	good := RuntimeShutdownObservation{Generation: 7, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_confirmed_release_observed"}
	for _, outcome := range []string{"cleanup_unconfirmed", "cleanup_confirmed_release_unconfirmed", "cleanup_confirmed_release_observed"} {
		v := good
		v.Outcome = outcome
		if v.Validate() != nil {
			t.Fatal(outcome)
		}
	}
	for _, mutate := range []func(*RuntimeShutdownObservation){func(v *RuntimeShutdownObservation) { v.Generation = -1 }, func(v *RuntimeShutdownObservation) { v.Generation = 2147483648 }, func(v *RuntimeShutdownObservation) { v.ClaimID = "" }, func(v *RuntimeShutdownObservation) { v.ClaimID = strings.Repeat("A", 64) }, func(v *RuntimeShutdownObservation) { v.ClaimID = strings.Repeat("a", 65) }, func(v *RuntimeShutdownObservation) { v.Outcome = "released" }, func(v *RuntimeShutdownObservation) { v.Outcome = "cleanup_unconfirmed\nsecret" }} {
		v := good
		mutate(&v)
		if v.Validate() == nil {
			t.Fatalf("accepted %#v", v)
		}
	}
}
