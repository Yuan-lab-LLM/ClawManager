package models

import (
	"strings"
	"testing"
)

func TestShutdownDeliveryWireIDAndOriginalOwnerBinding(t *testing.T) {
	e := RuntimeShutdownEvent{InstanceID: 8, OwnerUserID: 7, Generation: 3, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_confirmed_release_observed"}
	d := RuntimeShutdownDelivery{ID: e.DeliveryID(), Event: e}
	if d.ID != "be03a0044a9280ba19845b5200e99e638cc5dffec72991503e2d1332aa9c7825" || d.Validate() != nil {
		t.Fatal("runtime wire contract changed")
	}
	d.Event.OwnerUserID = 9
	if d.Validate() == nil {
		t.Fatal("owner edit preserved original identity")
	}
	d.Event = e
	d.Event.Generation++
	if d.Validate() == nil {
		t.Fatal("generation edit preserved original identity")
	}
}
