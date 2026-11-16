package admindashboard

import (
	"context"
	"encoding/json"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"testing"
	"time"
)

// Explicit opt-in, SELECT-only smoke test. Credentials stay in a private remote
// file and are never logged. This test does not create schema or business data.
func TestLiveMySQLReadOnlyAggregation(t *testing.T) {
	path := os.Getenv("ADMIN_OVERVIEW_TEST_DB_CONFIG")
	if path == "" {
		t.Skip("explicit read-only DB config required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read private DB config")
	}
	var cfg mysql.ConnectionURL
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("invalid private DB config")
	}
	sess, err := mysql.Open(cfg)
	if err != nil {
		t.Fatal("database connection unavailable")
	}
	defer sess.Close()
	d := NewStore(sess).Overview(context.Background(), time.Now())
	for _, key := range []string{"instances", "usage", "costs", "runtimes", "conversations"} {
		if d.Coverage[key].Status == "unavailable" {
			t.Fatalf("live SQL source unavailable: %s", key)
		}
	}
	if d.TotalUsers == nil || d.TotalInstances == nil || d.Usage.TotalTokens == nil {
		t.Fatal("live totals unavailable")
	}
	var modelTokens int64
	for _, m := range d.Models {
		modelTokens += m.Tokens
	}
	if modelTokens != *d.Usage.TotalTokens {
		t.Fatal("model and global token sources diverge")
	}
	var historyTokens int64
	for _, p := range d.History {
		if p.Tokens == nil {
			t.Fatal("history missing")
		}
		historyTokens += *p.Tokens
	}
	if historyTokens != *d.Usage.TotalTokens {
		t.Fatal("hourly and global token sources diverge")
	}
	if d.Usage.Conversations == nil {
		t.Fatal("recorded conversations unavailable")
	}
	var conversationTotal int64
	for _, p := range d.History {
		if p.Conversations == nil {
			t.Fatal("durable conversation history missing")
		}
		conversationTotal += *p.Conversations
	}
	if conversationTotal != *d.Usage.Conversations {
		t.Fatal("conversation total and hourly source diverge")
	}
	t.Logf("recorded gateway conversation turns=%d; hourly sum=%d", *d.Usage.Conversations, conversationTotal)

	t.Logf("read-only live SQL valid: users=%d instances=%d tokens=%d models=%d currencies=%d conversations=%s runtime=%s", *d.TotalUsers, *d.TotalInstances, *d.Usage.TotalTokens, len(d.Models), len(d.Usage.Currencies), d.Coverage["conversations"].Status, d.Coverage["runtimes"].Status)
}
