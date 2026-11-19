package repository

import (
	"errors"
	"fmt"
	"testing"

	"clawreef/internal/models"
)

func TestCancelledOutboxOnlyVerifiedReceiptsReleaseGlobalCapacity(t *testing.T) {
	s := databaseReportingMySQL(t)
	for _, q := range []string{
		`CREATE TABLE teams(id BIGINT PRIMARY KEY)`,
		`INSERT INTO teams VALUES(1),(2)`,
		`CREATE TABLE team_outbox_admission(id INT PRIMARY KEY,generation BIGINT)`,
		`INSERT INTO team_outbox_admission VALUES(1,0)`,
		`CREATE TABLE team_event_outbox(id BIGINT AUTO_INCREMENT PRIMARY KEY,team_id BIGINT,source_event_id VARCHAR(255),destination VARCHAR(255),message_id VARCHAR(255),payload_json LONGTEXT,status VARCHAR(30),attempts INT,available_at DATETIME(6),last_error TEXT,delivered_at DATETIME(6),created_at DATETIME(6),updated_at DATETIME(6),KEY identity_idx(team_id,destination,message_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE history_outbox_cancellations(team_id BIGINT,destination VARCHAR(255),message_id VARCHAR(255),source_event_id VARCHAR(255),payload_sha256 CHAR(64),cancelled_at DATETIME(6),reason VARCHAR(255),closure_sha256 CHAR(64),PRIMARY KEY(team_id,destination,message_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE history_outbox_receipts(team_id BIGINT,destination VARCHAR(255),message_id VARCHAR(255),source_event_id VARCHAR(255),payload_sha256 CHAR(64),delivered_at DATETIME(6),PRIMARY KEY(team_id,destination,message_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE fixture_digits(n INT PRIMARY KEY)`,
		`INSERT INTO fixture_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`,
		`INSERT INTO team_event_outbox(team_id,source_event_id,destination,message_id,payload_json,status) SELECT 1,'source','worker',CONCAT('message-',a.n+10*b.n+100*c.n+1000*d.n+10000*e.n),'{}','cancelled' FROM fixture_digits a CROSS JOIN fixture_digits b CROSS JOIN fixture_digits c CROSS JOIN fixture_digits d CROSS JOIN fixture_digits e LIMIT 75000`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewTeamRepository(s).(*teamRepository)
	fresh := &models.TeamEventOutbox{TeamID: 2, SourceEventID: "dispatch:new", Destination: "worker", MessageID: "new", PayloadJSON: "{}"}
	if err := repo.CreateEventOutbox(fresh); !errors.Is(err, ErrTeamOutboxCapacity) {
		t.Fatalf("unproven cancellation released capacity: %v", err)
	}
	if _, err := s.SQL().Exec(`INSERT INTO history_outbox_cancellations SELECT team_id,destination,message_id,source_event_id,REPEAT('0',64),UTC_TIMESTAMP(6),'native cancellation',REPEAT('a',64) FROM team_event_outbox`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateEventOutbox(fresh); !errors.Is(err, ErrTeamOutboxCapacity) {
		t.Fatalf("mismatched cancellation released capacity: %v", err)
	}
	if _, err := s.SQL().Exec(`UPDATE history_outbox_cancellations SET payload_sha256=SHA2('{}',256)`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, change string }{
		{"source case", "source_event_id=UPPER(source_event_id)"},
		{"destination case", "destination=UPPER(destination)"},
		{"message case", "message_id=UPPER(message_id)"},
		{"payload digest case", "payload_sha256=UPPER(payload_sha256)"},
		{"closure digest case", "closure_sha256=UPPER(closure_sha256)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.SQL().Exec("UPDATE history_outbox_cancellations SET " + tc.change); err != nil {
				t.Fatal(err)
			}
			candidate := &models.TeamEventOutbox{TeamID: 2, SourceEventID: "dispatch:new", Destination: "worker", MessageID: fmt.Sprint("new-", tc.name), PayloadJSON: "{}"}
			if err := repo.CreateEventOutbox(candidate); !errors.Is(err, ErrTeamOutboxCapacity) {
				t.Errorf("case-only %s mismatch released 75000 cancellations from quota: %v", tc.name, err)
			}
			if _, err := s.SQL().Exec("UPDATE history_outbox_cancellations SET source_event_id=LOWER(source_event_id),destination=LOWER(destination),message_id=LOWER(message_id),payload_sha256=LOWER(payload_sha256),closure_sha256=LOWER(closure_sha256)"); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := repo.CreateEventOutbox(fresh); err != nil {
		t.Fatalf("75000 proven terminal cancellations still exhaust global new-work admission: %v", err)
	}
	if fresh.ID < 1 || fresh.Status != "pending" {
		t.Fatalf("new work was not durably accepted: %+v", fresh)
	}
}
