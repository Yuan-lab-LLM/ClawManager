package ownerrecords

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Uses a CLI-created source and the retained failed/converted state of this
// task's raw-recovery fixture. No hand-authored SQL events count as this proof.
func TestOpenClaw102RealMigratedSQLiteFixture(t *testing.T) {
	mode := os.Getenv("PERSONAL_TEST_REAL_SQLITE_HISTORY")
	if mode != "owned-recovery-volume" && mode != "owned-stopped-opened-volume" {
		t.Skip("requires explicit task-owned real CLI recovery volume")
	}
	root := "/workspaces"
	var owner struct {
		RunID string `json:"run_id"`
		Mode  string `json:"mode"`
	}
	raw, err := os.ReadFile(filepath.Join(root, ".fixture-volume-owner"))
	if err != nil || json.Unmarshal(raw, &owner) != nil || owner.Mode != "isolated_rehearsal" || !strings.HasPrefix(owner.RunID, "personal-openclaw-102-migration-20261016-") {
		t.Fatal("not an owned recovery fixture")
	}
	if run := os.Getenv("PERSONAL_TEST_REQUIRE_NFS42"); run != "" {
		if !strings.HasPrefix(run, "personal-openclaw-20261016-") && !strings.HasPrefix(run, "personal-openclaw-20261017-") {
			t.Fatal("unexpected NFS fixture run")
		}
		raw, err := os.ReadFile("/proc/self/mountinfo")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range strings.Split(string(raw), "\n") {
			left, right, ok := strings.Cut(line, " - ")
			if !ok {
				continue
			}
			l, r := strings.Fields(left), strings.Fields(right)
			if len(l) < 6 || l[4] != root {
				continue
			}
			owned := "/.clawmanager-upgrade-fixtures/" + run
			if len(r) < 3 || r[0] != "nfs4" || !strings.Contains(r[2], "vers=4.2,") || !strings.Contains(","+r[2]+",", ",hard,") || strings.Contains(r[2], "soft") {
				t.Fatal("requires real NFS4.2 hard mount")
			}
			if !(l[3] == owned && r[1] == "10.98.254.136:/" || l[3] == "/" && r[1] == "10.98.254.136:"+owned) {
				t.Fatal("requires only the selected owned subpath")
			}
			found = true
		}
		if !found {
			t.Fatal("selected NFS mount missing")
		}
	}
	capsule := filepath.Join(root, ".clawmanager-openclaw-capsules", "operation-fixture", "user-1-instance-17.json.capsule")
	original := filepath.Join(capsule, "original", "home", ".openclaw", "agents", "main", "sessions", "7d39bc8b-7017-4bd5-9253-7f2e05bb7d19.jsonl")
	source, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	var expected []json.RawMessage
	for _, line := range bytes.Split(source, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) != 0 {
			expected = append(expected, append(json.RawMessage{}, line...))
		}
	}
	if len(expected) < 6 {
		t.Fatal("source was not created by the actual CLI fixture")
	}
	agent := filepath.Join(capsule, "recovery", "failed", "home", ".openclaw", "agents", "main")
	if mode == "owned-stopped-opened-volume" {
		release, err := lockOwnedOpenClawHistoryFixture(root)
		if err != nil {
			t.Fatal("actual target writer is not closed", err)
		}
		defer release()
		var h struct {
			Phase       string `json:"phase"`
			DataVersion string `json:"data_version"`
			Opened      bool   `json:"business_may_have_opened"`
		}
		raw, err := os.ReadFile(filepath.Join(root, ".clawmanager-openclaw-handoffs", "user-1-instance-17.json"))
		if err != nil || json.Unmarshal(raw, &h) != nil || !h.Opened || h.DataVersion != "2026.10.2" || (h.Phase != "opening" && h.Phase != "active" && h.Phase != "completed") {
			t.Fatal("exact opened owned target required")
		}
		agent = filepath.Join(root, "openclaw", "user-1", "instance-17", "home", ".openclaw", "agents", "main")
	}
	db := filepath.Join(agent, "agent", "openclaw-agent.sqlite")
	if err := checkHistoryPath(root, db); err != nil {
		t.Fatal(err)
	}
	if err := checkOpenClawSQLiteNetworkMode(db); err != nil {
		t.Fatal("migrated network database is not safe for a read-only local projection", err)
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	readDB := db
	if network, err := openClawSQLiteNetworkFilesystem(db); err != nil {
		t.Fatal(err)
	} else if !network {
		// This retained fixture is stopped and immutable. A closed local WAL
		// database may need a new SHM even with mode=ro. Copy its entire stopped
		// family into private test scratch rather than changing the retained
		// original or using immutable=1 to ignore a real WAL. NFS verification
		// below reads the actual rollback-journal source directly, not this copy.
		readDB = filepath.Join(t.TempDir(), "openclaw-agent.sqlite")
		for _, suffix := range []string{"", "-wal"} {
			input, err := os.Open(db + suffix)
			if os.IsNotExist(err) && suffix != "" {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.OpenFile(readDB+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				input.Close()
				t.Fatal(err)
			}
			_, copyErr := io.Copy(output, input)
			input.Close()
			closeErr := output.Close()
			if copyErr != nil || closeErr != nil {
				t.Fatal(copyErr, closeErr)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err = readHistorySQLite(ctx, readDB, openClaw102SnapshotSQL(time.Time{}), openClaw102ReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Snapshot string `json:"snapshot"`
	}
	var snapshot openClaw102Snapshot
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 || json.Unmarshal([]byte(rows[0].Snapshot), &snapshot) != nil {
		t.Fatal("actual snapshot did not match the pinned read contract")
	}
	if len(snapshot.Cold) != 0 || len(snapshot.Hot) != len(expected) {
		t.Fatal("actual imported event coverage changed", len(snapshot.Hot), len(expected))
	}
	sort.Slice(snapshot.Hot, func(i, j int) bool { return snapshot.Hot[i].Seq < snapshot.Hot[j].Seq })
	z, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(32<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	compressed := 0
	for n, e := range snapshot.Hot {
		var event []byte
		if e.Plain != nil {
			event = []byte(*e.Plain)
		} else {
			b, err := hex.DecodeString(e.Compressed)
			if err != nil {
				t.Fatal(err)
			}
			event, err = z.DecodeAll(b, nil)
			if err != nil {
				t.Fatal(err)
			}
			compressed++
		}
		var actualCanonical, sourceCanonical bytes.Buffer
		if json.Compact(&actualCanonical, event) != nil || json.Compact(&sourceCanonical, expected[n]) != nil || !bytes.Equal(actualCanonical.Bytes(), sourceCanonical.Bytes()) {
			t.Fatal("actual migrated message identity/content/order changed", n)
		}
	}
	// Recovery fixtures were deliberately seeded with a large real CLI event.
	// The separate live-start/restart fixture also covers small plaintext input;
	// do not mislabel that as compression acceptance. Its NFS large-event variant
	// explicitly requires compressed coverage, in addition to exact content.
	if compressed == 0 && (mode == "owned-recovery-volume" || os.Getenv("PERSONAL_TEST_REQUIRE_COMPRESSED_HISTORY") == "real-large-source") {
		t.Fatal("the fixture did not exercise actual compressed transcript events")
	}
	budget := int64(openClaw102ReadBytes)
	got, err := decodeOpenClaw102Snapshot(ctx, root, agent, snapshot, false, time.Time{}, &budget)
	legacy, legacyErr := ParseNativeLog("openclaw", source)
	if err != nil || legacyErr != nil || len(got) != len(legacy) {
		t.Fatal("native accounting diverged after actual migration", err, legacyErr)
	}
	for n := range got {
		if got[n] != legacy[n] {
			t.Fatal("question identity changed after migration")
		}
	}
	after, _ := os.ReadFile(db)
	if !bytes.Equal(before, after) {
		t.Fatal("read-only projection modified actual migrated database")
	}
	t.Logf("real 7.1 -> 10.2: %d original logical events match; %d compressed; %d owner-confirmed questions on both sides (CLI fixture has no portal owner attestation); no model calls", len(expected), compressed, len(got))
}
