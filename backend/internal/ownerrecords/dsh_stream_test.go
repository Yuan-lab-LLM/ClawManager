package ownerrecords

import (
	"context"
	"errors"
	"github.com/klauspost/compress/zstd"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDSHStreamingLargeAndPartial(t *testing.T) {
	head := `{"type":"session","id":"s"}` + "\n"
	noise := strings.Repeat(`{"type":"token","data":"`+strings.Repeat("x", 300)+`"}`+"\n", 35000)
	turn := `{"type":"turn/start","data":{"turn":1}}
{"type":"user/message","time":1789610460000,"data":{"id":"u","source":{"kind":"user"},"content":"hi"}}
{"type":"assistant/message","data":{"turn":1,"message":{"content":"ok"}}}
{"type":"turn/end","time":1789610470000,"data":{"turn":1,"reason":{"kind":"completed"}}}
`
	r := parseDSHStream(context.Background(), strings.NewReader(head+noise+turn))
	if r.err != nil || len(r.facts) != 1 || len(r.recent) != 1 {
		t.Fatal("large valid stream", r.err, len(r.facts))
	}
	r = parseDSHStream(context.Background(), strings.NewReader(head+turn+"{"))
	if !errors.Is(r.err, errIncompleteTail) || len(r.facts) != 1 {
		t.Fatal("confirmed prefix lost", r.err)
	}
	r = parseDSHStream(context.Background(), strings.NewReader(head+strings.ReplaceAll(turn, `"completed"`, `"failed"`)))
	if r.err != nil || len(r.facts) != 0 || len(r.recent) != 1 {
		t.Fatal("failed turn counted", r)
	}
	r = parseDSHStream(context.Background(), strings.NewReader(head+strings.Repeat("x", (1<<20)+1)))
	if !errors.Is(r.err, errHistoryRestricted) {
		t.Fatal("line cap", r.err)
	}
	// Actual native Team hosts use a distinct non-human source, not a human RPC.
	for _, source := range []string{"clawmanager-team", "runtime-context", "time-context"} {
		r = parseDSHStream(context.Background(), strings.NewReader(head+strings.ReplaceAll(turn, `"kind":"user"`, `"kind":"`+source+`"`)))
		if r.err != nil || len(r.facts) != 0 || len(r.recent) != 0 {
			t.Fatal("automatic input counted as human", source, r.err)
		}
	}
}

func TestDSHFileIsolationAndBounds(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "deepseek-harness", "user-1", "instance-1")
	dir := filepath.Join(ws, "home", ".dsh", "sessions")
	if e := os.MkdirAll(filepath.Join(dir, "good"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(dir, "bad"), 0700); e != nil {
		t.Fatal(e)
	}
	good := `{"type":"session","id":"s"}
{"type":"turn/start","data":{"turn":1}}
{"type":"user/message","time":1789610460000,"data":{"id":"u","source":{"kind":"user"},"content":"hello"}}
{"type":"assistant/message","data":{"turn":1,"message":{"content":"ok"}}}
{"type":"turn/end","time":1789610470000,"data":{"turn":1,"reason":{"kind":"completed"}}}
`
	encoder, e := zstd.NewWriter(nil)
	if e != nil {
		t.Fatal(e)
	}
	defer encoder.Close()
	path := filepath.Join(dir, "good", "session.jsonl.zstd")
	if e = os.WriteFile(path, encoder.EncodeAll([]byte(good), nil), 0600); e != nil {
		t.Fatal(e)
	}
	bad := filepath.Join(dir, "bad", "session.jsonl.zstd")
	if e = os.WriteFile(bad, []byte("bad compression"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx := withDSHBatch(context.Background())
	i := Identity{1, "owner@example.test", 1, "deepseek-harness", "lite"}
	qs, e := readNativeHistory(ctx, root, ws, i, false)
	if !usablePartial(e) || len(qs) != 1 {
		t.Fatal("good sibling lost", e, len(qs))
	}
	cache := ctx.Value(dshBatchKey{}).(*dshBatchCache)
	if len(cache.files) != 1 {
		t.Fatal("good file not shared")
	}
	recent, e := readNativeHistory(ctx, root, ws, i, true)
	if !usablePartial(e) || len(recent) != 1 || len(cache.files) != 1 {
		t.Fatal("partial excerpt", e)
	}
	progress := &dshProgress{previous: map[string]string{}, confirmed: map[string]string{}}
	progressCtx := context.WithValue(ctx, dshProgressKey{}, progress)
	qs, e = readNativeHistory(progressCtx, root, ws, i, false)
	if len(qs) != 1 || len(progress.confirmed) != 1 || !usablePartial(e) {
		t.Fatal("successful file progress", e)
	}
	progress.previous = progress.confirmed
	progress.confirmed = map[string]string{}
	qs, e = readNativeHistory(progressCtx, root, ws, i, false)
	if len(qs) != 0 || len(progress.confirmed) != 1 || !usablePartial(e) {
		t.Fatal("confirmed sibling reparsed", e)
	}
	changed := context.WithValue(progressCtx, dshGenerationKey{}, "changed-generation")
	qs, e = readNativeHistory(changed, root, ws, i, false)
	if len(qs) != 1 || !usablePartial(e) {
		t.Fatal("new generation skipped", e)
	}
	qs, e = readNativeHistory(ctx, root, ws, i, false, time.Unix(1789610461, 0))
	if len(qs) != 0 {
		t.Fatal("T0 included earlier question")
	}
	r := parseDSHStream(context.Background(), strings.NewReader(good+strings.Repeat(`{"type":"noise","data":"`+strings.Repeat("x", 1000)+`"}`+"\n", 18000)))
	if !errors.Is(r.err, errHistoryRestricted) || len(r.facts) != 1 || r.bytes > (16<<20)+1 {
		t.Fatal("decoded bound", r.err, r.bytes)
	}
	if e = os.WriteFile(bad, make([]byte, (8<<20)+1), 0600); e != nil {
		t.Fatal(e)
	}
	r = readDSHFile(context.Background(), bad)
	if !errors.Is(r.err, errHistoryRestricted) {
		t.Fatal("compressed bound", r.err)
	}
}
