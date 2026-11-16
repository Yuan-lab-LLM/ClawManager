package ownerrecords

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const HistoryReaderArgument = "--clawreef-history-read"

var errReaderBusy = errors.New("previous filesystem reader has not exited")

// An unreaped D-state process keeps its slot. Timeouts never create an unbounded
// collection of stuck readers. The SQL-only scheduler remains independent.
var nativeReaderSlots = make(chan struct{}, 1)

// Call after cancelling worker contexts during clean shutdown. Waiting is
// bounded; a genuinely unreaped child retains its durable guard, not a timer.
func DrainHistoryReaders(ctx context.Context) bool {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for len(nativeReaderSlots) > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
	return true
}

type historyReadRequest struct {
	Operation        string
	Root, Workspace  string
	Identity         Identity
	Recent           bool
	Cutoff           time.Time
	Generation       string
	Previous         map[string]string
	Fingerprint      string
	OpenClawPrevious map[string]openClawColdAcknowledgement
}
type historyReadResponse struct {
	Fingerprint       string
	Questions         []NativeQuestion
	Confirmed         map[string]string
	Empty             bool
	Reason            string
	Partial           bool
	Unchanged         bool
	Facts             []NativeQuestion
	FactReason        string
	FactPartial       bool
	OpenClawConfirmed map[string]openClawColdAcknowledgement
}

// Invoked explicitly before normal server startup. It never loads configuration,
// opens a DB, serves HTTP, or writes a workspace. Input/output have strict caps.
func RunHistoryReaderCLI(input io.Reader, output io.Writer) int {
	return RunHistoryReaderContext(context.Background(), input, output)
}

// RunHistoryReaderContext also runs inside the upstream credential-scoped NFS helper.
func RunHistoryReaderContext(parent context.Context, input io.Reader, output io.Writer) int {
	parent = context.WithValue(parent, nativeHistoryHelperContextKey{}, true)
	var request historyReadRequest
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return 2
	}
	expected := filepath.Join(request.Root, request.Identity.Runtime, fmt.Sprintf("user-%d", request.Identity.Scope), fmt.Sprintf("instance-%d", request.Identity.Instance))
	if !filepath.IsAbs(request.Root) || filepath.Clean(request.Workspace) != expected {
		return 2
	}
	ctx, cancel := context.WithTimeout(parent, nativeReadBudget)
	defer cancel()
	ctx = context.WithValue(withDSHBatch(ctx), dshGenerationKey{}, request.Generation)
	response := historyReadResponse{}
	var err error
	switch request.Operation {
	case "fingerprint":
		response.Fingerprint, err = historyFingerprint(ctx, request.Root, request.Workspace, request.Identity)
	case "read", "inspect":
		if request.Operation == "inspect" {
			response.Fingerprint, err = historyFingerprint(ctx, request.Root, request.Workspace, request.Identity)
			if err != nil {
				break
			}
			if request.Fingerprint != "" && response.Fingerprint == request.Fingerprint {
				response.Unchanged = true
				break
			}
		}
		progress := &dshProgress{previous: request.Previous, confirmed: map[string]string{}}
		ctx = context.WithValue(ctx, dshProgressKey{}, progress)
		coldProgress := &openClawColdProgress{previous: request.OpenClawPrevious, confirmed: map[string]openClawColdAcknowledgement{}}
		ctx = context.WithValue(ctx, openClawHistoryProgressKey{}, coldProgress)
		if request.Cutoff.IsZero() {
			response.Questions, err = readNativeHistory(ctx, request.Root, request.Workspace, request.Identity, request.Recent)
		} else {
			response.Questions, err = readNativeHistory(ctx, request.Root, request.Workspace, request.Identity, request.Recent, request.Cutoff)
		}
		response.Confirmed = progress.confirmed
		response.OpenClawConfirmed = coldProgress.confirmed
		if request.Operation == "inspect" {
			if request.Recent && request.Identity.Runtime == "deepseek-harness" {
				var factErr error
				response.Facts, factErr = readNativeHistory(ctx, request.Root, request.Workspace, request.Identity, false, request.Cutoff)
				response.FactReason, response.FactPartial = syncReason(factErr), usablePartial(factErr)
				response.Confirmed = progress.confirmed
			}
			if after, e := historyFingerprint(ctx, request.Root, request.Workspace, request.Identity); e != nil || after != response.Fingerprint {
				response.Fingerprint = ""
			}
		}
	case "empty":
		response.Empty, err = probeEmptyHistory(request)
	default:
		return 2
	}
	response.Reason = syncReason(err)
	response.Partial = usablePartial(err)
	raw, encodeErr := json.Marshal(response)
	if encodeErr != nil || len(raw) > 8<<20 {
		return 2
	}
	if _, err = output.Write(raw); err != nil {
		return 2
	}
	return 0
}

func probeEmptyHistory(r historyReadRequest) (bool, error) {
	home := filepath.Join(r.Workspace, "home")
	if err := checkHistoryPath(r.Root, home); err != nil {
		return false, err
	}
	dir, err := os.Open(home)
	if err != nil {
		return false, err
	}
	_, err = dir.Readdirnames(1)
	dir.Close()
	if err != nil && err != io.EOF {
		return false, err
	}
	rel, ok := map[string]string{"hermes": "home/.hermes/state.db", "opencode": "home/.local/share/opencode/opencode.db", "openclaw": "home/.openclaw/agents", "deepseek-harness": "home/.dsh/sessions"}[r.Identity.Runtime]
	if !ok {
		return false, errors.New("unsupported history")
	}
	err = checkHistoryPath(r.Root, filepath.Join(r.Workspace, filepath.FromSlash(rel)))
	return errors.Is(err, os.ErrNotExist), nil
}

func readResponseError(response historyReadResponse) error {
	var err error
	switch response.Reason {
	case "":
		return nil
	case "missing":
		err = os.ErrNotExist
	case "permission":
		err = os.ErrPermission
	case "budget":
		err = context.DeadlineExceeded
	case "restricted":
		err = errHistoryRestricted
	case "invalid_format":
		err = errHistoryCorrupt
	case "appending":
		err = errIncompleteTail
	default:
		err = errors.New("isolated history reader failed")
	}
	if response.Partial {
		return &partialHistoryError{err}
	}
	return err
}

func (s *Store) isolatedHistory(ctx context.Context, request historyReadRequest) (historyReadResponse, error) {
	var response historyReadResponse
	if err := ctx.Err(); err != nil {
		return response, err
	}
	select {
	case nativeReaderSlots <- struct{}{}:
	default:
		return response, errReaderBusy
	}
	executable, err := os.Executable()
	if err != nil {
		<-nativeReaderSlots
		return response, err
	}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > 1<<20 {
		<-nativeReaderSlots
		return response, errHistoryRestricted
	}
	guard, err := s.acquireReaderGuard(ctx)
	if err != nil {
		<-nativeReaderSlots
		return response, err
	}
	if s.IsolatedReader != nil {
		result, ended, readErr := s.IsolatedReader(ctx, raw)
		if ended {
			guard.release()
			<-nativeReaderSlots
		}
		if readErr != nil {
			return response, readErr
		}
		if err = json.Unmarshal(result, &response); err != nil {
			return response, errHistoryCorrupt
		}
		return response, readResponseError(response)
	}
	cmd := exec.Command(executable, HistoryReaderArgument)
	cmd.Env = []string{}
	// Do not hand DB credentials, API keys or proxy settings to a filesystem reader.
	for _, key := range []string{"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR", "LANG"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	configureHistoryReader(cmd)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout cappedOutput
	stdout.max = 8 << 20
	cmd.Stdout = &stdout
	// Never collect or log private file names, SQL literals, or excerpts from stderr.
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 200 * time.Millisecond
	if err = cmd.Start(); err != nil {
		guard.release()
		<-nativeReaderSlots
		return response, err
	}
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(exited)
		// SQLite helpers belong to the reader's process group. Reaping only the
		// group leader is insufficient if a descendant is still stuck in NFS.
		for !historyReaderGroupDrained(cmd) {
			time.Sleep(200 * time.Millisecond)
		}
		guard.release()
		<-nativeReaderSlots
		done <- err
	}()
	work, cancel := context.WithTimeout(ctx, nativeReaderWaitBudget)
	defer cancel()
	select {
	case <-work.Done():
		select {
		case <-exited:
		default:
			killHistoryReader(cmd)
		}
		return response, work.Err()
	case err = <-done:
		if err != nil {
			return response, errors.New("isolated reader process failed")
		}
		if err = json.Unmarshal(stdout.Bytes(), &response); err != nil {
			return response, errors.New("invalid reader result")
		}
		return response, readResponseError(response)
	}
}

func (s *Store) isolatedFingerprint(ctx context.Context, workspace string, i Identity) (string, error) {
	r, e := s.isolatedHistory(ctx, historyReadRequest{Operation: "fingerprint", Root: s.HistoryRoot, Workspace: workspace, Identity: i})
	return r.Fingerprint, e
}

func (s *Store) isolatedNativeRead(ctx context.Context, workspace string, i Identity, recent bool, cutoff time.Time) ([]NativeQuestion, error) {
	generation, _ := ctx.Value(dshGenerationKey{}).(string)
	if runtimeGeneration, _ := ctx.Value(scanRuntimeKey{}).(uint64); runtimeGeneration > 0 {
		generation = fmt.Sprintf("%s|%d", generation, runtimeGeneration)
	}
	progress, _ := ctx.Value(dshProgressKey{}).(*dshProgress)
	request := historyReadRequest{Operation: "read", Root: s.HistoryRoot, Workspace: workspace, Identity: i, Recent: recent, Cutoff: cutoff, Generation: generation}
	if progress != nil {
		request.Previous = progress.previous
	}
	coldProgress, _ := ctx.Value(openClawHistoryProgressKey{}).(*openClawColdProgress)
	if coldProgress != nil {
		request.OpenClawPrevious = coldProgress.previous
	}
	r, err := s.isolatedHistory(ctx, request)
	if progress != nil && (err == nil || usablePartial(err)) {
		progress.confirmed = r.Confirmed
	}
	if coldProgress != nil && (err == nil || usablePartial(err)) {
		coldProgress.confirmed = r.OpenClawConfirmed
	}
	return r.Questions, err
}

func (s *Store) inspectNative(ctx context.Context, item scanItem, recent bool, cutoff time.Time, previousFingerprint string) (historyReadResponse, error) {
	cache, _ := ctx.Value(dshBatchKey{}).(*dshBatchCache)
	key := Key(item.workspace, scanReadGeneration(item), item.identity.Owner, fmt.Sprint(item.identity.Scope))
	if !recent && item.identity.Runtime == "deepseek-harness" && cache != nil {
		if cached, ok := cache.inspected[key]; ok {
			cached.Questions, cached.Reason, cached.Partial = cached.Facts, cached.FactReason, cached.FactPartial
			return cached, readResponseError(cached)
		}
	}
	request := historyReadRequest{Operation: "inspect", Root: s.HistoryRoot, Workspace: item.workspace, Identity: item.identity, Recent: recent, Cutoff: cutoff, Generation: scanReadGeneration(item), Fingerprint: previousFingerprint}
	if p, _ := ctx.Value(dshProgressKey{}).(*dshProgress); p != nil {
		request.Previous = p.previous
	}
	coldProgress, _ := ctx.Value(openClawHistoryProgressKey{}).(*openClawColdProgress)
	if coldProgress != nil {
		request.OpenClawPrevious = coldProgress.previous
	}
	r, err := s.isolatedHistory(ctx, request)
	if coldProgress != nil && (err == nil || usablePartial(err)) {
		coldProgress.confirmed = r.OpenClawConfirmed
	}
	if recent && item.identity.Runtime == "deepseek-harness" && !r.Unchanged && cache != nil && (err == nil || usablePartial(err)) && len(cache.inspected) < 64 && cache.inspectedFacts+len(r.Facts) <= 20000 {
		if cache.inspected == nil {
			cache.inspected = map[string]historyReadResponse{}
		}
		cache.inspected[key] = r
		cache.inspectedFacts += len(r.Facts)
	}
	return r, err
}

// HistoryReaderGroupDrained includes fixed SQLite descendants, not only the helper PID.
func HistoryReaderGroupDrained(cmd *exec.Cmd) bool { return historyReaderGroupDrained(cmd) }
