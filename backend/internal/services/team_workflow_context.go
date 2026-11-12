package services

import (
	"clawreef/internal/models"
	"context"
	"strings"
)

// Only acknowledged public delivery fields enter the advisory Runtime context.
// Dispatch files, raw event payloads and model reasoning are not copied.
func acknowledgedWorkItemContext(item models.TeamWorkItem) map[string]interface{} {
	if !workItemHasAcceptedResultReceipt(item) {
		return nil
	}
	payload := workItemResultPayload(item)
	text := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer")
	runes := []rune(text)
	truncated := len(runes) > 4000
	if truncated {
		text = string(runes[:4000])
	}
	out := map[string]interface{}{
		"summary": workItemResultSummary(item), "resultMarkdown": text,
		"resultTruncated": truncated, "artifactRefs": workItemArtifactRefs(item),
	}
	if supplement, ok := payload["deliverySupplement"].(map[string]interface{}); ok {
		text := eventString(supplement, "text")
		runes := []rune(text)
		if len(runes) > 4000 {
			text = string(runes[:4000])
		}
		out["supplementaryText"] = text
		out["supplementTruncated"] = len(runes) > 4000 || eventBool(supplement, "textTruncated")
		out["supplementSourceRecordId"] = eventString(supplement, "sourceRecordId")
	}
	return out
}

// This protects the existing advisory projection, not the SQL state machine.
// Concurrent old callbacks must not roll back a newer ledger or terminal fact.
const publishTeamWorkflowContextScript = `
local previous = redis.call('GET', KEYS[1])
local next = cjson.decode(ARGV[1])
if previous then
 local ok, old = pcall(cjson.decode, previous)
 if ok and old.rootTaskId == next.rootTaskId then
  if tonumber(old.ledgerVersion or 0) > tonumber(next.ledgerVersion or 0) then return 0 end
  if tonumber(old.ledgerVersion or 0) == tonumber(next.ledgerVersion or 0)
   and old.terminal == true and next.terminal ~= true then return 0 end
  if tonumber(old.ledgerVersion or 0) == tonumber(next.ledgerVersion or 0)
   and old.assignmentLedgerComplete == true and next.assignmentLedgerComplete ~= true then return 0 end
 end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', 604800)
return 1`

func publishTeamWorkflowContext(bus *redisBus, key string, encoded []byte) {
	if bus == nil || strings.TrimSpace(key) == "" {
		return
	}
	// A failed projection never changes dispatch/completion authority. SQL and
	// the existing durable delivery path remain responsible for business facts.
	_, _ = bus.do(context.Background(), "EVAL", publishTeamWorkflowContextScript, "1", key, string(encoded))
}
