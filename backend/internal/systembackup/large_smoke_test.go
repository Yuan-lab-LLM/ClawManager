package systembackup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func fixtureKubernetesServer() string {
	if value := os.Getenv("TEST_SYSTEM_BACKUP_KUBE_SERVER"); value != "" {
		return value
	}
	return "https://127.0.0.1:36443"
}

// Opt-in capacity fixture; never raises production admission or consistency.
// Budgets are fixed before capture, not extended after observing a failure.
func populateLargeSmoke(t *testing.T, ctx context.Context, source CaptureSources, root, bucket, prefix string) {
	t.Helper()
	started := time.Now()
	block := bytes.Repeat([]byte("team-backup-fragment-0123456789abcdef"), 32768)
	block = block[:1<<20]
	for i := 0; i < 2048; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("fragment-%04d.bin", i)), block, 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("large fixture: 2 GiB workspace in 2048 files")
	for i := 0; i < 10000; i++ {
		if _, e := source.Objects.PutObject(ctx, bucket, fmt.Sprintf("skills/large-smoke/%08d.bin", i), bytes.NewReader(block[:1024]), 1024, minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"fixture": "large-smoke"}}); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("large fixture: 10000 Skill objects")
	for batch := 0; batch < 100; batch++ {
		values := make([]string, 1000)
		for i := range values {
			values[i] = "(901,901,'large_smoke','{\"partial\":true}')"
		}
		if _, e := source.MySQL.ExecContext(ctx, "INSERT INTO team_events(team_id,task_id,event_type,payload_json) VALUES "+strings.Join(values, ",")); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("large fixture: 100000 Team events")
	// Register cleanup before population so an interrupted fixture remains bounded.
	t.Cleanup(func() {
		for batch := 0; batch < 100; batch++ {
			args := [][]byte{[]byte("DEL")}
			for i := 0; i < 1000; i++ {
				args = append(args, []byte(fmt.Sprintf("%s:large:%06d", prefix, batch*1000+i)))
			}
			if _, e := source.Redis.command(context.Background(), args...); e != nil {
				t.Error(e)
				return
			}
		}
	})
	for batch := 0; batch < 100; batch++ {
		args := [][]byte{[]byte("MSET")}
		for i := 0; i < 1000; i++ {
			args = append(args, []byte(fmt.Sprintf("%s:large:%06d", prefix, batch*1000+i)), block[:128])
		}
		if _, e := source.Redis.command(ctx, args...); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("large fixture: 100000 Redis records; prepared in %s", time.Since(started))
}
