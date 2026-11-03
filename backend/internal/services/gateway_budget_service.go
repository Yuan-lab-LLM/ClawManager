package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"clawreef/internal/repository"
)

var ErrGatewayKeyBudgetExceeded = errors.New("gateway key budget exceeded")

// No implicit FX conversions: governed budgets and their ledger are USD only.
const GatewayBudgetCurrency = "USD"

var ErrGatewayBudgetCurrencyMismatch = errors.New("gateway budget only supports USD models")

type GatewayBudgetLease struct {
	ReservationID string
	GatewayKeyID  int
	ReservedCost  float64
}

type GatewayBudgetService interface {
	Reserve(context.Context, int, float64) (*GatewayBudgetLease, error)
	Finalize(context.Context, *GatewayBudgetLease, float64) error
	Release(context.Context, *GatewayBudgetLease) error
	Usage(context.Context, int) (*repository.GatewayKeyBudgetUsage, error)
}

type gatewayBudgetService struct {
	repo repository.GatewayKeyRepository
}

func NewGatewayBudgetService(repo repository.GatewayKeyRepository) GatewayBudgetService {
	return &gatewayBudgetService{repo: repo}
}

func (s *gatewayBudgetService) Reserve(ctx context.Context, keyID int, estimatedCost float64) (*GatewayBudgetLease, error) {
	if s == nil || s.repo == nil || keyID <= 0 {
		return nil, nil
	}
	reservationID, err := newGatewayBudgetReservationID()
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReserveBudget(ctx, keyID, reservationID, estimatedCost, time.Now().UTC().Add(10*time.Minute)); err != nil {
		if errors.Is(err, repository.ErrGatewayKeyBudgetExceeded) {
			return nil, ErrGatewayKeyBudgetExceeded
		}
		return nil, err
	}
	return &GatewayBudgetLease{ReservationID: reservationID, GatewayKeyID: keyID, ReservedCost: estimatedCost}, nil
}

func (s *gatewayBudgetService) Finalize(ctx context.Context, lease *GatewayBudgetLease, actualCost float64) error {
	if s == nil || s.repo == nil || lease == nil {
		return nil
	}
	return s.repo.FinalizeBudget(ctx, lease.ReservationID, lease.GatewayKeyID, actualCost)
}

func (s *gatewayBudgetService) Release(ctx context.Context, lease *GatewayBudgetLease) error {
	if s == nil || s.repo == nil || lease == nil {
		return nil
	}
	return s.repo.ReleaseBudget(ctx, lease.ReservationID)
}

func (s *gatewayBudgetService) Usage(ctx context.Context, keyID int) (*repository.GatewayKeyBudgetUsage, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("gateway budget repository is not configured")
	}
	return s.repo.GetBudgetUsage(ctx, keyID)
}

func newGatewayBudgetReservationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "gbr_" + hex.EncodeToString(raw), nil
}

// GatewayBudgetController carries one request's durable budget reservation
// through routing, provider failover and handler cleanup.
type GatewayBudgetController struct {
	service    GatewayBudgetService
	keyID      int
	lease      *GatewayBudgetLease
	reserved   bool
	completed  bool
	actualCost float64
	finalized  bool
	mu         sync.Mutex
}

func NewGatewayBudgetController(service GatewayBudgetService, keyID int) *GatewayBudgetController {
	if service == nil || keyID <= 0 {
		return nil
	}
	return &GatewayBudgetController{service: service, keyID: keyID}
}

func (c *GatewayBudgetController) Reserve(ctx context.Context, estimatedCost float64, currency ...string) error {
	if c == nil {
		return nil
	}
	if len(currency) > 0 && strings.ToUpper(strings.TrimSpace(currency[0])) != GatewayBudgetCurrency {
		return ErrGatewayBudgetCurrencyMismatch
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reserved {
		return nil
	}
	lease, err := c.service.Reserve(ctx, c.keyID, estimatedCost)
	if err != nil {
		return err
	}
	c.lease = lease
	c.reserved = true
	return nil
}

func (c *GatewayBudgetController) Finalize(ctx context.Context, actualCost float64, currency ...string) error {
	if c == nil {
		return nil
	}
	if len(currency) > 0 && strings.ToUpper(strings.TrimSpace(currency[0])) != GatewayBudgetCurrency {
		return ErrGatewayBudgetCurrencyMismatch
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalized || !c.reserved {
		return nil
	}
	c.completed = true
	c.actualCost = actualCost
	if err := c.service.Finalize(ctx, c.lease, actualCost); err != nil {
		return err
	}
	c.finalized = true
	return nil
}

func (c *GatewayBudgetController) Release(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalized || !c.reserved {
		return nil
	}
	if c.completed {
		if err := c.service.Finalize(ctx, c.lease, c.actualCost); err != nil {
			return err
		}
		c.finalized = true
		return nil
	}
	c.finalized = true
	return c.service.Release(ctx, c.lease)
}

// Transfers settlement responsibility to a durable image job before dispatch.
// The callback atomically records the job and holds the reservation in MySQL.
func (c *GatewayBudgetController) HandoffImage(arm func(*GatewayBudgetLease) error) error {
	if c == nil {
		return arm(nil)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.reserved || c.finalized || c.completed || c.lease == nil {
		return fmt.Errorf("image budget reservation unavailable")
	}
	if err := arm(c.lease); err != nil {
		return err
	}
	c.finalized = true
	return nil
}
