package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/repository"
)

type fakeGatewayBudgetService struct {
	reserveCost   float64
	finalizeCost  float64
	finalizeErr   error
	finalizeCalls int
	releases      int
	reserveErr    error
}

func TestImageBudgetHandoffTransfersCleanupOwnership(t *testing.T) {
	for _, armFails := range []bool{false, true} {
		store := &fakeGatewayBudgetService{}
		controller := NewGatewayBudgetController(store, 12)
		if err := controller.Reserve(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		err := controller.HandoffImage(func(lease *GatewayBudgetLease) error {
			if lease == nil || lease.GatewayKeyID != 12 || lease.ReservationID == "" {
				t.Fatal("lease lost")
			}
			if armFails {
				return errors.New("transaction rolled back")
			}
			return nil
		})
		if (err != nil) != armFails {
			t.Fatal(err)
		}
		if err := controller.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		wantReleases := 0
		if armFails {
			wantReleases = 1
		}
		if store.releases != wantReleases || store.finalizeCalls != 0 {
			t.Fatal("request cleanup stole durable job settlement", store)
		}
	}
	var noKey *GatewayBudgetController
	if err := noKey.HandoffImage(func(lease *GatewayBudgetLease) error {
		if lease != nil {
			t.Fatal("JWT call has lease")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (s *fakeGatewayBudgetService) Reserve(_ context.Context, keyID int, cost float64) (*GatewayBudgetLease, error) {
	s.reserveCost = cost
	if s.reserveErr != nil {
		return nil, s.reserveErr
	}
	return &GatewayBudgetLease{ReservationID: "reservation", GatewayKeyID: keyID, ReservedCost: cost}, nil
}
func (s *fakeGatewayBudgetService) Finalize(_ context.Context, _ *GatewayBudgetLease, cost float64) error {
	s.finalizeCalls++
	s.finalizeCost = cost
	err := s.finalizeErr
	s.finalizeErr = nil
	return err
}
func (s *fakeGatewayBudgetService) Release(_ context.Context, _ *GatewayBudgetLease) error {
	s.releases++
	return nil
}
func (s *fakeGatewayBudgetService) Usage(context.Context, int) (*repository.GatewayKeyBudgetUsage, error) {
	return nil, nil
}

func TestGatewayBudgetControllerFinalizesWithoutRelease(t *testing.T) {
	store := &fakeGatewayBudgetService{}
	controller := NewGatewayBudgetController(store, 12)
	if err := controller.Reserve(context.Background(), 1.25); err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if err := controller.Finalize(context.Background(), 0.75); err != nil {
		t.Fatalf("finalize budget: %v", err)
	}
	if err := controller.Release(context.Background()); err != nil {
		t.Fatalf("release finalized budget: %v", err)
	}
	if store.reserveCost != 1.25 || store.finalizeCost != 0.75 || store.releases != 0 {
		t.Fatalf("unexpected lifecycle: %#v", store)
	}
}

func TestGatewayBudgetControllerReleasesFailedRequest(t *testing.T) {
	store := &fakeGatewayBudgetService{}
	controller := NewGatewayBudgetController(store, 12)
	if err := controller.Reserve(context.Background(), 1); err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if err := controller.Release(context.Background()); err != nil {
		t.Fatalf("release budget: %v", err)
	}
	if store.releases != 1 {
		t.Fatalf("release count = %d, want 1", store.releases)
	}
}

func TestGatewayBudgetControllerPropagatesBudgetExceeded(t *testing.T) {
	store := &fakeGatewayBudgetService{reserveErr: ErrGatewayKeyBudgetExceeded}
	controller := NewGatewayBudgetController(store, 12)
	if err := controller.Reserve(context.Background(), 1); !errors.Is(err, ErrGatewayKeyBudgetExceeded) {
		t.Fatalf("reserve error = %v, want budget exceeded", err)
	}
}

func TestGatewayBudgetControllerRejectsForeignCurrency(t *testing.T) {
	store := &fakeGatewayBudgetService{}
	controller := NewGatewayBudgetController(store, 12)
	if err := controller.Reserve(context.Background(), 5, "CNY"); !errors.Is(err, ErrGatewayBudgetCurrencyMismatch) || store.reserveCost != 0 {
		t.Fatalf("foreign currency reserved: %v", err)
	}
	if err := controller.Reserve(context.Background(), 1, "usd"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Finalize(context.Background(), 5, "CNY"); !errors.Is(err, ErrGatewayBudgetCurrencyMismatch) || store.finalizeCalls != 0 {
		t.Fatalf("foreign currency settled: %v", err)
	}
}

func TestGatewayBudgetControllerRetriesCompletedSettlementDuringRelease(t *testing.T) {
	store := &fakeGatewayBudgetService{finalizeErr: errors.New("temporary database failure")}
	controller := NewGatewayBudgetController(store, 12)
	if err := controller.Reserve(context.Background(), 1); err != nil {
		t.Fatalf("reserve budget: %v", err)
	}
	if err := controller.Finalize(context.Background(), 0.5); err == nil {
		t.Fatal("expected first settlement attempt to fail")
	}
	if err := controller.Release(context.Background()); err != nil {
		t.Fatalf("retry settlement during release: %v", err)
	}
	if store.finalizeCalls != 2 || store.finalizeCost != 0.5 {
		t.Fatalf("finalize calls/cost = %d/%f, want 2/0.5", store.finalizeCalls, store.finalizeCost)
	}
	if store.releases != 0 {
		t.Fatalf("completed reservation was released %d times", store.releases)
	}
}
