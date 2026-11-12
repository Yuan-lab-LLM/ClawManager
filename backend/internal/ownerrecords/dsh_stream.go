package ownerrecords

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/klauspost/compress/zstd"
)

var errHistoryCorrupt = errors.New("invalid native history format")

type partialHistoryError struct{ err error }

func (e *partialHistoryError) Error() string { return "partial native history: " + e.err.Error() }
func (e *partialHistoryError) Unwrap() error { return e.err }
func usablePartial(err error) bool           { var p *partialHistoryError; return errors.As(err, &p) }

type dshResult struct {
	facts, recent []NativeQuestion
	bytes         int64
	err           error
}
type dshBatchKey struct{}
type dshBatchCache struct {
	files          map[string]dshResult
	facts          int
	inspected      map[string]historyReadResponse
	inspectedFacts int
}
type dshGenerationKey struct{}

// Cache lives for one serial worker pass, never across ownership/generation changes.
func withDSHBatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, dshBatchKey{}, &dshBatchCache{files: map[string]dshResult{}})
}

func parseDSHStream(ctx context.Context, reader io.Reader) dshResult {
	return parseDSHStreamLimit(ctx, reader, 16<<20)
}
func parseDSHStreamLimit(ctx context.Context, reader io.Reader, limit int64) dshResult {
	result := dshResult{}
	limited := &io.LimitedReader{R: historyContextReader{ctx, reader}, N: limit + 1}
	lines := bufio.NewReaderSize(limited, 64<<10)
	session, turn := "", ""
	var pending []NativeQuestion
	good := false
	events := 0
	for {
		var line []byte
		for {
			part, prefix, e := lines.ReadLine()
			line = append(line, part...)
			if len(line) > 1<<20 {
				result.err = errHistoryRestricted
				break
			}
			if e != nil {
				if e != io.EOF {
					result.err = e
				}
				if len(line) == 0 {
					result.bytes = limit + 1 - limited.N
					if result.bytes > limit {
						result.err = errHistoryRestricted
					}
					return result
				}
			}
			if !prefix {
				break
			}
		}
		result.bytes = limit + 1 - limited.N
		if result.bytes > limit {
			result.err = errHistoryRestricted
		}
		if result.err != nil {
			return result
		}
		if err := ctx.Err(); err != nil {
			result.err = err
			return result
		}
		if len(line) == 0 {
			continue
		}
		events++
		if events > 100000 {
			result.err = errHistoryRestricted
			return result
		}
		var header struct{ Type string }
		if e := json.Unmarshal(line, &header); e != nil {
			if _, peekErr := lines.Peek(1); peekErr == io.EOF {
				result.err = errIncompleteTail
			} else {
				result.err = errHistoryCorrupt
			}
			return result
		}
		switch header.Type {
		case "session", "turn/start", "user/message", "assistant/message", "turn/end":
		default:
			continue
		}
		var p struct {
			Type, ID        string
			Time            float64
			DelegationDepth int `json:"delegationDepth"`
			Data            struct {
				Turn    json.RawMessage
				ID      string
				Content json.RawMessage
				Source  struct{ Kind string }
				Message struct{ Content json.RawMessage }
				Reason  struct{ Kind string }
			}
		}
		if err := json.Unmarshal(line, &p); err != nil {
			// A malformed last append is retried; other corruption is never skipped.
			if _, peekErr := lines.Peek(1); peekErr == io.EOF {
				result.err = errIncompleteTail
			} else {
				result.err = errHistoryCorrupt
			}
			return result
		}
		switch p.Type {
		case "session":
			session = p.ID
			if p.DelegationDepth > 0 {
				return dshResult{bytes: result.bytes}
			}
		case "turn/start":
			turn = string(p.Data.Turn)
			pending = nil
			good = false
		case "user/message":
			if session != "" && p.Data.Source.Kind == "user" && p.Data.ID != "" && hasText(p.Data.Content) {
				q := NativeQuestion{Session: session, ID: p.Data.ID, Submitted: p.Time / 1000, At: p.Time / 1000, Text: questionExcerpt(humanText(p.Data.Content))}
				if q.At > 0 && (len(result.recent) == 0 || q.At >= result.recent[0].At) {
					result.recent = []NativeQuestion{q}
				}
				q.Text = ""
				q.At = 0
				if turn != "" {
					pending = append(pending, q)
				}
			}
		case "assistant/message":
			if turn != "" && string(p.Data.Turn) == turn && hasText(p.Data.Message.Content) {
				good = true
			}
		case "turn/end":
			if turn != "" && string(p.Data.Turn) == turn {
				if good && p.Data.Reason.Kind == "completed" {
					for _, q := range pending {
						q.At = p.Time / 1000
						result.facts = append(result.facts, q)
					}
				}
				turn = ""
				pending = nil
				good = false
			}
		}
		if len(pending) > 1000 || len(result.facts) > 10000 {
			result.err = errHistoryRestricted
			return result
		}
	}
}

func readDSHFile(ctx context.Context, path string, remaining ...int64) dshResult {
	f, e := os.Open(path)
	if e != nil {
		return dshResult{err: e}
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return dshResult{err: e}
	}
	if st.Size() > 8<<20 {
		return dshResult{err: errHistoryRestricted}
	}
	input := &io.LimitedReader{R: historyContextReader{ctx, f}, N: (8 << 20) + 1}
	z, e := zstd.NewReader(input, zstd.WithDecoderMaxMemory(32<<20), zstd.WithDecoderConcurrency(1))
	if e != nil {
		return dshResult{err: errHistoryCorrupt}
	}
	defer z.Close()
	limit := int64(16 << 20)
	if len(remaining) > 0 && remaining[0] < limit {
		limit = remaining[0]
	}
	r := parseDSHStreamLimit(ctx, z, limit)
	if input.N <= 0 {
		r.err = errHistoryRestricted
	}
	if r.err != nil && !errors.Is(r.err, context.DeadlineExceeded) && !errors.Is(r.err, context.Canceled) && !errors.Is(r.err, errHistoryRestricted) && !errors.Is(r.err, errIncompleteTail) {
		r.err = errHistoryCorrupt
	}
	return r
}

func readDSHHistory(ctx context.Context, root, workspace string, i Identity, recent bool, cutoff ...time.Time) ([]NativeQuestion, error) {
	base := filepath.Join(workspace, "home/.dsh/sessions")
	if e := checkHistoryPath(root, base); e != nil {
		return nil, e
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	entries := 0
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > 2000 {
			return errHistoryRestricted
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errHistoryCorrupt
		}
		if !d.IsDir() && d.Name() == "session.jsonl.zstd" {
			st, e := d.Info()
			if e != nil {
				return e
			}
			if len(cutoff) == 0 || !st.ModTime().Before(cutoff[0]) {
				files = append(files, file{path, st.Size(), st.ModTime()})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Recent activity first; stable tie break. A failed file doesn't abort siblings.
	sort.Slice(files, func(a, b int) bool {
		if files[a].at.Equal(files[b].at) {
			return files[a].path < files[b].path
		}
		return files[a].at.After(files[b].at)
	})
	cache, _ := ctx.Value(dshBatchKey{}).(*dshBatchCache)
	generation, _ := ctx.Value(dshGenerationKey{}).(string)
	progress, _ := ctx.Value(dshProgressKey{}).(*dshProgress)
	var result []NativeQuestion
	var failed error
	var decoded, input int64
	for index, f := range files {
		if index >= 200 || decoded >= 32<<20 || input >= 16<<20 {
			failed = errHistoryRestricted
			break
		}
		if f.size > (16<<20)-input {
			failed = errHistoryRestricted
			continue
		}
		if e := ctx.Err(); e != nil {
			failed = e
			break
		}
		if e := checkHistoryPath(root, f.path); e != nil {
			failed = e
			continue
		}
		key := fmt.Sprintf("%s|%s|%d|%d|%d|%s", f.path, i.Owner, i.Scope, f.size, f.at.UnixNano(), generation)
		fileKey, fingerprint := Key(f.path), Key(key)
		if !recent && progress != nil && progress.previous[fileKey] == fingerprint {
			progress.confirmed[fileKey] = fingerprint
			continue
		}
		var r dshResult
		ok := false
		if cache != nil {
			r, ok = cache.files[key]
		}
		if ok && r.bytes > (32<<20)-decoded {
			failed = errHistoryRestricted
			continue
		}
		if !ok {
			r = readDSHFile(ctx, f.path, (32<<20)-decoded)
			if r.err == nil && cache != nil && len(cache.files) < 200 && cache.facts+len(r.facts) <= 20000 {
				if after, e := os.Stat(f.path); e == nil && after.Size() == f.size && after.ModTime().Equal(f.at) {
					cache.files[key] = r
					cache.facts += len(r.facts)
				}
			}
		}
		decoded += r.bytes
		input += f.size
		if r.err == nil && !recent && progress != nil {
			if after, e := os.Stat(f.path); e == nil && after.Size() == f.size && after.ModTime().Equal(f.at) {
				progress.confirmed[fileKey] = fingerprint
			}
		}
		if r.err != nil {
			failed = r.err
		}
		qs := r.facts
		if recent {
			qs = r.recent
		}
		for _, q := range qs {
			if !recent && len(cutoff) > 0 && (q.Submitted <= 0 || q.Submitted < float64(cutoff[0].UnixMilli())/1000) {
				continue
			}
			result = append(result, q)
		}
		if len(result) > 10000 {
			failed = errHistoryRestricted
			break
		}
	}
	if failed != nil {
		return result, &partialHistoryError{failed}
	}
	return result, nil
}
