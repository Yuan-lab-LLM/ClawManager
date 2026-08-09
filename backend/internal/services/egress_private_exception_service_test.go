package services

import (
	"net/netip"
	"testing"

	"clawreef/internal/models"
)

type stubEgressPrivateExceptionRepo struct {
	items []models.EgressPrivateException
	next  int
}

func (s *stubEgressPrivateExceptionRepo) List(string, *int) ([]models.EgressPrivateException, error) {
	return append([]models.EgressPrivateException(nil), s.items...), nil
}
func (s *stubEgressPrivateExceptionRepo) ListEnabled() ([]models.EgressPrivateException, error) {
	var out []models.EgressPrivateException
	for _, item := range s.items {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *stubEgressPrivateExceptionRepo) GetByID(id int) (*models.EgressPrivateException, error) {
	for i := range s.items {
		if s.items[i].ID == id {
			item := s.items[i]
			return &item, nil
		}
	}
	return nil, nil
}
func (s *stubEgressPrivateExceptionRepo) Create(item *models.EgressPrivateException) error {
	s.next++
	item.ID = s.next
	s.items = append(s.items, *item)
	return nil
}
func (s *stubEgressPrivateExceptionRepo) Update(item *models.EgressPrivateException) error {
	for i := range s.items {
		if s.items[i].ID == item.ID {
			s.items[i] = *item
			return nil
		}
	}
	return errEgressPrivateExceptionNotFound
}
func (s *stubEgressPrivateExceptionRepo) Delete(id int) error {
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return nil
		}
	}
	return errEgressPrivateExceptionNotFound
}

type stubEgressInstanceLookup struct {
	instance *models.Instance
}

func (s *stubEgressInstanceLookup) GetByID(id int) (*models.Instance, error) {
	if s.instance != nil && s.instance.ID == id {
		return s.instance, nil
	}
	return nil, nil
}

type stubEgressUserLookup struct {
	user *models.User
}

func (s *stubEgressUserLookup) GetByID(id int) (*models.User, error) {
	if s.user != nil && s.user.ID == id {
		return s.user, nil
	}
	return nil, nil
}

func TestEgressPrivateExceptionServiceRejectsPublicCIDR(t *testing.T) {
	svc := NewEgressPrivateExceptionService(
		&stubEgressPrivateExceptionRepo{},
		&stubEgressInstanceLookup{instance: &models.Instance{ID: 8, UserID: 1}},
		&stubEgressUserLookup{user: &models.User{ID: 1}},
	)
	_, err := svc.Create(SaveEgressPrivateExceptionRequest{
		ScopeType: "instance",
		ScopeID:   8,
		CIDR:      "8.8.8.0/24",
		Port:      18080,
		Enabled:   true,
	})
	if err == nil || err.Error() != errEgressPrivateCIDRNotPrivate.Error() {
		t.Fatalf("expected private cidr error, got %v", err)
	}
}

func TestEgressPrivateExceptionServiceRejectsInvalidPort(t *testing.T) {
	svc := NewEgressPrivateExceptionService(
		&stubEgressPrivateExceptionRepo{},
		&stubEgressInstanceLookup{instance: &models.Instance{ID: 8, UserID: 1}},
		&stubEgressUserLookup{user: &models.User{ID: 1}},
	)
	_, err := svc.Create(SaveEgressPrivateExceptionRequest{
		ScopeType: "instance",
		ScopeID:   8,
		CIDR:      "10.255.25.3/32",
		Port:      0,
		Enabled:   true,
	})
	if err == nil || err.Error() != errInvalidEgressPrivatePort.Error() {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestEgressPrivateExceptionServiceCreatesPrivateCIDR(t *testing.T) {
	repo := &stubEgressPrivateExceptionRepo{}
	svc := NewEgressPrivateExceptionService(
		repo,
		&stubEgressInstanceLookup{instance: &models.Instance{ID: 8, UserID: 1}},
		&stubEgressUserLookup{user: &models.User{ID: 1}},
	)
	item, err := svc.Create(SaveEgressPrivateExceptionRequest{
		ScopeType: "instance",
		ScopeID:   8,
		CIDR:      "10.255.25.3/32",
		Port:      18080,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if item.CIDR != "10.255.25.3/32" || item.Port != 18080 {
		t.Fatalf("unexpected item: %+v", item)
	}
	instanceID := 8
	if !svc.Allows(&instanceID, nil, netip.MustParseAddr("10.255.25.3"), 18080) {
		t.Fatal("expected allows after create")
	}
}
