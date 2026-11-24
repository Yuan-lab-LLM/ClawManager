package systembackuptrial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	domain "clawreef/internal/systembackuptrial/records"
)

const maxHermesHistory = 24 << 20
const hermesHistoryFormat = "clawmanager-trial-hermes-history.v1"

var errHistory = errors.New("trial Hermes history invalid or unavailable")

type historyReceipt struct {
	Sessions int    `json:"sessions"`
	Messages int    `json:"messages"`
	Digest   string `json:"transcript_sha256"`
}

type historyManifest struct {
	UserID     int    `json:"user_id"`
	InstanceID int    `json:"instance_id"`
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256"`
	historyReceipt
}

func describeHistory(payload []byte, snapshot domain.Snapshot) (historyManifest, error) {
	var input struct {
		Format     string `json:"format"`
		UserID     int    `json:"user_id"`
		InstanceID int    `json:"instance_id"`
		Digest     string `json:"transcript_sha256"`
		Sessions   []struct {
			ID       string `json:"id"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		} `json:"sessions"`
	}
	if len(payload) == 0 || len(payload) > maxHermesHistory || json.Unmarshal(payload, &input) != nil ||
		input.Format != hermesHistoryFormat || input.UserID <= 0 || input.InstanceID <= 0 ||
		len(input.Digest) != 64 || len(input.Sessions) == 0 {
		return historyManifest{}, errHistory
	}
	if _, err := hex.DecodeString(input.Digest); err != nil {
		return historyManifest{}, errHistory
	}
	if !historyBelongsToInstance(snapshot, input.UserID, input.InstanceID) {
		return historyManifest{}, errHistory
	}
	seen := make(map[string]bool, len(input.Sessions))
	var messages int
	for _, session := range input.Sessions {
		if session.ID == "" || session.Messages == nil || seen[session.ID] {
			return historyManifest{}, errHistory
		}
		for _, message := range session.Messages {
			if message.Role == "" {
				return historyManifest{}, errHistory
			}
		}
		seen[session.ID] = true
		messages += len(session.Messages)
	}
	digest := sha256.Sum256(payload)
	return historyManifest{UserID: input.UserID, InstanceID: input.InstanceID, Bytes: len(payload),
		SHA256: hex.EncodeToString(digest[:]), historyReceipt: historyReceipt{
			Sessions: len(input.Sessions), Messages: messages, Digest: input.Digest,
		}}, nil
}

func historyBelongsToInstance(snapshot domain.Snapshot, userID, instanceID int) bool {
	for _, table := range snapshot.Tables {
		if table.Name != "instances" {
			continue
		}
		for _, row := range table.Rows {
			if row["id"].String() == strconv.Itoa(instanceID) {
				return row["user_id"].String() == strconv.Itoa(userID) && row["type"].String() == "hermes" &&
					row["instance_mode"].String() == "lite" && row["runtime_type"].String() == "gateway"
			}
		}
	}
	return false
}

type historyTarget struct {
	socket string
	root   string
}

func (s *Service) historyTarget() (historyTarget, error) {
	target := historyTarget{socket: s.value("HERMES_HISTORY_SOCKET"), root: s.value("HERMES_HISTORY_TARGET_ROOT")}
	if !filepath.IsAbs(target.socket) || !filepath.IsAbs(target.root) || filepath.Clean(target.root) != target.root {
		return historyTarget{}, errHistory
	}
	socket, err := os.Lstat(target.socket)
	if err != nil || socket.Mode()&os.ModeSocket == 0 {
		return historyTarget{}, errHistory
	}
	return target, nil
}

func (t historyTarget) client() *http.Client {
	return privateSocketClient(t.socket)
}

func privateSocketClient(socket string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: 4096,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (t historyTarget) ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hermes-history/healthz", nil)
	if err != nil {
		return errHistory
	}
	req.Header.Set("X-Trial-Workspace-Root", t.root)
	response, err := t.client().Do(req)
	if err != nil {
		return errHistory
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errHistory
	}
	return nil
}

// The adapter shares a private Unix socket with the application. It owns the
// matching workspace mount and can use the Python environment from the Hermes
// image. The request never leaves the Pod over the network.
func (t historyTarget) run(ctx context.Context, mode string, history historyManifest, payload []byte) error {
	if mode != "restore" && mode != "verify" && mode != "preflight" {
		return errHistory
	}
	path := "/" + mode + "/" + strconv.Itoa(history.UserID) + "/" + strconv.Itoa(history.InstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://hermes-history"+path, bytes.NewReader(payload))
	if err != nil {
		return errHistory
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trial-Workspace-Root", t.root)
	response, err := t.client().Do(req)
	if err != nil {
		return errHistory
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errHistory
	}
	output, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	var actual historyReceipt
	if err != nil || len(output) > 4096 || json.Unmarshal(output, &actual) != nil || actual != history.historyReceipt {
		return errHistory
	}
	return nil
}
