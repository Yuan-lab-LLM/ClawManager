package services

import (
	"testing"

	"clawreef/internal/models"
)

type stubChatMessageRepository struct {
	existing []models.ChatMessageRecord
	created  []models.ChatMessageRecord
}

func (r *stubChatMessageRepository) Create(message *models.ChatMessageRecord) error {
	copyRecord := *message
	r.created = append(r.created, copyRecord)
	r.existing = append(r.existing, copyRecord)
	return nil
}

func (r *stubChatMessageRepository) ListByTraceID(traceID string) ([]models.ChatMessageRecord, error) {
	items := make([]models.ChatMessageRecord, len(r.existing))
	copy(items, r.existing)
	return items, nil
}

func TestRecordMessagesAppendsOnlyNewTranscriptSuffix(t *testing.T) {
	repo := &stubChatMessageRepository{
		existing: []models.ChatMessageRecord{
			{TraceID: "trc_1", SessionID: "sess_1", Role: "user", Content: "北京今天天气怎么样", SequenceNo: 1},
			{TraceID: "trc_1", SessionID: "sess_1", Role: "assistant", Content: "tool_call get_weather({\"city\":\"Beijing\"})", SequenceNo: 2},
		},
	}
	service := NewChatMessageService(repo)

	err := service.RecordMessages(
		"trc_1",
		"sess_1",
		nil,
		nil,
		nil,
		nil,
		[]PersistedChatMessage{
			{Role: "user", Content: "北京今天天气怎么样"},
			{Role: "assistant", Content: "tool_call get_weather({\"city\":\"Beijing\"})"},
			{Role: "tool", Content: "{\"temp_c\":25}"},
		},
	)
	if err != nil {
		t.Fatalf("RecordMessages returned error: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("expected exactly one new persisted message, got %d", len(repo.created))
	}
	if repo.created[0].Role != "tool" {
		t.Fatalf("expected appended role tool, got %q", repo.created[0].Role)
	}
	if repo.created[0].SequenceNo != 3 {
		t.Fatalf("expected appended sequence number 3, got %d", repo.created[0].SequenceNo)
	}
}
