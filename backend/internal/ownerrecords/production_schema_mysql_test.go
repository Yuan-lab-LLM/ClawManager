package ownerrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func addProductionOwnerColumn(t *testing.T, s *Store) {
	t.Helper()
	raw, err := os.ReadFile("../db/migrations/047_add_instance_owner.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := regexp.MustCompile(`ALTER TABLE instances ADD COLUMN owner_normalized [^']+`).FindString(string(raw))
	if ddl == "" {
		t.Fatal("production owner migration not found")
	}
	resilienceExec(t, s, ddl)
}

func TestSyncResilienceMySQLProductionCheckpointSchema(t *testing.T) {
	s := resilienceStore(t)
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(1,99,'schema@example.test','openclaw','lite','/fixture','fixture')`)
	i := scanItem{identity: Identity{Scope: 99, Instance: 1, Owner: "schema@example.test", Runtime: "openclaw", Mode: "lite"}, workspace: "/fixture", runtimeGeneration: 1}
	row, err := s.Session.SQL().QueryRow(`SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&i.generation); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, lane := range []string{"recent", "history"} {
		if err = s.finishScan(ctx, lane, i, time.Now(), nil); err != nil {
			t.Fatal("production-schema checkpoint", lane, err)
		}
		if err = s.finishScan(ctx, lane, i, time.Now(), nil); err != nil {
			t.Fatal("idempotent checkpoint", lane, err)
		}
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('recent_schedule','history_schedule') AND status='ready'`); n != 2 {
		t.Fatal("missing checkpoints", n)
	}
	resilienceExec(t, s, `UPDATE instances SET runtime_generation=2 WHERE id=1`)
	if err = s.finishScan(ctx, "history", i, time.Now(), context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_schedule' AND status='ready'`); n != 1 {
		t.Fatal("late generation overwrote checkpoint")
	}
}

func TestPortalUsersMySQLGlobalPagination(t *testing.T) {
	s := resilienceStore(t)
	now := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	for n := 0; n < 61; n++ {
		owner := fmt.Sprintf("page-%03d@example.test", n)
		resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,rule_version,payload) VALUES('growth_profile',?,0,?,'active',?,'growth-v4',JSON_OBJECT())`, Key("growth", owner), owner, n)
		tasks := []GrowthTask{{Code: "a", Period: "day", Awarded: n%2 == 1}, {Code: "b", Period: "week", Awarded: true}}
		raw, _ := json.Marshal(map[string]any{"dashboard": map[string]any{"summary": Metrics{TotalTokens: uint64(1000 - n), Sessions: uint64(n)}}, "growth": map[string]any{"tasks": tasks, "as_of": now.Format(time.RFC3339)}})
		resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,payload) VALUES('owner_snapshot',?,0,?,'published',?)`, Key("owner-published", owner), owner, string(raw))
		for d := 0; d < n%4; d++ {
			day := now.AddDate(0, 0, -d).Format("2006-01-02")
			resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date) VALUES('login_day',?,0,?,'confirmed',?)`, Key("login", owner, day), owner, day)
		}
	}
	for _, order := range []string{"xp", "tokens", "tasks", "visits", "streak"} {
		var all []PortalUser
		for page := 1; page <= 4; page++ {
			got, err := s.PortalUsers(context.Background(), "page-", order, page, now)
			if err != nil {
				t.Fatal(order, err)
			}
			if got.Total != 61 {
				t.Fatal("total", got.Total)
			}
			all = append(all, got.Users...)
		}
		if len(all) != 61 {
			t.Fatal("pagination lost users", order, len(all))
		}
		seen := map[string]bool{}
		for j, u := range all {
			if seen[u.Owner] {
				t.Fatal("duplicate owner", u.Owner)
			}
			seen[u.Owner] = true
			if j > 0 {
				a := all[j-1]
				av, bv := int64(a.XP), int64(u.XP)
				switch order {
				case "tokens":
					av, bv = int64(a.Tokens), int64(u.Tokens)
				case "tasks":
					av, bv = int64(a.TasksDone), int64(u.TasksDone)
				case "visits":
					av, bv = int64(a.Visits.Total), int64(u.Visits.Total)
				case "streak":
					av, bv = int64(a.Visits.Streak), int64(u.Visits.Streak)
				}
				if av < bv || av == bv && a.Owner > u.Owner {
					t.Fatal("not globally sorted", order, j)
				}
			}
		}
	}
	got, err := s.PortalUsers(context.Background(), "page-060", "tokens", 1, now)
	if err != nil || got.Total != 1 || len(got.Users) != 1 || got.Users[0].Tokens != 940 {
		t.Fatal("search", got, err)
	}
}

func TestPortalUsersMySQLBeyondLegacyUserLimit(t *testing.T) {
	s := resilienceStore(t)
	resilienceExec(t, s, `CREATE TABLE fixture_digits(n INT PRIMARY KEY)`)
	resilienceExec(t, s, `INSERT INTO fixture_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,payload) SELECT 'growth_profile',SHA2(CONCAT('scale-',n),256),0,CONCAT('scale-',LPAD(n,5,'0'),'@example.test'),'active',n,JSON_OBJECT() FROM (SELECT a.n+10*b.n+100*c.n+1000*d.n n FROM fixture_digits a CROSS JOIN fixture_digits b CROSS JOIN fixture_digits c CROSS JOIN fixture_digits d) nums WHERE n<5001`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := s.PortalUsers(ctx, "", "xp", 251, time.Now())
	if err != nil {
		t.Fatal("legacy whole-site cap still rejects pagination", err)
	}
	if got.Total != 5001 || len(got.Users) != 1 || got.Users[0].Owner != "scale-00000@example.test" {
		t.Fatal("wrong last page", got.Total, len(got.Users))
	}
}

func TestPortalUsersMySQLProductionEquivalentPayload(t *testing.T) {
	s := resilienceStore(t)
	now := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	profiles, snapshots, logins := []string{}, []string{}, []string{}
	pa, sa, la := []any{}, []any{}, []any{}
	loginCount := 0
	for n := 0; n < 853; n++ {
		owner := fmt.Sprintf("realistic-%04d@example.test", n)
		profiles = append(profiles, "('growth_profile',?,0,?,'active',?,JSON_OBJECT())")
		pa = append(pa, Key("growth", owner), owner, n)
		raw, _ := json.Marshal(map[string]any{"dashboard": map[string]any{"summary": Metrics{TotalTokens: uint64(10000 - n)}}, "growth": map[string]any{"tasks": []GrowthTask{{Code: "day", Period: "day", Awarded: true}}, "as_of": now.Format(time.RFC3339)}, "retained_fixture": strings.Repeat("x", 2800)})
		snapshots = append(snapshots, "('owner_snapshot',?,0,?,'published',0,?)")
		sa = append(sa, Key("owner-published", owner), owner, string(raw))
		if n < 261 {
			snapshots = append(snapshots, "('owner_snapshot',?,0,?,'archived',0,?)")
			sa = append(sa, Key("old-snapshot", owner), owner, string(raw))
		}
		for d := 0; d < 3 && loginCount < 2421; d++ {
			day := now.AddDate(0, 0, -d).Format("2006-01-02")
			logins = append(logins, "('login_day',?,0,?,'confirmed',?)")
			la = append(la, Key("login", owner, day), owner, day)
			loginCount++
		}
	}
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,payload) VALUES `+strings.Join(profiles, ","), pa...)
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,payload) VALUES `+strings.Join(snapshots, ","), sa...)
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date) VALUES `+strings.Join(logins, ","), la...)
	for _, order := range []string{"xp", "tokens", "tasks", "visits", "streak"} {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		page, err := s.PortalUsers(ctx, "", order, 1, now)
		cancel()
		if err != nil || page.Total != 853 || len(page.Users) != 20 {
			t.Fatal("production-equivalent first page", order, page.Total, err)
		}
		seen := map[string]bool{}
		for _, u := range page.Users {
			if seen[u.Owner] {
				t.Fatal("archived snapshots duplicated a published user", order, u.Owner)
			}
			seen[u.Owner] = true
		}
		t.Logf("853 profiles/1114 snapshots (~3MiB)/2421 login days: sort=%s elapsed=%s", order, time.Since(started))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.PortalUsers(ctx, "", "xp", 1, now); err == nil {
		t.Fatal("cancelled admin read continued")
	}
}
