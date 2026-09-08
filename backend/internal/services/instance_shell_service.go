package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// InstanceShellService streams an interactive shell into an instance pod.
type InstanceShellService struct {
	podService *k8s.PodService
	upgrader   websocket.Upgrader
}

type shellClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type shellTerminalSizeQueue struct {
	sizes  chan remotecommand.TerminalSize
	mu     sync.Mutex
	closed bool
}

type shellWebSocketWriter struct {
	conn *websocket.Conn
	mu   *sync.Mutex
	done <-chan struct{}
}

func NewInstanceShellService() *InstanceShellService {
	return &InstanceShellService{
		podService: k8s.NewPodService(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *InstanceShellService) Stream(ctx context.Context, instance *models.Instance, w http.ResponseWriter, r *http.Request) error {
	if s.podService == nil || s.podService.GetClient() == nil || s.podService.GetClient().Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("failed to upgrade shell websocket: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	writeMu := &sync.Mutex{}
	done := make(chan struct{})
	defer close(done)
	defer conn.Close()

	stdinReader, stdinWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdinWriter.Close()

	sizeQueue := newShellTerminalSizeQueue()
	defer sizeQueue.close()
	sizeQueue.push(120, 32)

	go readShellWebSocket(streamCtx, cancel, conn, stdinWriter, sizeQueue)

	pod, container, command, err := s.execTarget(streamCtx, instance)
	if err != nil {
		_ = writeShellBytes(conn, writeMu, done, []byte(fmt.Sprintf("\r\nfailed to start terminal: %v\r\n", err)))
		return fmt.Errorf("failed to get pod: %w", err)
	}

	req := s.podService.GetClient().Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    false,
		TTY:       true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(s.podService.GetClient().Config, "POST", req.URL())
	if err != nil {
		_ = writeShellBytes(conn, writeMu, done, []byte(fmt.Sprintf("\r\nfailed to initialize shell: %v\r\n", err)))
		return fmt.Errorf("failed to initialize shell stream: %w", err)
	}

	writer := &shellWebSocketWriter{conn: conn, mu: writeMu, done: done}
	if err := exec.StreamWithContext(streamCtx, remotecommand.StreamOptions{
		Stdin:             stdinReader,
		Stdout:            writer,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	}); err != nil && streamCtx.Err() == nil {
		_ = writeShellBytes(conn, writeMu, done, []byte(fmt.Sprintf("\r\nshell closed: %v\r\n", err)))
		return fmt.Errorf("shell stream failed: %w", err)
	}

	return nil
}

func (s *InstanceShellService) execTarget(ctx context.Context, instance *models.Instance) (*corev1.Pod, string, []string, error) {
	if instance == nil {
		return nil, "", nil, fmt.Errorf("instance is required")
	}
	if !strings.EqualFold(strings.TrimSpace(instance.RuntimeType), RuntimeBackendShell) {
		return nil, "", nil, fmt.Errorf("shell access is only available for shell instances")
	}

	pod, err := s.podService.GetPod(ctx, instance.UserID, instance.ID)
	if err != nil {
		return nil, "", nil, err
	}
	return pod, "desktop", defaultShellCommand(), nil
}

func defaultShellCommand() []string {
	return []string{
		"sh",
		"-lc",
		`export TERM="${TERM:-xterm-256color}" COLORTERM="${COLORTERM:-truecolor}"; if command -v tmux >/dev/null 2>&1; then session="${CLAWMANAGER_TMUX_SESSION:-openclaw-shell}"; exec tmux new-session -A -s "$session"; fi; if command -v bash >/dev/null 2>&1; then exec bash -l; fi; exec sh`,
	}
}

func newShellTerminalSizeQueue() *shellTerminalSizeQueue {
	return &shellTerminalSizeQueue{
		sizes: make(chan remotecommand.TerminalSize, 4),
	}
}

func (q *shellTerminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.sizes
	if !ok {
		return nil
	}
	return &size
}

func (q *shellTerminalSizeQueue) push(cols, rows uint16) {
	if q == nil || cols == 0 || rows == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	size := remotecommand.TerminalSize{Width: cols, Height: rows}
	select {
	case q.sizes <- size:
	default:
		select {
		case <-q.sizes:
		default:
		}
		select {
		case q.sizes <- size:
		default:
		}
	}
}

func (q *shellTerminalSizeQueue) close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.sizes)
	}
}

func readShellWebSocket(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stdin *io.PipeWriter, sizeQueue *shellTerminalSizeQueue) {
	defer cancel()
	defer stdin.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			_ = stdin.CloseWithError(err)
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) > 0 {
				_, _ = stdin.Write(payload)
			}
		case websocket.TextMessage:
			var message shellClientMessage
			if json.Unmarshal(payload, &message) == nil && message.Type != "" {
				switch message.Type {
				case "input":
					if message.Data != "" {
						_, _ = io.WriteString(stdin, message.Data)
					}
				case "resize":
					sizeQueue.push(message.Cols, message.Rows)
				}
				continue
			}

			if len(payload) > 0 {
				_, _ = stdin.Write(payload)
			}
		}
	}
}

func (w *shellWebSocketWriter) Write(payload []byte) (int, error) {
	if err := writeShellBytes(w.conn, w.mu, w.done, payload); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func writeShellBytes(conn *websocket.Conn, mu *sync.Mutex, done <-chan struct{}, payload []byte) error {
	select {
	case <-done:
		return io.ErrClosedPipe
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}
