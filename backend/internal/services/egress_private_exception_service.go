package services

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const (
	egressPrivateExceptionCacheTTL       = 30 * time.Second
	egressPrivateExceptionRefreshTimeout = 2 * time.Second
	egressPrivateExceptionRetryDelay     = time.Second
)

var (
	errInvalidEgressPrivateScopeType  = errors.New("invalid egress private exception scope type")
	errInvalidEgressPrivateCIDR       = errors.New("invalid egress private exception cidr")
	errEgressPrivateCIDRNotPrivate    = errors.New("egress private exception cidr must be private or link-local")
	errInvalidEgressPrivatePort       = errors.New("invalid egress private exception port")
	errEgressPrivateExceptionNotFound = errors.New("egress private exception not found")
)

// EgressPrivateExceptionService manages private CIDR exceptions for the egress proxy.
type EgressPrivateExceptionService interface {
	List(scopeType string, scopeID *int) ([]models.EgressPrivateException, error)
	Create(req SaveEgressPrivateExceptionRequest) (*models.EgressPrivateException, error)
	Update(id int, req SaveEgressPrivateExceptionRequest) (*models.EgressPrivateException, error)
	Delete(id int) error
	Allows(instanceID, userID *int, ip netip.Addr, port int) bool
	SnapshotRules() []egresspolicy.PrivateExceptionRule
	SnapshotRulesContext(context.Context) []egresspolicy.PrivateExceptionRule
}

type SaveEgressPrivateExceptionRequest struct {
	ScopeType   string
	ScopeID     int
	CIDR        string
	Port        int
	Enabled     bool
	Description *string
	CreatedBy   *int
}

type egressInstanceLookup interface {
	GetByID(id int) (*models.Instance, error)
}

type egressUserLookup interface {
	GetByID(id int) (*models.User, error)
}

type egressPrivateExceptionService struct {
	repo         repository.EgressPrivateExceptionRepository
	instanceRepo egressInstanceLookup
	userRepo     egressUserLookup

	mu         sync.RWMutex
	rules      []egresspolicy.PrivateExceptionRule
	loadedAt   time.Time
	invalidate bool
	refreshing chan struct{}
	generation uint64
	retryAt    time.Time
}

// NewEgressPrivateExceptionService creates the service with an in-memory rule cache.
func NewEgressPrivateExceptionService(
	repo repository.EgressPrivateExceptionRepository,
	instanceRepo egressInstanceLookup,
	userRepo egressUserLookup,
) EgressPrivateExceptionService {
	svc := &egressPrivateExceptionService{
		repo:         repo,
		instanceRepo: instanceRepo,
		userRepo:     userRepo,
		invalidate:   true,
	}
	return svc
}

func (s *egressPrivateExceptionService) List(scopeType string, scopeID *int) ([]models.EgressPrivateException, error) {
	return s.repo.List(scopeType, scopeID)
}

func (s *egressPrivateExceptionService) Create(req SaveEgressPrivateExceptionRequest) (*models.EgressPrivateException, error) {
	item, err := s.buildValidated(req)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(item); err != nil {
		return nil, err
	}
	s.markStale()
	return item, nil
}

func (s *egressPrivateExceptionService) Update(id int, req SaveEgressPrivateExceptionRequest) (*models.EgressPrivateException, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errEgressPrivateExceptionNotFound
	}
	item, err := s.buildValidated(req)
	if err != nil {
		return nil, err
	}
	item.ID = existing.ID
	item.CreatedAt = existing.CreatedAt
	item.CreatedBy = existing.CreatedBy
	if err := s.repo.Update(item); err != nil {
		return nil, err
	}
	s.markStale()
	return item, nil
}

func (s *egressPrivateExceptionService) Delete(id int) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return errEgressPrivateExceptionNotFound
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.markStale()
	return nil
}

func (s *egressPrivateExceptionService) Allows(instanceID, userID *int, ip netip.Addr, port int) bool {
	return egresspolicy.MatchPrivateException(s.SnapshotRules(), instanceID, userID, ip, port)
}

func (s *egressPrivateExceptionService) SnapshotRules() []egresspolicy.PrivateExceptionRule {
	return s.SnapshotRulesContext(context.Background())
}

func (s *egressPrivateExceptionService) SnapshotRulesContext(ctx context.Context) []egresspolicy.PrivateExceptionRule {
	ctx, cancel := context.WithTimeout(ctx, egressPrivateExceptionRefreshTimeout)
	defer cancel()
	for ctx.Err() == nil {
		s.mu.Lock()
		if !s.invalidate && time.Since(s.loadedAt) < egressPrivateExceptionCacheTTL {
			out := append([]egresspolicy.PrivateExceptionRule(nil), s.rules...)
			s.mu.Unlock()
			return out
		}
		if s.refreshing == nil && time.Now().Before(s.retryAt) {
			s.mu.Unlock()
			return nil
		}
		if s.refreshing == nil {
			s.refreshing = make(chan struct{})
			s.rules = nil
			go s.refreshRules(s.refreshing, s.generation)
		}
		done := s.refreshing
		s.mu.Unlock()
		select {
		case <-done:
			continue
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func (s *egressPrivateExceptionService) refreshRules(done chan struct{}, generation uint64) {
	// A caller canceling its wait must not cancel the refresh for other callers
	// or poison the global cache. This one shared operation has its own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), egressPrivateExceptionRefreshTimeout)
	defer cancel()
	items, err := s.repo.ListEnabled(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	rules := make([]egresspolicy.PrivateExceptionRule, 0, len(items))
	if err == nil {
		for _, item := range items {
			prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(item.CIDR))
			prefix = egresspolicy.NormalizePrivateExceptionPrefix(prefix)
			if parseErr != nil || !egresspolicy.IsEligiblePrivateExceptionPrefix(prefix) {
				continue
			}
			rules = append(rules, egresspolicy.PrivateExceptionRule{ScopeType: item.ScopeType, ScopeID: item.ScopeID, Prefix: prefix, Port: item.Port})
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation == s.generation {
		if err != nil {
			s.rules = nil
			s.invalidate = true
			s.retryAt = time.Now().Add(egressPrivateExceptionRetryDelay)
		} else {
			s.rules = rules
			s.loadedAt = time.Now()
			s.invalidate = false
			s.retryAt = time.Time{}
		}
	}
	s.refreshing = nil
	close(done)
}

func (s *egressPrivateExceptionService) markStale() {
	s.mu.Lock()
	s.invalidate = true
	s.rules = nil
	s.generation++
	s.retryAt = time.Time{}
	s.mu.Unlock()
}

func (s *egressPrivateExceptionService) buildValidated(req SaveEgressPrivateExceptionRequest) (*models.EgressPrivateException, error) {
	scopeType := strings.ToLower(strings.TrimSpace(req.ScopeType))
	switch scopeType {
	case models.EgressPrivateScopeInstance, models.EgressPrivateScopeUser:
	default:
		return nil, errInvalidEgressPrivateScopeType
	}
	if req.ScopeID <= 0 {
		return nil, errInvalidEgressPrivateScopeType
	}
	if req.Port <= 0 || req.Port > 65535 {
		return nil, errInvalidEgressPrivatePort
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(req.CIDR))
	if err != nil {
		return nil, errInvalidEgressPrivateCIDR
	}
	prefix = egresspolicy.NormalizePrivateExceptionPrefix(prefix)
	if !egresspolicy.IsEligiblePrivateExceptionPrefix(prefix) {
		return nil, errEgressPrivateCIDRNotPrivate
	}
	switch scopeType {
	case models.EgressPrivateScopeInstance:
		instance, err := s.instanceRepo.GetByID(req.ScopeID)
		if err != nil {
			return nil, err
		}
		if instance == nil {
			return nil, errors.New("instance not found")
		}
	case models.EgressPrivateScopeUser:
		user, err := s.userRepo.GetByID(req.ScopeID)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, errors.New("user not found")
		}
	}

	var description *string
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		if trimmed != "" {
			description = &trimmed
		}
	}

	return &models.EgressPrivateException{
		ScopeType:   scopeType,
		ScopeID:     req.ScopeID,
		CIDR:        prefix.String(),
		Port:        req.Port,
		Enabled:     req.Enabled,
		Description: description,
		CreatedBy:   req.CreatedBy,
	}, nil
}
