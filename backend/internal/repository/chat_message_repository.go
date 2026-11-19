package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/observability"

	"github.com/upper/db/v4"
)

// ChatMessageRepository defines repository operations for persisted chat messages.
type ChatMessageRepository interface {
	Create(message *models.ChatMessageRecord) error
	ListByTraceID(traceID string) ([]models.ChatMessageRecord, error)
}

type chatMessageRepository struct {
	sess db.Session
}

// NewChatMessageRepository creates a new chat message repository (schema is owned by embedded migrations).
func NewChatMessageRepository(sess db.Session) ChatMessageRepository {
	repo := &chatMessageRepository{sess: sess}
	return repo
}

func (r *chatMessageRepository) Create(message *models.ChatMessageRecord) error {
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	if handled, err := persistObservation(context.Background(), r.sess, "chat_messages", message); handled {
		return err
	}
	return r.sess.Tx(func(tx db.Session) error {
		res, err := tx.Collection("chat_messages").Insert(message)
		if err != nil {
			return fmt.Errorf("failed to create chat message: %w", err)
		}
		message.ID = int(res.ID().(int64))
		return maintainObservationSummary(context.Background(), tx, "chat_messages", message)
	})
}

// CreateBatch persists one request's transcript before projecting its messages.
func (r *chatMessageRepository) CreateBatch(messages []*models.ChatMessageRecord) error {
	runtime := installedObservability.Load()
	if runtime != nil && len(messages) > 1 {
		handled, err := r.persistBatch(runtime, messages)
		if handled {
			return err
		}
	}
	for _, message := range messages {
		if err := r.Create(message); err != nil {
			return err
		}
	}
	return nil
}

func (r *chatMessageRepository) persistBatch(runtime *observationRuntime, messages []*models.ChatMessageRecord) (bool, error) {
	ctx := context.Background()
	release, err := backupgate.HoldWorkUnit(ctx, "observability.persist")
	if err != nil {
		return true, err
	}
	defer release()

	events := make([]observability.Event, 0, len(messages))
	now := time.Now()
	for _, message := range messages {
		if message.CreatedAt.IsZero() {
			message.CreatedAt = now
		}
		event, err := makeObservationEvent("chat_messages", message)
		if err != nil {
			return true, err
		}
		events = append(events, event)
	}
	first := events[0]
	if err := runtime.journal.AppendBatch(ctx, events); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		runtime.failures.Store(first.Owner+"\x00"+first.RequestID, err)
		return true, err
	}

	delivered := make([]observability.Event, 0, len(events))
	for i, event := range events {
		projectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		projected, err := runtime.projectValue(projectCtx, r.sess, event)
		cancel()
		if err != nil {
			break // The encrypted batch remains recoverable during storage outages.
		}
		messages[i].ID = projected.(*models.ChatMessageRecord).ID
		delivered = append(delivered, event)
	}
	return true, runtime.journal.DeliveredBatch(ctx, delivered)
}

func (r *chatMessageRepository) ListByTraceID(traceID string) ([]models.ChatMessageRecord, error) {
	items, err := routedLogDetails[models.ChatMessageRecord](r.sess, "chat_messages", traceID, "", 0, 200, false)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SequenceNo != items[j].SequenceNo {
			return items[i].SequenceNo < items[j].SequenceNo
		}
		return items[i].ID < items[j].ID
	})
	return items, err
}
