package ownerrecords

// Pinned 2026.10.2 read contract, derived from transcript-payload and
// session-cold-storage-codec in that exact package. No OpenClaw CLI/Doctor is
// launched, no cold transcript is restored into the live database. Unknown
// schemas fail closed; a retired JSONL is never treated as an empty session.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const openClaw102ReadBytes = 16 << 20

type openClaw102Event struct {
	Session    string  `json:"session"`
	Seq        int64   `json:"seq"`
	Plain      *string `json:"plain"`
	Compressed string  `json:"compressed"`
	Bytes      int64   `json:"bytes"`
	Oversized  bool    `json:"oversized"`
}
type openClaw102Cold struct {
	Session      string `json:"session"`
	Generation   string `json:"generation"`
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	Events       int    `json:"events"`
	RawBytes     int64  `json:"raw_bytes"`
	ArchiveBytes int64  `json:"archive_bytes"`
	LastSeq      int64  `json:"last_seq"`
	Storage      string `json:"storage"`
	Blob         string `json:"blob"`
}
type openClaw102Snapshot struct {
	Sessions []string           `json:"sessions"`
	Hot      []openClaw102Event `json:"hot"`
	Cold     []openClaw102Cold  `json:"cold"`
}

func readHistorySQLite(ctx context.Context, path, query string, limit int) ([]byte, error) {
	return readHistorySQLiteOutput(ctx, path, query, limit, "-json")
}

func readHistorySQLiteOutput(ctx context.Context, path, query string, limit int, mode string) ([]byte, error) {
	// busy_timeout emits a row, so set it through the shell's non-outputting
	// .timeout instead of accidentally decoding two JSON documents.
	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-noheader", mode, "-cmd", ".timeout 100", path)
	cmd.Stdin = strings.NewReader("PRAGMA query_only=ON; " + query)
	out, diagnostic := &cappedOutput{max: limit}, &cappedOutput{max: 4096}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Do not leak paths, prompts, plugin content, or raw SQLite diagnostics.
		if errors.Is(err, errHistoryRestricted) || strings.Contains(diagnostic.String(), "no such table") || strings.Contains(diagnostic.String(), "no such column") || out.Len() >= limit {
			return nil, errHistoryRestricted
		}
		return nil, errors.New("OpenClaw SQLite history could not be read")
	}
	return out.Bytes(), nil
}

func openClaw102SnapshotSQL(cutoff time.Time, cached ...string) string {
	minimum := int64(0)
	if !cutoff.IsZero() {
		minimum = cutoff.UnixMilli()
	}
	// The first 201/10001 are deliberate overflow sentinels, not silent
	// pagination. One SELECT keeps hot/cold ownership in the same read snapshot.
	includeBlob := "1"
	if len(cached) > 0 && cached[0] != "" {
		includeBlob = "NOT (" + cached[0] + ")"
	}
	return fmt.Sprintf(`WITH chosen AS (SELECT session_id FROM session_windows WHERE updated_at>=%d LIMIT 201),
	 hot AS (SELECT e.session_id,e.seq,e.event_json,e.event_zstd,e.event_utf8_bytes FROM chosen c CROSS JOIN transcript_events e INDEXED BY sqlite_autoindex_transcript_events_1 ON e.session_id=c.session_id LIMIT 10001),
	 cold AS (SELECT a.* FROM chosen c CROSS JOIN session_transcript_cold_archives a ON a.session_id=c.session_id LIMIT 201)
 SELECT json_object('sessions',(SELECT json_group_array(session_id) FROM chosen),
 'hot',(SELECT json_group_array(json_object('session',session_id,'seq',seq,'plain',CASE WHEN length(CAST(event_json AS BLOB))<=4194304 THEN event_json END,'compressed',CASE WHEN length(event_zstd)<=4194304 THEN hex(event_zstd) ELSE '' END,'bytes',COALESCE(event_utf8_bytes,0),'oversized',json(CASE WHEN length(CAST(event_json AS BLOB))>4194304 OR length(event_zstd)>4194304 THEN 'true' ELSE 'false' END))) FROM hot),
 'cold',(SELECT json_group_array(json_object('session',session_id,'generation',generation,'name',archive_name,'sha256',archive_sha256,'events',event_count,'raw_bytes',raw_bytes,'archive_bytes',archive_bytes,'last_seq',last_seq,'storage',storage,'blob',CASE WHEN %s AND length(archive_blob)<=8388608 THEN hex(archive_blob) ELSE '' END)) FROM cold)) AS snapshot;`, minimum, includeBlob)
}

func openClaw102AcknowledgedSQL(ctx context.Context, agent string) string {
	p, _ := ctx.Value(openClawHistoryProgressKey{}).(*openClawColdProgress)
	if p == nil {
		return ""
	}
	var terms []string
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	for _, a := range p.previous {
		if a.AgentKey == Key(agent) && len(a.Session) <= 255 && len(a.Generation) <= 128 && len(a.SHA256) == 64 {
			terms = append(terms, "(session_id="+quote(a.Session)+" AND generation="+quote(a.Generation)+" AND archive_sha256="+quote(a.SHA256)+")")
		}
		if len(terms) > 200 {
			return ""
		}
	}
	sort.Strings(terms)
	return strings.Join(terms, " OR ")
}

// Returns the agent directories whose native SQLite history is authoritative.
// Their retained/import JSONL is not scanned again. Old agent stores without
// native transcript tables continue on the unchanged 7.1 reader.
func readOpenClawSQLiteAgents(ctx context.Context, root, base string, recent bool, cutoff ...time.Time) ([]NativeQuestion, map[string]bool, error) {
	skip := map[string]bool{}
	dirs, err := os.ReadDir(base)
	if err != nil {
		return nil, skip, err
	}
	if len(dirs) > 200 {
		return nil, skip, errHistoryRestricted
	}
	var result []NativeQuestion
	budget := int64(openClaw102ReadBytes)
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, skip, err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil, skip, errHistoryRestricted
		}
		if !d.IsDir() {
			continue
		}
		agent := filepath.Join(base, d.Name())
		db := filepath.Join(agent, "agent", "openclaw-agent.sqlite")
		if _, err := os.Lstat(db); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, skip, err
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if _, err := os.Lstat(db + suffix); os.IsNotExist(err) && suffix != "" {
				continue
			}
			if err := checkHistoryPath(root, db+suffix); err != nil {
				return nil, skip, err
			}
		}
		if err := checkOpenClawSQLiteNetworkMode(db); err != nil {
			return nil, skip, err
		}
		raw, err := readHistorySQLite(ctx, db, `SELECT name FROM sqlite_master WHERE type='table' AND name IN ('transcript_events','session_windows','session_transcript_cold_archives');`, 4096)
		if err != nil {
			return nil, skip, err
		}
		var tables []struct {
			Name string `json:"name"`
		}
		if len(bytes.TrimSpace(raw)) != 0 && json.Unmarshal(raw, &tables) != nil {
			return nil, skip, errHistoryRestricted
		}
		if len(tables) == 0 {
			continue
		}
		if len(tables) != 3 {
			return nil, skip, errHistoryRestricted
		}
		skip[agent] = true
		start := time.Time{}
		if !recent && len(cutoff) > 0 {
			start = cutoff[0]
		}
		// This SELECT already returns exactly one JSON object. SQLite 3.48's
		// -json mode re-escapes that whole value with many small writes, exhausting
		// the reader budget on ordinary multi-megabyte transcripts. Raw list
		// output preserves the same atomic, bounded snapshot without the wrapper.
		raw, err = readHistorySQLiteOutput(ctx, db, openClaw102SnapshotSQL(start, openClaw102AcknowledgedSQL(ctx, agent)), openClaw102ReadBytes, "-list")
		if err != nil {
			return nil, skip, err
		}
		var snapshot openClaw102Snapshot
		if json.Unmarshal(raw, &snapshot) != nil {
			return nil, skip, errHistoryRestricted
		}
		qs, err := decodeOpenClaw102Snapshot(ctx, root, agent, snapshot, recent, start, &budget)
		if err != nil {
			return nil, skip, err
		}
		result = append(result, qs...)
		if len(result) > 10000 {
			return nil, skip, errHistoryRestricted
		}
	}
	return result, skip, nil
}

func decodeOpenClaw102Snapshot(ctx context.Context, root, agent string, s openClaw102Snapshot, recent bool, cutoff time.Time, budget *int64) ([]NativeQuestion, error) {
	if len(s.Sessions) > 200 || len(s.Hot) > 10000 || len(s.Cold) > 200 {
		return nil, errHistoryRestricted
	}
	known := map[string]bool{}
	for _, id := range s.Sessions {
		if id == "" || known[id] {
			return nil, errHistoryCorrupt
		}
		known[id] = true
	}
	logs := map[string]*bytes.Buffer{}
	progress, _ := ctx.Value(openClawHistoryProgressKey{}).(*openClawColdProgress)
	var cachedRecent []NativeQuestion
	type pendingCold struct{ key, session, generation, sha string }
	var pending []pendingCold
	last := map[string]int64{}
	appendEvent := func(session string, seq int64, event []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !known[session] || len(event) > 1<<20 || !json.Valid(event) || bytes.IndexByte(event, '\n') >= 0 {
			return errHistoryRestricted
		}
		if _, ok := last[session]; ok && seq <= last[session] {
			return errHistoryCorrupt
		}
		*budget -= int64(len(event) + 1)
		if *budget < 0 {
			return errHistoryRestricted
		}
		if logs[session] == nil {
			logs[session] = new(bytes.Buffer)
		}
		logs[session].Write(event)
		logs[session].WriteByte('\n')
		last[session] = seq
		return nil
	}
	z, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(32<<20), zstd.WithDecodeAllCapLimit(true))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	// Do not add a global SQL sort that must visit every matching event before
	// LIMIT. Fetch a bounded overflow sentinel with indexed per-session seeks,
	// then restore logical order in this already bounded in-memory page.
	sort.Slice(s.Hot, func(i, j int) bool {
		if s.Hot[i].Session != s.Hot[j].Session {
			return s.Hot[i].Session < s.Hot[j].Session
		}
		return s.Hot[i].Seq < s.Hot[j].Seq
	})
	for _, e := range s.Hot {
		if e.Oversized {
			return nil, errHistoryRestricted
		}
		var event []byte
		if e.Plain != nil && e.Compressed == "" {
			event = []byte(*e.Plain)
		} else if e.Plain == nil && e.Compressed != "" && e.Bytes > 0 && e.Bytes <= 1<<20 {
			compressed, err := hex.DecodeString(e.Compressed)
			if err != nil || len(compressed) > 4<<20 {
				return nil, errHistoryCorrupt
			}
			event, err = z.DecodeAll(compressed, make([]byte, 0, int(e.Bytes)))
			if err != nil || int64(len(event)) != e.Bytes {
				return nil, errHistoryCorrupt
			}
		} else {
			return nil, errHistoryCorrupt
		}
		if err := appendEvent(e.Session, e.Seq, event); err != nil {
			return nil, err
		}
	}
	for _, c := range s.Cold {
		sha, shaErr := hex.DecodeString(c.SHA256)
		if !known[c.Session] || logs[c.Session] != nil || c.Generation == "" || c.Events < 1 || c.Events > 10000 || c.ArchiveBytes < 1 || c.ArchiveBytes > 8<<20 || c.RawBytes < 1 || c.RawBytes > openClaw102ReadBytes || shaErr != nil || len(sha) != 32 || c.SHA256 != strings.ToLower(c.SHA256) || c.Name != c.SHA256+".jsonl.zst" {
			return nil, errHistoryRestricted
		}
		// A canonical cold generation is immutable. Include external artifact
		// metadata so disappearance/replacement is not masked by a cached SHA.
		cacheKey := Key("oc102-cold-v1", agent, c.Session, c.Generation, c.SHA256, c.Storage)
		if c.Storage == "file" {
			p := filepath.Join(agent, "sessions", "cold", c.Name)
			if err := checkHistoryPath(root, p); err != nil {
				return nil, err
			}
			info, err := os.Lstat(p)
			if err != nil {
				return nil, err
			}
			cacheKey = Key(cacheKey, fmt.Sprintf("%d:%d:%v", info.Size(), info.ModTime().UnixNano(), info.Mode()))
		}
		if progress != nil {
			if a, ok := progress.previous[cacheKey]; ok && a.AgentKey == Key(agent) && a.Session == c.Session && a.Generation == c.Generation && a.SHA256 == c.SHA256 {
				progress.confirmed[cacheKey] = a
				if recent && a.Latest.ID != "" {
					cachedRecent = append(cachedRecent, a.Latest)
				}
				continue
			}
		}
		var compressed []byte
		if c.Storage == "sqlite" {
			compressed, err = hex.DecodeString(c.Blob)
		} else if c.Storage == "file" && c.Blob == "" {
			p := filepath.Join(agent, "sessions", "cold", c.Name)
			if err = checkHistoryPath(root, p); err != nil {
				return nil, err
			}
			var dir *os.Root
			dir, err = os.OpenRoot(agent)
			if err == nil {
				var f *os.File
				f, err = dir.Open(filepath.Join("sessions", "cold", c.Name))
				if err == nil {
					info, statErr := f.Stat()
					if statErr != nil || !info.Mode().IsRegular() || info.Size() != c.ArchiveBytes {
						err = errHistoryCorrupt
					} else {
						compressed, err = io.ReadAll(io.LimitReader(historyContextReader{ctx, f}, c.ArchiveBytes+1))
					}
					f.Close()
				}
				dir.Close()
			}
		} else {
			return nil, errHistoryCorrupt
		}
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(compressed)
		if int64(len(compressed)) != c.ArchiveBytes || hex.EncodeToString(hash[:]) != c.SHA256 {
			return nil, errHistoryCorrupt
		}
		// Cold envelopes contain identities/projections in addition to raw
		// events. Bound the envelope independently; do not restore or warm it.
		envelope, err := z.DecodeAll(compressed, make([]byte, 0, openClaw102ReadBytes))
		if err != nil {
			return nil, errHistoryRestricted
		}
		*budget -= int64(len(envelope))
		if *budget < 0 {
			return nil, errHistoryRestricted
		}
		scanner := bufio.NewScanner(bytes.NewReader(envelope))
		scanner.Buffer(make([]byte, 4096), 2<<20)
		header, events, rawBytes := false, 0, int64(0)
		for scanner.Scan() {
			var p struct {
				Kind       string `json:"kind"`
				Version    int    `json:"version"`
				Session    string `json:"sessionId"`
				Generation string `json:"generation"`
				Row        struct {
					Seq   int64  `json:"seq"`
					Event string `json:"event_json"`
				} `json:"row"`
			}
			if json.Unmarshal(scanner.Bytes(), &p) != nil {
				return nil, errHistoryCorrupt
			}
			if !header {
				if p.Kind != "header" || p.Version != 1 || p.Session != c.Session || p.Generation != c.Generation {
					return nil, errHistoryCorrupt
				}
				header = true
				continue
			}
			switch p.Kind {
			case "event":
				if err := appendEvent(c.Session, p.Row.Seq, []byte(p.Row.Event)); err != nil {
					return nil, err
				}
				events++
				rawBytes += int64(len(p.Row.Event))
				if events > 1 {
					rawBytes++
				}
			case "identity", "active", "index", "fts": // auxiliary metadata, not messages
			default:
				return nil, errHistoryCorrupt
			}
		}
		if scanner.Err() != nil || !header || events != c.Events || rawBytes != c.RawBytes || last[c.Session] != c.LastSeq {
			return nil, errHistoryCorrupt
		}
		pending = append(pending, pendingCold{cacheKey, c.Session, c.Generation, c.SHA256})
	}
	var result []NativeQuestion
	result = append(result, cachedRecent...)
	ids := make([]string, 0, len(logs))
	for id := range logs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		data := logs[id].Bytes()
		var first struct{ Type, ID string }
		line, _, _ := bytes.Cut(data, []byte{'\n'})
		if json.Unmarshal(line, &first) != nil || first.Type != "session" || first.ID != id {
			return nil, errHistoryCorrupt
		}
		parse := ParseNativeLog
		if recent {
			parse = parseRecentNativeLog
		}
		qs, err := parse("openclaw", data)
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			if !recent && !cutoff.IsZero() && q.Submitted < float64(cutoff.UnixMilli())/1000 {
				continue
			}
			result = append(result, q)
		}
	}
	if progress != nil {
		for _, c := range pending {
			qs, err := parseRecentNativeLog("openclaw", logs[c.session].Bytes())
			if err != nil {
				return nil, err
			}
			var latest NativeQuestion
			for _, q := range qs {
				if q.At > latest.At || q.At == latest.At && q.ID > latest.ID {
					latest = q
				}
			}
			progress.confirmed[c.key] = openClawColdAcknowledgement{Latest: latest, AgentKey: Key(agent), Session: c.session, Generation: c.generation, SHA256: c.sha}
		}
	}
	return result, nil
}
