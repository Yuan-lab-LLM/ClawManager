package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func duplexReply(id string, result any) []byte {
	value, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	return value
}

func duplexCall(id any, method string, params any) []byte {
	value, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return value
}

func TestHermesDesktopDuplexRepliesAreSocketScopedAndSingleUse(t *testing.T) {
	scope := newHermesDesktopRPCScope()
	scope.live["live"] = true
	request := []byte(`{"jsonrpc":"2.0","id":"same-id","method":"clarify","params":{"session_id":"live","questions":[{"qid":"q1","question":"Choose"}]}}`)
	if err := scope.admit(duplexCall("same-id", "ping", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.observe(request); err != nil {
		t.Fatal(err)
	}
	reply := duplexReply("same-id", map[string]any{"answers": map[string]any{"q1": "answer"}, "outcome": "submitted"})
	filtered, err := hermesDesktopFilterRPC(reply)
	if err != nil {
		t.Fatal(err)
	}
	if err := newHermesDesktopRPCScope().admit(filtered); err == nil {
		t.Fatal("another socket accepted answer")
	}
	if err := scope.admit(duplexReply("same-id", map[string]any{"answers": map[string]any{"q2": "wrong question"}})); err == nil {
		t.Fatal("unknown question accepted")
	}
	if err := scope.admit(filtered); err != nil {
		t.Fatal(err)
	}
	if err := scope.admit(filtered); err == nil {
		t.Fatal("answer replayed")
	}
	if _, ok := scope.pending[`"same-id"`]; !ok {
		t.Fatal("server reply consumed client request")
	}
	if _, err := scope.observe([]byte(`{"jsonrpc":"2.0","id":"same-id","result":{}}`)); err != nil {
		t.Fatal(err)
	}
	if len(scope.pending) != 0 {
		t.Fatal("client reply did not finish")
	}
}

func TestHermesDesktopDuplexApprovalAndReplay(t *testing.T) {
	scope := newHermesDesktopRPCScope()
	if err := scope.admit(desktopRPCFrame(1, "session.resume", map[string]any{"session_id": "stored"})); err != nil {
		t.Fatal(err)
	}
	replay := []byte(`{"jsonrpc":"2.0","id":1,"result":{"session_id":"live","open_requests":[{"id":"server-a","method":"approval","params":{"session_id":"live","request_id":"queue-a","command":"rm example","choices":["once","session","always","deny"]}}]}}`)
	projected, err := scope.observe(replay)
	if err != nil || !strings.Contains(string(projected), `"choices":["once","deny"]`) {
		t.Fatalf("approval projection: %s %v", projected, err)
	}
	for _, result := range []any{map[string]any{"choice": "always"}, map[string]any{"choice": "session"}, map[string]any{"choice": "once", "all": true}} {
		if scope.admit(duplexReply("server-a", result)) == nil {
			t.Fatal("standing or batch approval accepted")
		}
	}
	if scope.admit(duplexReply("server-a", map[string]any{"choice": "once"})) != nil {
		t.Fatal("live once rejected")
	}
	if scope.admit(duplexReply("server-a", map[string]any{"choice": "deny"})) == nil {
		t.Fatal("resolved approval replayed")
	}
	bad := []byte(`{"jsonrpc":"2.0","id":"server-b","method":"approval","params":{"session_id":"live","request_id":"queue-b","command":""}}`)
	if _, err := scope.observe(bad); err != nil {
		t.Fatal(err)
	}
	if scope.admit(duplexReply("server-b", map[string]any{"choice": "once"})) == nil {
		t.Fatal("blind approval accepted")
	}
	if scope.admit(duplexReply("server-b", map[string]any{"choice": "deny"})) != nil {
		t.Fatal("deny rejected")
	}
}

func TestHermesDesktopDuplexCancellationAndClarifyLocks(t *testing.T) {
	scope := newHermesDesktopRPCScope()
	scope.live["live"] = true
	request := []byte(`{"jsonrpc":"2.0","id":"clarify-a","method":"clarify","params":{"session_id":"live","questions":[{"qid":"q1"}]}}`)
	if _, err := scope.observe(request); err != nil {
		t.Fatal(err)
	}
	for _, qid := range []string{"q1", "unknown"} {
		err := scope.admit(duplexCall(qid, "clarify.lock", map[string]any{"request_id": "clarify-a", "question_id": qid, "answer": "yes"}))
		if (err == nil) != (qid == "q1") {
			t.Fatalf("lock %s: %v", qid, err)
		}
	}
	if _, err := scope.observe([]byte(`{"jsonrpc":"2.0","method":"event","params":{"type":"request.cancel","session_id":"live","payload":{"id":"clarify-a","method":"clarify","reason":"timeout"}}}`)); err != nil {
		t.Fatal(err)
	}
	if scope.admit(duplexReply("clarify-a", map[string]any{})) == nil {
		t.Fatal("cancelled request accepted")
	}
	if scope.admit(desktopRPCFrame(2, "request.answer", map[string]any{"id": "clarify-a", "result": map[string]any{}})) == nil {
		t.Fatal("unscoped answer alias accepted")
	}
}

func TestHermesDesktopDuplexHonorsUpstreamDenyOnlyChoices(t *testing.T) {
	scope := newHermesDesktopRPCScope()
	scope.live["live"] = true
	request := []byte(`{"jsonrpc":"2.0","id":"server-deny","method":"approval","params":{"session_id":"live","request_id":"queue-deny","command":"visible command","choices":["deny"]}}`)
	if _, err := scope.observe(request); err != nil {
		t.Fatal(err)
	}
	if scope.admit(duplexReply("server-deny", map[string]any{"choice": "once"})) == nil {
		t.Fatal("upstream deny-only request escalated")
	}
	if scope.admit(duplexReply("server-deny", map[string]any{"choice": "deny"})) != nil {
		t.Fatal("deny rejected")
	}
}

func TestHermesDesktopDuplexRejectsMalformedResponses(t *testing.T) {
	for _, frame := range []string{
		`{"jsonrpc":"2.0","id":"a","result":{},"error":{"code":1,"message":"x"}}`,
		`{"jsonrpc":"2.0","id":null,"result":{}}`,
		`{"jsonrpc":"2.0","id":"a","result":[],"params":{}}`,
		`{"jsonrpc":"2.0","id":"a","result":null}`,
		`{"jsonrpc":"2.0","id":"a","error":null}`,
	} {
		if _, err := hermesDesktopFilterRPC([]byte(frame)); err == nil {
			t.Fatalf("accepted malformed response: %s", frame)
		}
	}
}
