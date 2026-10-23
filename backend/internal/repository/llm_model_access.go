package repository

import (
	"clawreef/internal/models"
	"context"
	"sync"
)

// ModelAccessChecker is implemented by the durable repository; reads the primary
// database on dispatch and during active calls, not a stale in-process ACL cache.
type ModelAccessChecker interface {
	CheckModelAccess(context.Context, int, int) error
}

func (r *llmModelRepository) CheckModelAccess(ctx context.Context, userID, modelID int) error {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT EXISTS (
	 SELECT 1 FROM llm_models m JOIN users u ON u.id = ? AND u.is_active = TRUE
	 WHERE m.id = ? AND m.is_active = TRUE AND
	 (m.visibility_scope = 'all' OR (m.visibility_scope = 'selected_users' AND
	 EXISTS (SELECT 1 FROM llm_model_user_grants g WHERE g.model_id = m.id AND g.user_id = u.id)))
	)`, userID, modelID)
	if err != nil {
		return err
	}
	var allowed bool
	if err := row.Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return models.ErrModelUnavailable
	}
	return nil
}

var modelAccessChanges = struct {
	sync.Mutex
	listeners map[chan struct{}]struct{}
}{listeners: make(map[chan struct{}]struct{})}

// Local notifications reduce revocation latency. Other replicas also poll the
// primary database; notification delivery is never the authorization boundary.
func SubscribeModelAccessChanges() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	modelAccessChanges.Lock()
	modelAccessChanges.listeners[ch] = struct{}{}
	modelAccessChanges.Unlock()
	return ch, func() {
		modelAccessChanges.Lock()
		delete(modelAccessChanges.listeners, ch)
		modelAccessChanges.Unlock()
	}
}
func publishModelAccessChange() {
	modelAccessChanges.Lock()
	defer modelAccessChanges.Unlock()
	for ch := range modelAccessChanges.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ForModelUser is a request-local view. Every router read (including fallback)
// goes through this view; the shared service/repository is never mutated.
func ForModelUser(repo LLMModelRepository, userID int) LLMModelRepository {
	return &userModelRepository{LLMModelRepository: repo, userID: userID}
}

type userModelRepository struct {
	LLMModelRepository
	userID int
}

func (r *userModelRepository) filter(items []models.LLMModel, err error) ([]models.LLMModel, error) {
	if err != nil {
		return nil, err
	}
	if r.userID <= 0 {
		return nil, models.ErrModelUnavailable
	}
	visible := make([]models.LLMModel, 0, len(items))
	for _, item := range items {
		if item.VisibleTo(r.userID) {
			visible = append(visible, item)
		}
	}
	if len(items) > 0 && len(visible) == 0 {
		return nil, models.ErrModelUnavailable
	}
	return visible, nil
}
func (r *userModelRepository) List() ([]models.LLMModel, error) {
	return r.filter(r.LLMModelRepository.List())
}
func (r *userModelRepository) ListActive() ([]models.LLMModel, error) {
	return r.filter(r.LLMModelRepository.ListActive())
}
func (r *userModelRepository) GetByID(id int) (*models.LLMModel, error) {
	m, err := r.LLMModelRepository.GetByID(id)
	if err != nil {
		return nil, err
	}
	if m != nil && !m.VisibleTo(r.userID) {
		return nil, models.ErrModelUnavailable
	}
	return m, nil
}
func (r *userModelRepository) GetByDisplayName(name string) (*models.LLMModel, error) {
	m, err := r.LLMModelRepository.GetByDisplayName(name)
	if err != nil {
		return nil, err
	}
	if m != nil && !m.VisibleTo(r.userID) {
		return nil, models.ErrModelUnavailable
	}
	return m, nil
}
