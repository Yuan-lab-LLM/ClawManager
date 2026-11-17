package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTeamOutboxReceiptClosureRequiresTerminalSQLState(t *testing.T) {
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
	for _, q := range []string{
		`CREATE TABLE teams(id INT PRIMARY KEY)`, `INSERT INTO teams VALUES(1)`,
		`CREATE TABLE team_tasks(id INT PRIMARY KEY,team_id INT,message_id VARCHAR(255),status VARCHAR(30),finished_at DATETIME(6))`,
		`CREATE TABLE team_work_items(id INT PRIMARY KEY,root_task_id INT,status VARCHAR(30),finished_at DATETIME(6))`,
		`CREATE TABLE team_workflow_phases(id INT PRIMARY KEY,root_task_id INT,status VARCHAR(30),completed_at DATETIME(6))`,
		`CREATE TABLE team_event_outbox(id INT PRIMARY KEY,team_id INT,destination VARCHAR(255),message_id VARCHAR(255),source_event_id VARCHAR(255),payload_json LONGTEXT,status VARCHAR(30),created_at DATETIME(6),delivered_at DATETIME(6))`,
		`CREATE TABLE team_projection_command_sources(team_id INT,message_id VARCHAR(255),source_stream_id VARCHAR(128),task_id INT,PRIMARY KEY(team_id,message_id))`,
		`CREATE TABLE team_outbox_receipt_closures(outbox_id BIGINT PRIMARY KEY,team_id BIGINT,task_id BIGINT,source_window_sha256 CHAR(64),closed_before DATETIME(6),closed_at DATETIME(6),redis_reclaimed_at DATETIME(6))`,
	} {
		if _, err = s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().AddDate(0, 0, -10)
	s.SQL().Exec(`INSERT INTO team_tasks VALUES(7,1,'task','succeeded',?)`, old)
	if _, err = s.SQL().Exec(`INSERT INTO team_event_outbox VALUES(1,1,'claw:team:1:inbox:worker','message','source','{"taskId":"team-1-task-7"}','delivered',?,?)`, old, old); err != nil {
		t.Fatal(err)
	}
	repo := NewTeamRepository(s).(TeamOutboxRetentionRepository)
	ctx := context.Background()
	rows, err := repo.ListEligibleTeamOutboxReceipts(ctx, 1, time.Now().UTC().AddDate(0, 0, -8), 100)
	if err != nil || len(rows) != 1 || rows[0].TaskID != 7 {
		t.Fatalf("eligible %+v %v", rows, err)
	}
	if err = repo.CloseTeamOutboxReceiptWindow(ctx, 1, 7, "unverified"); err == nil {
		t.Fatal("invalid source proof accepted")
	}
	s.SQL().Exec(`UPDATE team_tasks SET status='running' WHERE id=7`)
	if err = repo.CloseTeamOutboxReceiptWindow(ctx, 1, 7, strings.Repeat("a", 64)); err == nil {
		t.Fatal("active task identity closed")
	}
	s.SQL().Exec(`UPDATE team_tasks SET status='succeeded' WHERE id=7`)
	if err = repo.CloseTeamOutboxReceiptWindow(ctx, 1, 7, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ListEligibleTeamOutboxReceipts(ctx, 1, time.Now().UTC().AddDate(0, 0, -8), 100)
	if err != nil || len(rows) != 1 || !rows[0].Closed {
		t.Fatal("closed Redis retry lost", err)
	}
	if err = repo.CloseTeamOutboxReceiptWindow(ctx, 1, 7, strings.Repeat("a", 64)); err != nil {
		t.Fatal("closure retry not idempotent", err)
	}
	if err = repo.MarkTeamOutboxReceiptReclaimed(ctx, 1); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ListEligibleTeamOutboxReceipts(ctx, 1, time.Now().UTC().AddDate(0, 0, -8), 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("closed receipt starved later pages", err)
	}
	var count int
	row, _ := s.SQL().QueryRow(`SELECT COUNT(*) FROM history_outbox_receipts`)
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatal("permanent SQL dedup absent", err)
	}
}
