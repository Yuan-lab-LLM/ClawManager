package services

import (
	"testing"

	"clawreef/internal/models"
)

// stubInstanceVisibilityRepo is a minimal InstanceRepository used by the
// visibility tests below. It only implements the read paths exercised by
// GetByUserID / GetAllInstances; every other method panics so that any
// accidental call surfaces as a test failure instead of a silent stub.
type stubInstanceVisibilityRepo struct {
	all []models.Instance
}

func (r *stubInstanceVisibilityRepo) byUser(userID int) []models.Instance {
	out := make([]models.Instance, 0, len(r.all))
	for _, inst := range r.all {
		if inst.UserID == userID {
			out = append(out, inst)
		}
	}
	return out
}

func (r *stubInstanceVisibilityRepo) Create(*models.Instance) error { panic("not used") }
func (r *stubInstanceVisibilityRepo) GetByID(int) (*models.Instance, error) {
	panic("not used")
}
func (r *stubInstanceVisibilityRepo) GetByAccessToken(string) (*models.Instance, error) {
	panic("not used")
}
func (r *stubInstanceVisibilityRepo) GetByAgentBootstrapToken(string) (*models.Instance, error) {
	panic("not used")
}
func (r *stubInstanceVisibilityRepo) GetAll(offset, limit int) ([]models.Instance, error) {
	return paginate(r.all, offset, limit), nil
}
func (r *stubInstanceVisibilityRepo) CountAll() (int, error) { return len(r.all), nil }
func (r *stubInstanceVisibilityRepo) GetByUserID(userID, offset, limit int) ([]models.Instance, error) {
	return paginate(r.byUser(userID), offset, limit), nil
}
func (r *stubInstanceVisibilityRepo) CountByUserID(userID int) (int, error) {
	return len(r.byUser(userID)), nil
}
func (r *stubInstanceVisibilityRepo) ExistsByUserIDAndName(int, string) (bool, error) {
	return false, nil
}
func (r *stubInstanceVisibilityRepo) GetAllRunning() ([]models.Instance, error) {
	return nil, nil
}
func (r *stubInstanceVisibilityRepo) Update(*models.Instance) error { panic("not used") }
func (r *stubInstanceVisibilityRepo) Delete(int) error              { panic("not used") }

func paginate(items []models.Instance, offset, limit int) []models.Instance {
	if offset >= len(items) {
		return []models.Instance{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	out := make([]models.Instance, end-offset)
	copy(out, items[offset:end])
	return out
}

// TestGetByUserIDFiltersByCaller locks in the workspace-view contract: the
// caller-scoped listing must return only the caller's own instances,
// regardless of any role the caller may hold elsewhere. Regression guard
// for the admin-cross-user-leakage bug in which role=admin widened this
// endpoint to every user's instances.
func TestGetByUserIDFiltersByCaller(t *testing.T) {
	t.Parallel()

	repo := &stubInstanceVisibilityRepo{
		all: []models.Instance{
			{ID: 1, UserID: 10, Name: "alice-1"},
			{ID: 2, UserID: 10, Name: "alice-2"},
			{ID: 3, UserID: 20, Name: "bob-1"},
		},
	}
	svc := &instanceService{instanceRepo: repo}

	instances, total, err := svc.GetByUserID(10, 0, 50)
	if err != nil {
		t.Fatalf("GetByUserID returned error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2 for alice, got %d", total)
	}
	if len(instances) != 2 || instances[0].Name != "alice-1" || instances[1].Name != "alice-2" {
		t.Fatalf("expected alice's instances only, got %+v", instances)
	}

	instances, total, err = svc.GetByUserID(20, 0, 50)
	if err != nil {
		t.Fatalf("GetByUserID returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1 for bob, got %d", total)
	}
	if len(instances) != 1 || instances[0].Name != "bob-1" {
		t.Fatalf("expected bob's instance only, got %+v", instances)
	}

	// A user with no instances sees an empty list, never someone else's.
	instances, total, err = svc.GetByUserID(99, 0, 50)
	if err != nil {
		t.Fatalf("GetByUserID returned error: %v", err)
	}
	if total != 0 || len(instances) != 0 {
		t.Fatalf("expected empty listing for user 99, got total=%d instances=%+v", total, instances)
	}
}

// TestGetAllInstancesReturnsEveryUser verifies the admin-console surface:
// cross-user listing, with pagination, independent of caller identity (the
// admin middleware gates reachability at the route layer, not here).
func TestGetAllInstancesReturnsEveryUser(t *testing.T) {
	t.Parallel()

	repo := &stubInstanceVisibilityRepo{
		all: []models.Instance{
			{ID: 1, UserID: 10, Name: "alice-1"},
			{ID: 2, UserID: 10, Name: "alice-2"},
			{ID: 3, UserID: 20, Name: "bob-1"},
		},
	}
	svc := &instanceService{instanceRepo: repo}

	instances, total, err := svc.GetAllInstances(0, 50)
	if err != nil {
		t.Fatalf("GetAllInstances returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3 across users, got %d", total)
	}
	if len(instances) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(instances))
	}

	// Pagination still respected.
	page1, _, err := svc.GetAllInstances(0, 2)
	if err != nil {
		t.Fatalf("GetAllInstances page1 error: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected page1 size=2, got %d", len(page1))
	}
	page2, _, err := svc.GetAllInstances(2, 2)
	if err != nil {
		t.Fatalf("GetAllInstances page2 error: %v", err)
	}
	if len(page2) != 1 || page2[0].Name != "bob-1" {
		t.Fatalf("expected page2=[bob-1], got %+v", page2)
	}
}
