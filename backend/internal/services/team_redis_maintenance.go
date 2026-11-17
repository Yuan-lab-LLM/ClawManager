package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func (s *teamService) maintainTeamRedis(parent context.Context) {
	r, ok := s.repo.(repository.TeamRedisMaintenanceRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	release, err := backupgate.HoldWorkUnit(ctx, "team.redis_maintenance")
	if err != nil {
		return
	}
	defer release()
	teams, err := r.ListTeamRedisMaintenance(s.redisMaintenanceAfter, 100)
	if err != nil {
		fmt.Printf("Team Redis maintenance: %v\n", err)
		return
	}
	force := time.Since(s.redisMaintenanceLast) >= 5*time.Minute
	for _, team := range teams {
		if ctx.Err() != nil {
			break
		}
		s.redisMaintenanceAfter = team.ID
		bus, err := s.redisBusForTeam(ctx, &team)
		if err != nil {
			continue
		}
		if team.Status == models.TeamStatusDeleted {
			err = s.cleanupDeletedTeamRedis(ctx, r, &team, bus)
		} else if team.Status != models.TeamStatusDeleting {
			if force {
				err = bus.initializeTeamCapacity(ctx, team.ID, true)
			}
			if err == nil {
				err = s.trimTeamRedis(ctx, &team, bus)
			}
		}
		if err != nil {
			fmt.Printf("Team %d Redis maintenance retained data: %v\n", team.ID, err)
		}
	}
	if len(teams) < 100 {
		s.redisMaintenanceAfter = 0
		if force {
			s.redisMaintenanceLast = time.Now()
		}
	}
}

func (s *teamService) trimTeamRedis(ctx context.Context, team *models.Team, bus *redisBus) error {
	if projection, ok := s.repo.(repository.TeamProjectionRepository); ok {
		if mark, err := projection.GetTeamProjectionWatermark(ctx, team.ID); err == nil && mark != "" {
			if _, err = bus.trimReliableTeamStream(ctx, teamEventsKey(team.ID), mark); err != nil {
				return err
			}
		}
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.Status == models.TeamMemberStatusDeleted {
			continue
		}
		// A missing stream is not a maintenance error. Registered groups fence
		// all unread/pending entries; an unknown group refuses trimming.
		key := teamInboxKey(team.ID, member.MemberKey)
		if exists, e := bus.doRaw(ctx, "EXISTS", key); e != nil {
			return e
		} else if exists != int64(0) {
			if _, e = bus.trimReliableTeamStream(ctx, key, ""); e != nil {
				return e
			}
		}
	}
	if err := s.archiveTeamDLQ(ctx, team, bus); err != nil {
		return err
	}
	return s.reclaimTeamDeliveryReceipts(ctx, team, bus)
}

func (s *teamService) cleanupDeletedTeamRedis(ctx context.Context, r repository.TeamRedisMaintenanceRepository, team *models.Team, bus *redisBus) error {
	job, err := r.TeamRedisCleanupJob(team.ID)
	if err != nil || job == nil || (job.State != "pending" && job.State != "archiving") {
		return err
	}
	// Legacy instance deletion is asynchronous. The inventory was captured
	// before members lost their instance IDs; every instance must really vanish.
	if time.Since(team.UpdatedAt) < 15*time.Second {
		return nil
	}
	var ids []int
	if err = json.Unmarshal([]byte(job.ProducerIDs), &ids); err != nil || ids == nil {
		return fmt.Errorf("producer inventory unverified")
	}
	for _, id := range ids {
		if s.instanceService == nil {
			return fmt.Errorf("producer verifier unavailable")
		}
		instance, e := s.instanceService.GetByID(id)
		if e != nil {
			return e
		}
		if instance != nil {
			return fmt.Errorf("producer instance %d deletion still in progress", id)
		}
	}
	tokenBytes := make([]byte, 16)
	if _, err = rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	claimed, err := r.ClaimTeamRedisCleanup(ctx, team.ID, token)
	if err != nil || !claimed {
		return err
	}
	lock := teamCapacityPrefix(team.ID) + "lock"
	if reply, e := bus.doRaw(ctx, "SET", lock, token, "NX", "PX", "45000"); e != nil || reply != "OK" {
		return fmt.Errorf("cleanup writer fence unavailable")
	}
	defer bus.doRaw(context.Background(), "EVAL", `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, "1", lock, token)
	if err = r.CloseDeletedTeamWork(ctx, team.ID); err != nil {
		return err
	}
	key, err := r.PendingTeamRedisCleanupKey(ctx, team.ID)
	if err != nil {
		return err
	}
	if key == nil {
		reply, e := bus.doRaw(ctx, "SCAN", job.ScanCursor, "MATCH", fmt.Sprintf("claw:team:%d:*", team.ID), "COUNT", "200")
		if e != nil {
			return e
		}
		parts, ok := reply.([]interface{})
		if !ok || len(parts) != 2 {
			return errRedisReplyMalformed
		}
		next, ok := parts[0].(string)
		if !ok {
			return errRedisReplyMalformed
		}
		values, ok := parts[1].([]interface{})
		if !ok {
			return errRedisReplyMalformed
		}
		keys := map[string]string{}
		for _, v := range values {
			k, ok := v.(string)
			if !ok || !strings.HasPrefix(k, fmt.Sprintf("claw:team:%d:", team.ID)) {
				return errRedisReplyMalformed
			}
			if k == lock {
				continue
			}
			kind, e := bus.doRaw(ctx, "TYPE", k)
			if e != nil {
				return e
			}
			text, ok := kind.(string)
			if !ok {
				return errRedisReplyMalformed
			}
			keys[k] = text
		}
		if err = r.DiscoverTeamRedisCleanupKeys(ctx, team.ID, next, keys); err != nil {
			return err
		}
		key, err = r.PendingTeamRedisCleanupKey(ctx, team.ID)
		if err != nil {
			return err
		}
		if key == nil {
			if next == "0" {
				return r.FinishTeamRedisCleanup(ctx, team.ID)
			}
			return nil
		}
	}
	if !key.Complete {
		raw, next, complete, e := readRedisArchiveChunk(ctx, bus, key)
		if e != nil {
			return e
		}
		owner := "user:" + strconv.Itoa(team.UserID)
		ref, e := repository.DurableArchivePut(ctx, owner, raw)
		if e != nil {
			return e
		}
		verified, e := repository.DurableArchiveRead(ctx, owner, ref)
		if e != nil || !bytes.Equal(verified, raw) {
			return fmt.Errorf("Redis archive readback did not verify: %w", e)
		}
		if e = r.SaveTeamRedisArchive(ctx, key, next, complete, ref, raw); e != nil {
			return e
		}
		key.Complete = complete
	}
	if key.Complete {
		// Only the current lease owner can remove a completely archived key.
		_, err = bus.doRaw(ctx, "EVAL", `if redis.call('GET',KEYS[1])~=ARGV[1] then return redis.error_reply('CM_CLEANUP_FENCE_LOST') end return redis.call('UNLINK',KEYS[2])`, "2", lock, key.Key, token)
		if err != nil {
			return err
		}
		return r.MarkTeamRedisKeyDeleted(ctx, key)
	}
	return nil
}

// Chunks retain RESP strings as bytes (base64 in JSON), including non-UTF8 data.
// COUNT is adaptive because Redis COUNT is a hint and old hash values may be big.
func redisArchiveValue(value any) any {
	switch v := value.(type) {
	case string:
		return []byte(v)
	case []interface{}:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = redisArchiveValue(x)
		}
		return out
	default:
		return value
	}
}
func readRedisArchiveChunk(ctx context.Context, bus *redisBus, k *repository.TeamRedisCleanupKey) ([]byte, string, bool, error) {
	next := "0"
	complete := true
	var value any
	var err error
	switch k.Kind {
	case "none":
		value = nil
	case "string":
		value, err = bus.doRaw(ctx, "GET", k.Key)
	case "hash", "set":
		cmd := "HSCAN"
		if k.Kind == "set" {
			cmd = "SSCAN"
		}
		for count := 50; count >= 1; count /= 2 {
			value, err = bus.doRaw(ctx, cmd, k.Key, k.Cursor, "COUNT", strconv.Itoa(count))
			if err == nil || !errors.Is(err, errRedisReplyBudget) {
				break
			}
		}
		if err == nil {
			parts, ok := value.([]interface{})
			if !ok || len(parts) != 2 {
				return nil, "", false, errRedisReplyMalformed
			}
			next, ok = parts[0].(string)
			if !ok {
				return nil, "", false, errRedisReplyMalformed
			}
			value = parts[1]
			complete = next == "0"
		}
	case "stream":
		start := "-"
		if k.Cursor != "0" {
			start = "(" + k.Cursor
		}
		value, err = bus.doRaw(ctx, "XRANGE", k.Key, start, "+", "COUNT", "50")
		if err == nil {
			rows, ok := value.([]interface{})
			if !ok {
				return nil, "", false, errRedisReplyMalformed
			}
			if len(rows) > 0 {
				last, ok := rows[len(rows)-1].([]interface{})
				if !ok || len(last) < 1 {
					return nil, "", false, errRedisReplyMalformed
				}
				next, ok = last[0].(string)
				if !ok {
					return nil, "", false, errRedisReplyMalformed
				}
				complete = false
			}
		}
	default:
		return nil, "", false, fmt.Errorf("unsupported retained Redis type %q", k.Kind)
	}
	if err != nil {
		return nil, "", false, err
	}
	raw, err := json.Marshal(map[string]any{"key": []byte(k.Key), "type": k.Kind, "chunk": k.Chunk, "cursor": k.Cursor, "next": next, "complete": complete, "data": redisArchiveValue(value)})
	if len(raw) > 8<<20 {
		return nil, "", false, fmt.Errorf("Redis archive chunk exceeds 8 MiB")
	}
	return raw, next, complete, err
}

func (s *teamService) archiveTeamDLQ(ctx context.Context, team *models.Team, bus *redisBus) error {
	// Detailed archive/receipt implementation is shared with deleted-Team
	// maintenance; live DLQ removal requires a durable SQL receipt below.
	return s.archiveLiveTeamDLQ(ctx, team, bus)
}

func (s *teamService) archiveLiveTeamDLQ(ctx context.Context, team *models.Team, bus *redisBus) error {
	r, ok := s.repo.(repository.TeamRedisMaintenanceRepository)
	if !ok {
		return nil
	}
	key := teamDLQKey(team.ID)
	if exists, err := bus.doRaw(ctx, "EXISTS", key); err != nil {
		return err
	} else if exists == int64(0) {
		return nil
	}
	groups, err := bus.doRaw(ctx, "XINFO", "GROUPS", key)
	if err != nil {
		return err
	}
	if rows, ok := groups.([]interface{}); !ok || len(rows) > 0 {
		return fmt.Errorf("DLQ has active consumer/replay groups; retain until explicitly closed")
	}
	maximum := fmt.Sprintf("%d-0", time.Now().Add(-8*24*time.Hour).UnixMilli())
	reply, err := bus.doRaw(ctx, "XRANGE", key, "-", maximum, "COUNT", "100")
	if err != nil {
		return err
	}
	rows, ok := reply.([]interface{})
	if !ok {
		return errRedisReplyMalformed
	}
	for _, row := range rows {
		entry, ok := row.([]interface{})
		if !ok || len(entry) != 2 {
			return errRedisReplyMalformed
		}
		id, ok := entry[0].(string)
		if !ok {
			return errRedisReplyMalformed
		}
		raw, err := json.Marshal(map[string]any{"key": []byte(key), "type": "stream", "data": redisArchiveValue(entry)})
		if err != nil {
			return err
		}
		owner := "user:" + strconv.Itoa(team.UserID)
		ref, err := repository.DurableArchivePut(ctx, owner, raw)
		if err != nil {
			return err
		}
		verified, err := repository.DurableArchiveRead(ctx, owner, ref)
		if err != nil || !bytes.Equal(verified, raw) {
			return fmt.Errorf("DLQ archive verification failed")
		}
		if err = r.RecordTeamDLQArchive(ctx, team.ID, id, ref, raw); err != nil {
			return err
		}
		// Recheck group creation atomically with deletion; no newly registered
		// replay group can lose an entry between the proof and removal.
		_, err = bus.doRaw(ctx, "EVAL", `if #redis.call('XINFO','GROUPS',KEYS[1])>0 then return redis.error_reply('CM_DLQ_GROUP_RACE') end return redis.call('XDEL',KEYS[1],ARGV[1])`, "1", key, id)
		if err != nil {
			return err
		}
	}
	return nil
}
