package systembackuptrial

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	domain "clawreef/internal/systembackuptrial/records"

	"github.com/gin-gonic/gin"
)

const maxHistoryInventoryPage = 100
const maxHistorySessionSelection = 64

type historySourceBinding struct {
	UserID     int `json:"user_id"`
	InstanceID int `json:"instance_id"`
}

type historySessionSelection struct {
	historySourceBinding
	SessionIDs []string `json:"session_ids"`
}

func validHistorySessionID(id string) bool {
	return len(id) > 0 && len(id) <= 256 && utf8.ValidString(id) && strings.TrimSpace(id) == id &&
		!strings.ContainsFunc(id, func(r rune) bool { return r < 32 || r == 127 })
}

func (s historySessionSelection) valid() bool {
	if s.UserID <= 0 || s.InstanceID <= 0 || len(s.SessionIDs) == 0 || len(s.SessionIDs) > maxHistorySessionSelection {
		return false
	}
	seen := make(map[string]bool, len(s.SessionIDs))
	for _, id := range s.SessionIDs {
		if !validHistorySessionID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

type historySessionOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type historyInventory struct {
	Sessions []historySessionOption `json:"sessions"`
	Next     string                 `json:"next,omitempty"`
}

type historySource struct {
	socket  string
	root    string
	binding historySourceBinding
}

func (s *Service) historySourceConfig() (historySource, error) {
	user, userErr := strconv.Atoi(s.value("HERMES_HISTORY_SOURCE_USER_ID"))
	instance, instanceErr := strconv.Atoi(s.value("HERMES_HISTORY_SOURCE_INSTANCE_ID"))
	socket, root := s.value("HERMES_HISTORY_SOURCE_SOCKET"), s.value("HERMES_HISTORY_SOURCE_ROOT")
	if userErr != nil || instanceErr != nil || user <= 0 || instance <= 0 ||
		!filepath.IsAbs(socket) || filepath.Clean(socket) != socket ||
		!filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return historySource{}, errHistory
	}
	profile, err := s.environmentProfile()
	binding := historySourceBinding{UserID: user, InstanceID: instance}
	if err != nil || profile.Side != "source" || profile.Source.HermesHistory == nil || profile.Source.HermesHistory.historySourceBinding != binding {
		return historySource{}, errHistory
	}
	return historySource{socket: socket, root: root, binding: binding}, nil
}

func (s historySource) request(ctx context.Context, method, operation, after string, body []byte, limit int64) ([]byte, error) {
	socket, err := os.Lstat(s.socket)
	if err != nil || socket.Mode()&os.ModeSocket == 0 {
		return nil, errHistory
	}
	path := "/" + operation + "/" + strconv.Itoa(s.binding.UserID) + "/" + strconv.Itoa(s.binding.InstanceID)
	endpoint := url.URL{Scheme: "http", Host: "hermes-history-source", Path: path}
	if after != "" {
		endpoint.RawQuery = url.Values{"after": {after}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errHistory
	}
	req.Header.Set("X-Trial-Workspace-Root", s.root)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := privateSocketClient(s.socket).Do(req)
	if err != nil {
		return nil, errHistory
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errHistory
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		clear(raw)
		return nil, errHistory
	}
	return raw, nil
}

func (s historySource) inventory(ctx context.Context, after string) (historyInventory, error) {
	var inventory historyInventory
	raw, err := s.request(ctx, http.MethodGet, "inventory", after, nil, 128<<10)
	if err != nil {
		return inventory, err
	}
	defer clear(raw)
	if json.Unmarshal(raw, &inventory) != nil || inventory.Sessions == nil || len(inventory.Sessions) > maxHistoryInventoryPage {
		return historyInventory{}, errHistory
	}
	last := after
	for _, option := range inventory.Sessions {
		if !validHistorySessionID(option.ID) || option.ID <= last || len(option.Title) > 640 {
			return historyInventory{}, errHistory
		}
		last = option.ID
	}
	if inventory.Next != "" && (len(inventory.Sessions) != maxHistoryInventoryPage || inventory.Next != last) {
		return historyInventory{}, errHistory
	}
	return inventory, nil
}

func (s *Service) handleBackupHistory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	after := values["after"]
	if err != nil || len(after) > 1 || (len(values) != 0 && (len(values) != 1 || len(after) != 1 || !validHistorySessionID(after[0]))) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_backup_scope"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	environment := s.environment(ctx, false)
	if environment.Profile == nil || environment.missing("source") != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "trial_environment_source"})
		return
	}
	collector, err := s.historySourceConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "hermes_history"})
		return
	}
	source, database, err := s.database("SOURCE")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "source_credential"})
		return
	}
	defer source.Close()
	choice := domain.TrialInstanceChoice{ID: int64(collector.binding.InstanceID), UserID: int64(collector.binding.UserID)}
	if err := domain.CheckTrialLiteOwner(ctx, source, database, s.value("SOURCE_UUID"), choice, "hermes"); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "hermes_history"})
		return
	}
	inventory, err := collector.inventory(ctx, values.Get("after"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "hermes_history"})
		return
	}
	c.JSON(http.StatusOK, struct {
		historySourceBinding
		historyInventory
	}{collector.binding, inventory})
}

func (s *Service) selectedHistory(ctx context.Context, selection *backupSelection, snapshot domain.Snapshot) ([]byte, error) {
	if selection != nil && selection.HistorySessions != nil {
		requested := selection.HistorySessions
		collector, err := s.historySourceConfig()
		if err != nil || collector.binding != requested.historySourceBinding ||
			!historyBelongsToInstance(snapshot, requested.UserID, requested.InstanceID) {
			return nil, domain.ErrTrialScope
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		body, err := json.Marshal(requested.SessionIDs)
		if err != nil {
			return nil, errHistory
		}
		raw, err := collector.request(ctx, http.MethodPost, "capture", "", body, maxHermesHistory)
		if err != nil || !capturedHistoryIncludes(raw, *requested) {
			clear(raw)
			return nil, errHistory
		}
		return raw, nil
	}
	file := s.value("HERMES_HISTORY_FILE")
	if (selection == nil && file == "") || (selection != nil && selection.HistorySHA256 == "") {
		return nil, nil
	}
	data, err := readSetting(file, maxHermesHistory)
	if err != nil || len(data) == 0 {
		return nil, errHistory
	}
	if selection != nil && sourceMaterialSHA256(data) != selection.HistorySHA256 {
		clear(data)
		return nil, domain.ErrTrialScope
	}
	return data, nil
}

func capturedHistoryIncludes(raw []byte, requested historySessionSelection) bool {
	var captured struct {
		historySourceBinding
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if json.Unmarshal(raw, &captured) != nil || captured.historySourceBinding != requested.historySourceBinding {
		return false
	}
	ids := make(map[string]bool, len(captured.Sessions))
	for _, session := range captured.Sessions {
		ids[session.ID] = true
	}
	for _, id := range requested.SessionIDs {
		if !ids[id] {
			return false
		}
	}
	return true
}
