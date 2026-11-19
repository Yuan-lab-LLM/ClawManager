package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Replay closure is a statement about actual sources, not a TTL on dedup keys.
// The bridge admits only XADD '*', so future producer writes cannot recreate an
// entry behind this cutoff. Pending ghosts and unknown consumer groups fail closed.
func verifyTeamReplaySourceWindow(ctx context.Context, bus *redisBus, teamID int, cutoff time.Time) (string, error) {
	maximum := strconv.FormatInt(cutoff.UnixMilli(), 10) + "-0"
	h := sha256.New()
	fmt.Fprintf(h, "team=%d;cutoff=%s;", teamID, cutoff.UTC().Format(time.RFC3339Nano))
	cursor := "0"
	visited := 0
	for {
		reply, err := bus.doRaw(ctx, "SCAN", cursor, "MATCH", fmt.Sprintf("claw:team:%d:*", teamID), "COUNT", "200")
		if err != nil {
			return "", err
		}
		parts, ok := reply.([]interface{})
		if !ok || len(parts) != 2 {
			return "", errRedisReplyMalformed
		}
		cursor, ok = parts[0].(string)
		if !ok {
			return "", errRedisReplyMalformed
		}
		keys, ok := parts[1].([]interface{})
		if !ok {
			return "", errRedisReplyMalformed
		}
		for _, raw := range keys {
			key, ok := raw.(string)
			if !ok {
				return "", errRedisReplyMalformed
			}
			visited++
			if visited > 12000 {
				return "", fmt.Errorf("replay proof exceeds namespace budget")
			}
			if !reliableTeamStream(key) || strings.HasSuffix(key, ":dlq") {
				continue
			}
			rows, err := bus.doRaw(ctx, "XRANGE", key, "-", maximum, "COUNT", "1")
			if err != nil {
				return "", err
			}
			values, ok := rows.([]interface{})
			if !ok {
				return "", errRedisReplyMalformed
			}
			if len(values) > 0 {
				return "", fmt.Errorf("replay source %s remains inside retained window", key)
			}
			groups, err := bus.doRaw(ctx, "XINFO", "GROUPS", key)
			if err != nil {
				return "", err
			}
			groupRows, ok := groups.([]interface{})
			if !ok {
				return "", errRedisReplyMalformed
			}
			for _, rawGroup := range groupRows {
				group, ok := rawGroup.([]interface{})
				if !ok || len(group)%2 != 0 {
					return "", errRedisReplyMalformed
				}
				name := ""
				for i := 0; i < len(group); i += 2 {
					if group[i] == "name" {
						name, _ = group[i+1].(string)
					}
				}
				if name == "" {
					return "", errRedisReplyMalformed
				}
				registered, err := bus.doRaw(ctx, "HEXISTS", teamCapacityPrefix(teamID)+"groups:"+key, name)
				if err != nil {
					return "", err
				}
				if registered != int64(1) {
					return "", fmt.Errorf("unregistered group fences replay closure")
				}
				pending, err := bus.doRaw(ctx, "XPENDING", key, name)
				if err != nil {
					return "", err
				}
				p, ok := pending.([]interface{})
				if !ok || len(p) < 2 {
					return "", errRedisReplyMalformed
				}
				if n, ok := p[0].(int64); !ok {
					return "", errRedisReplyMalformed
				} else if n > 0 {
					oldest, ok := p[1].(string)
					if !ok {
						return "", errRedisReplyMalformed
					}
					first, _, ok := strings.Cut(oldest, "-")
					stamp, err := strconv.ParseInt(first, 10, 64)
					if !ok || err != nil || stamp <= cutoff.UnixMilli() {
						return "", fmt.Errorf("pending source still requires dedup identity")
					}
				}
			}
			fmt.Fprint(h, key, ";")
		}
		if cursor == "0" {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *teamService) reclaimTeamDeliveryReceipts(ctx context.Context, team *models.Team, bus *redisBus) error {
	r, ok := s.repo.(repository.TeamOutboxRetentionRepository)
	if !ok {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -8)
	candidates, err := r.ListEligibleTeamOutboxReceipts(ctx, team.ID, cutoff, 100)
	if err != nil {
		return err
	}
	projection, ok := s.repo.(repository.TeamProjectionRepository)
	if !ok {
		return fmt.Errorf("projection proof unavailable")
	}
	watermark, err := projection.GetTeamProjectionWatermark(ctx, team.ID)
	if err != nil {
		return err
	}
	proof, err := verifyTeamReplaySourceWindow(ctx, bus, team.ID, cutoff)
	if err != nil {
		return err
	}
	proofHash := sha256.Sum256([]byte(proof + ":" + watermark))
	proof = hex.EncodeToString(proofHash[:])
	publication, err := json.Marshal(map[string]any{"schema": 1, "closed_before_ms": cutoff.UnixMilli(), "committed_cursor": watermark, "proof_sha256": proof, "verified_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	if _, err = bus.doRaw(ctx, "SET", teamCapacityPrefix(team.ID)+"replay-source-closed", string(publication)); err != nil {
		return err
	}
	for _, row := range candidates {
		if !row.Closed {
			if err = r.CloseTeamOutboxReceiptWindow(ctx, row.OutboxID, row.TaskID, proof); err != nil {
				return err
			}
		}
		identity := repository.TeamOutboxReceiptIdentity(team.ID, row.Destination, row.MessageID)
		if row.Destination == teamProjectionCommandDestination {
			identity = boundedTeamEventID("projection", row.MessageID)
		}
		key := teamCapacityPrefix(team.ID) + "receipt:" + identity
		// A durable SQL tombstone now prevents every normal sender from creating
		// this obligation again. Retrying after a Redis/SQL crash is harmless.
		_, err = bus.doRaw(ctx, "EVAL", `
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
local prior=tonumber(redis.call('HGET',KEYS[2],'b:'..KEYS[1]) or '0')
local known=redis.call('HEXISTS',KEYS[2],'k:'..KEYS[1])==1
local n=redis.call('DEL',KEYS[1]); redis.call('HDEL',KEYS[2],'b:'..KEYS[1],'k:'..KEYS[1])
if known then redis.call('HINCRBY',KEYS[2],'keys',-1); redis.call('HINCRBY',KEYS[2],'bytes',-prior) end
return n`, "3", key, teamCapacityPrefix(team.ID)+"usage", teamCapacityPrefix(team.ID)+"lock")
		if err != nil {
			return err
		}
		if err = r.MarkTeamOutboxReceiptReclaimed(ctx, row.OutboxID); err != nil {
			return err
		}
	}
	return s.reclaimTeamCompletionReceipts(ctx, team, bus, cutoff, proof)
}

var completionLegacyKeyChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func (s *teamService) reclaimTeamCompletionReceipts(ctx context.Context, team *models.Team, bus *redisBus, cutoff time.Time, proof string) error {
	r, ok := s.repo.(repository.TeamCompletionRetentionRepository)
	if !ok {
		return nil
	}
	rows, err := r.ListTeamCompletionReceipts(ctx, team.ID, cutoff, 100)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !row.Closed {
			if err = r.CloseTeamCompletionReceipt(ctx, team.ID, row.EventID, row.TaskID, proof); err != nil {
				return err
			}
		}
		keys := []string{}
		if row.BusinessEventID != "" {
			digest := sha256.Sum256([]byte(row.BusinessEventID))
			keys = append(keys, fmt.Sprintf("claw:team:%d:completions:delivery-%x", team.ID, digest))
		}
		if row.CompletionID != "" {
			legacy := completionLegacyKeyChars.ReplaceAllString(row.CompletionID, "_")
			if len(legacy) > 160 {
				legacy = legacy[:160]
			}
			keys = append(keys, fmt.Sprintf("claw:team:%d:completions:%s", team.ID, legacy))
		}
		for _, key := range keys {
			// Legacy sanitized names can collide. Only the exact persisted source
			// stream ID authorizes deleting its marker; another value is retained.
			_, err = bus.doRaw(ctx, "EVAL", `if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
local prior=tonumber(redis.call('HGET',KEYS[2],'b:'..KEYS[1]) or '0'); local known=redis.call('HEXISTS',KEYS[2],'k:'..KEYS[1])==1
local n=redis.call('DEL',KEYS[1]); redis.call('HDEL',KEYS[2],'b:'..KEYS[1],'k:'..KEYS[1]); if known then redis.call('HINCRBY',KEYS[2],'keys',-1); redis.call('HINCRBY',KEYS[2],'bytes',-prior) end; return n`, "3", key, teamCapacityPrefix(team.ID)+"usage", teamCapacityPrefix(team.ID)+"lock", row.StreamID)
			if err != nil {
				return err
			}
		}
		if err = r.MarkTeamCompletionReceiptReclaimed(ctx, team.ID, row.EventID); err != nil {
			return err
		}
	}
	return nil
}
