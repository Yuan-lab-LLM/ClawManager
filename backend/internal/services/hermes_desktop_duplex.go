package services

import (
	"encoding/json"
	"regexp"
)

var hermesDesktopRPCMethod = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,159}$`)

// Server requests have their own ID namespace. A browser response must never
// retire a pending client call, answer another socket, or create an approval.
type hermesDesktopServerRequest struct {
	method, sessionID, approvalID string
	questions                     map[string]bool
}

func hermesDesktopFilterResponse(packet map[string]json.RawMessage) ([]byte, error) {
	var id string
	if string(packet["jsonrpc"]) != `"2.0"` || json.Unmarshal(packet["id"], &id) != nil || !hermesDesktopSessionID.MatchString(id) {
		return nil, ErrHermesDesktopForbidden
	}
	for key := range packet {
		if key != "jsonrpc" && key != "id" && key != "result" && key != "error" {
			return nil, ErrHermesDesktopForbidden
		}
	}
	if (packet["result"] == nil) == (packet["error"] == nil) {
		return nil, ErrHermesDesktopForbidden
	}
	if raw := packet["error"]; raw != nil {
		var value struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &value) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["code"] == nil || fields["message"] == nil || len(value.Message) > 1024 {
			return nil, ErrHermesDesktopForbidden
		}
	} else {
		var result map[string]json.RawMessage
		if len(packet["result"]) > 128<<10 || json.Unmarshal(packet["result"], &result) != nil || result == nil {
			return nil, ErrHermesDesktopForbidden
		}
	}
	return json.Marshal(packet)
}

// Callers hold scope.mu, including for open_requests restored by a resume.
func (scope *hermesDesktopRPCScope) observeServerRequest(packet map[string]json.RawMessage, expectedSession string) ([]byte, error) {
	var id, method, sid string
	var params map[string]json.RawMessage
	if json.Unmarshal(packet["id"], &id) != nil || !hermesDesktopSessionID.MatchString(id) ||
		json.Unmarshal(packet["method"], &method) != nil || !hermesDesktopRPCMethod.MatchString(method) ||
		json.Unmarshal(packet["params"], &params) != nil || params == nil ||
		json.Unmarshal(params["session_id"], &sid) != nil || !hermesDesktopSessionID.MatchString(sid) ||
		(expectedSession != "" && sid != expectedSession) {
		return nil, ErrHermesDesktopUpstream
	}
	keyBytes, _ := json.Marshal(id)
	key := string(keyBytes)
	previous, exists := scope.serverRequests[key]
	if (!exists && len(scope.serverRequests) >= 256) || (exists && (previous.method != method || previous.sessionID != sid)) {
		return nil, ErrHermesDesktopUpstream
	}
	request := hermesDesktopServerRequest{method: method, sessionID: sid}
	if method == "approval" {
		safe, err := scope.projectApproval(sid, packet["params"], false)
		if err != nil {
			return nil, err
		}
		packet["params"] = safe
		_ = json.Unmarshal(params["request_id"], &request.approvalID)
		if exists && previous.approvalID != request.approvalID {
			return nil, ErrHermesDesktopUpstream
		}
	}
	if method == "clarify" {
		var questions []struct {
			QID string `json:"qid"`
		}
		if json.Unmarshal(params["questions"], &questions) != nil || len(questions) == 0 || len(questions) > 5 {
			return nil, ErrHermesDesktopUpstream
		}
		request.questions = map[string]bool{}
		for _, question := range questions {
			if question.QID == "" || len(question.QID) > 160 || request.questions[question.QID] {
				return nil, ErrHermesDesktopUpstream
			}
			request.questions[question.QID] = true
		}
	}
	scope.serverRequests[key] = request
	return json.Marshal(packet)
}

func (scope *hermesDesktopRPCScope) admitServerResponse(frame []byte, key string) error {
	var packet map[string]json.RawMessage
	if json.Unmarshal(frame, &packet) != nil {
		return ErrHermesDesktopForbidden
	}
	if _, err := hermesDesktopFilterResponse(packet); err != nil {
		return err
	}
	request, found := scope.serverRequests[key]
	if !found {
		return ErrHermesDesktopForbidden
	}
	// A renderer may decline an unsupported prompt, but cannot acknowledge an
	// unimplemented native or credential operation with fabricated success.
	if packet["error"] != nil {
		delete(scope.serverRequests, key)
		return nil
	}
	if !scope.live[request.sessionID] {
		return errHermesDesktopUnboundSession
	}
	var result map[string]json.RawMessage
	_ = json.Unmarshal(packet["result"], &result)
	switch request.method {
	case "approval":
		var choice string
		if json.Unmarshal(result["choice"], &choice) != nil || (choice != "once" && choice != "deny") {
			return ErrHermesDesktopForbidden
		}
		for field, value := range result {
			if field != "choice" && (field != "all" || string(value) != "false") {
				return ErrHermesDesktopForbidden
			}
		}
		denyOnly, known := scope.approvals[request.sessionID][request.approvalID]
		if !known || denyOnly && choice != "deny" {
			return ErrHermesDesktopForbidden
		}
	case "clarify":
		for field, value := range result {
			switch field {
			case "answers":
				if string(value) == "null" {
					continue
				}
				var answers map[string]*string
				if json.Unmarshal(value, &answers) != nil || len(answers) > len(request.questions) {
					return ErrHermesDesktopForbidden
				}
				for qid, answer := range answers {
					if !request.questions[qid] || answer != nil && len(*answer) > 16384 {
						return ErrHermesDesktopForbidden
					}
				}
			case "outcome":
				if string(value) != `"submitted"` && string(value) != `"timed_out"` && string(value) != `"cancelled"` && string(value) != "null" {
					return ErrHermesDesktopForbidden
				}
			default:
				return ErrHermesDesktopForbidden
			}
		}
	default:
		return ErrHermesDesktopForbidden
	}
	delete(scope.serverRequests, key)
	return nil
}

func (scope *hermesDesktopRPCScope) cancelServerRequest(packet map[string]json.RawMessage) {
	var event struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		Payload   struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if json.Unmarshal(packet["params"], &event) != nil || event.Type != "request.cancel" {
		return
	}
	id, _ := json.Marshal(event.Payload.ID)
	if request, ok := scope.serverRequests[string(id)]; ok && request.sessionID == event.SessionID {
		delete(scope.serverRequests, string(id))
	}
}
