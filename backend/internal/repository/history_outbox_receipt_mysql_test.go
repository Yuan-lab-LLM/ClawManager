package repository

import (
	"clawreef/internal/models"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeliveredOutboxReceiptPreventsReplayAndRejectsChangedPayload(t *testing.T) {
	s := databaseReportingMySQL(t)
	cancellations, err := os.ReadFile("../db/migrations/120_add_team_outbox_cancellations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(splitHistorySQL(string(cancellations))[0]); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../db/migrations/114_add_history_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range splitHistorySQL(string(raw)) {
		if _, err = s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SQL().Exec(`CREATE TABLE team_event_outbox(id INT AUTO_INCREMENT PRIMARY KEY,team_id INT,source_event_id VARCHAR(255),destination VARCHAR(255),message_id VARCHAR(255),payload_json LONGTEXT,status VARCHAR(30),attempts INT,available_at DATETIME(6),last_error TEXT,delivered_at DATETIME(6),created_at DATETIME(6),updated_at DATETIME(6))`); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(`{"x":1}`))
	if _, err = s.SQL().Exec(`INSERT INTO history_outbox_receipts VALUES(1,'dest','message','source',?,?,?)`, hex.EncodeToString(hash[:]), time.Now().UTC(), hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`CREATE TABLE teams(id BIGINT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	s.SQL().Exec(`INSERT INTO teams VALUES(1)`)
	repo := NewTeamRepository(s)
	out := &models.TeamEventOutbox{TeamID: 1, Destination: "dest", MessageID: "message", SourceEventID: "source", PayloadJSON: `{"x":1}`}
	if err = repo.CreateEventOutbox(out); err != nil {
		t.Fatal(err)
	}
	var n int
	row, err := s.SQL().QueryRow(`SELECT COUNT(*) FROM team_event_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&n); err != nil || n != 0 {
		t.Fatalf("delivered replay inserted%d %v", n, err)
	}
	if out.Status != "delivered" {
		t.Fatalf("receipt replay status %q", out.Status)
	}
	out.PayloadJSON = `{"x":2}`
	if err = repo.CreateEventOutbox(out); err == nil {
		t.Fatal("changed payload accepted for delivered message identity")
	}
}

func splitHistorySQL(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	var queries []string
	for _, q := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(q) != "" {
			queries = append(queries, q)
		}
	}
	return queries
}

func TestArchivedTeamBusinessIdentitiesRemainIdempotent(t *testing.T) {
	s := databaseReportingMySQL(t)
	raw, err := os.ReadFile("../db/migrations/114_add_history_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range splitHistorySQL(string(raw)) {
		if _, err = s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SQL().Exec(`CREATE TABLE teams(id BIGINT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	s.SQL().Exec(`INSERT INTO teams VALUES(1)`)
	payload := `{"done":true}`
	digest := sha256.Sum256([]byte(payload))
	key := sha256.Sum256([]byte("event-key"))
	if _, err = s.SQL().Exec(`INSERT INTO history_identity_receipts VALUES('team_events',1,'event_id',UNHEX(?),77,?)`, hex.EncodeToString(key[:]), hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	repo := NewTeamRepository(s)
	eventID := "event-key"
	event := &models.TeamEvent{TeamID: 1, EventID: &eventID, PayloadJSON: &payload}
	if err = repo.CreateEvent(event); !errors.Is(err, ErrDuplicateTeamEvent) || event.ID != 77 {
		t.Fatalf("archived event replay %d %v", event.ID, err)
	}
	changed := `{"changed":true}`
	event.PayloadJSON = &changed
	if err = repo.CreateEvent(event); err == nil || errors.Is(err, ErrDuplicateTeamEvent) {
		t.Fatal("changed archived event falsely reported identical")
	}
	taskKey := sha256.Sum256([]byte("task-key"))
	if _, err = s.SQL().Exec(`INSERT INTO history_identity_receipts VALUES('team_tasks',1,'message_id',UNHEX(?),88,?)`, hex.EncodeToString(taskKey[:]), hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	task := &models.TeamTask{TeamID: 1, MessageID: "task-key", PayloadJSON: payload}
	if err = repo.CreateTask(task); !errors.Is(err, ErrArchivedTeamTask) || task.ID != 88 {
		t.Fatalf("archived task replay %d %v", task.ID, err)
	}
}
