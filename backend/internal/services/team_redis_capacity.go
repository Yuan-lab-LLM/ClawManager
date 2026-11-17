package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrTeamRedisCapacity = errors.New("Team Redis capacity unavailable; retry after pending work drains")

const teamRedisInternalNamespace = ":__cm_capacity:"

func teamRedisID(key string) (int, bool) {
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 || parts[0] != "claw" || parts[1] != "team" || len(key) > 512 {
		return 0, false
	}
	id, err := strconv.Atoi(parts[2])
	return id, err == nil && id > 0
}
func teamCapacityPrefix(teamID int) string {
	return fmt.Sprintf("claw:team:%d:__cm_capacity:", teamID)
}
func reliableTeamStream(key string) bool {
	if _, ok := teamRedisID(key); !ok {
		return false
	}
	suffix := strings.SplitN(key, ":", 4)[3]
	return suffix == "events" || suffix == "dlq" || strings.HasPrefix(suffix, "inbox:") || strings.HasPrefix(suffix, "completion-acks:")
}

// Reconciliation fences cooperating writers, accounts retained legacy state and
// publishes one complete budget snapshot. Unknown or oversized legacy state is
// retained and backpressured, never deleted to make initialization succeed.
func (b *redisBus) initializeTeamCapacity(ctx context.Context, teamID int, force bool) error {
	prefix := teamCapacityPrefix(teamID)
	meta, lock := prefix+"usage", prefix+"lock"
	if !force {
		ready, err := b.doRaw(ctx, "HGET", meta, "ready")
		if err != nil {
			return err
		}
		if ready == "1" {
			return nil
		}
	}
	// Once a deleted namespace is reclaimed, a stale cached producer must not
	// initialize a fresh budget and resurrect it. Existing atomic append/state
	// scripts also require the ready snapshot and the current writer fence.
	if b.teamLiveGuard != nil {
		if err := b.teamLiveGuard(ctx, teamID); err != nil {
			return err
		}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce)
	claimed, err := b.doRaw(ctx, "SET", lock, token, "NX", "PX", "30000")
	if err != nil || claimed != "OK" {
		return fmt.Errorf("%w: reconciliation busy", ErrTeamRedisCapacity)
	}
	defer b.doRaw(context.Background(), "EVAL", `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, "1", lock, token)
	build := prefix + "build:" + token
	defer b.doRaw(context.Background(), "DEL", build)
	args := []string{"HSET", build, "ready", "1", "bytes", "0", "keys", "0"}
	seen := make(map[string]bool)
	var total int64
	cursor := "0"
	deadline := time.Now().Add(20 * time.Second)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("%w: initialization deadline", ErrTeamRedisCapacity)
		}
		reply, err := b.doRaw(ctx, "SCAN", cursor, "MATCH", fmt.Sprintf("claw:team:%d:*", teamID), "COUNT", "200")
		if err != nil {
			return err
		}
		parts, ok := reply.([]interface{})
		if !ok || len(parts) != 2 {
			return errRedisReplyMalformed
		}
		cursor, ok = parts[0].(string)
		if !ok {
			return errRedisReplyMalformed
		}
		keys, ok := parts[1].([]interface{})
		if !ok {
			return errRedisReplyMalformed
		}
		for _, value := range keys {
			key, ok := value.(string)
			if !ok || !strings.HasPrefix(key, fmt.Sprintf("claw:team:%d:", teamID)) {
				return errRedisReplyMalformed
			}
			if (strings.HasPrefix(key, prefix) && !strings.HasPrefix(key, prefix+"receipt:") && !strings.HasPrefix(key, prefix+"groups:")) || seen[key] {
				continue
			}
			if len(key) > 512 || len(seen) >= 10000 {
				return fmt.Errorf("%w: legacy key budget", ErrTeamRedisCapacity)
			}
			seen[key] = true
			memory, err := b.doRaw(ctx, "MEMORY", "USAGE", key)
			if err != nil {
				return err
			}
			n, _ := memory.(int64) // A concurrently expired key contributes zero.
			total += n
			args = append(args, "k:"+key, "1", "b:"+key, strconv.FormatInt(n, 10))
		}
		if cursor == "0" {
			break
		}
	}
	args[5], args[7] = strconv.FormatInt(total, 10), strconv.Itoa(len(seen))
	if _, err := b.doRaw(ctx, args...); err != nil {
		return err
	}
	_, err = b.doRaw(ctx, "EVAL", `if redis.call('GET',KEYS[1])~=ARGV[1] then return redis.error_reply('CM_CAPACITY_LOCK_LOST') end redis.call('RENAME',KEYS[2],KEYS[3]); redis.call('PERSIST',KEYS[3]); redis.call('DEL',KEYS[1]); return 1`, "3", lock, build, meta, token)
	return err
}

func reliableTerminal(fields map[string]string) bool {
	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(fields["payload"]), &payload)
	kind := eventString(payload, "event", "event_type", "type")
	return kind == "completion_proposed" || kind == "completion_ack" || kind == "task_failed" || kind == "task_completed" || kind == "message_failed" || kind == "dlq"
}

func (b *redisBus) xAddReliableOnce(ctx context.Context, key string, fields map[string]string, receiptKey string) (string, error) {
	if b != nil && b.commandHook != nil {
		return "", fmt.Errorf("reliable Team delivery must wait for projection commit")
	}
	id, ok := teamRedisID(key)
	if !ok || !reliableTeamStream(key) {
		return "", fmt.Errorf("invalid reliable Team stream")
	}
	if receiptKey != "" && !strings.HasPrefix(receiptKey, teamCapacityPrefix(id)+"receipt:") {
		return "", fmt.Errorf("invalid Team delivery receipt")
	}
	if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
		return "", err
	}
	flag := "0"
	if receiptKey != "" {
		flag = "1"
	} else {
		receiptKey = teamCapacityPrefix(id) + "unused"
	}
	terminal := "0"
	if reliableTerminal(fields) {
		terminal = "1"
	}
	args := []string{"EVAL", teamCapacityAppendScript, "4", key, teamCapacityPrefix(id) + "usage", teamCapacityPrefix(id) + "lock", receiptKey, flag, terminal}
	for field, value := range fields {
		args = append(args, field, value)
	}
	result, err := b.doRaw(ctx, args...)
	if err != nil {
		if strings.Contains(err.Error(), "CM_CAPACITY") {
			return "", fmt.Errorf("%w: reliable stream admission", ErrTeamRedisCapacity)
		}
		return "", err
	}
	streamID, ok := result.(string)
	if !ok || streamID == "" {
		return "", errRedisReplyMalformed
	}
	return streamID, nil
}

const teamCapacityAppendScript = `
if ARGV[1]=='1' then local existing=redis.call('GET',KEYS[4]); if existing then return existing end end
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
if redis.call('HGET',KEYS[2],'ready')~='1' then return redis.error_reply('CM_CAPACITY_NOT_READY') end
local bytes=0
for i=3,#ARGV do bytes=bytes+string.len(ARGV[i]) end
if bytes>65536 then return redis.error_reply('CM_CAPACITY_MESSAGE_BYTES') end
local maxcount=2000; local maxbytes=33554432
if string.match(KEYS[1],':events$') then maxcount=10000; maxbytes=67108864 end
local terminal=ARGV[2]=='1'
local teamlimit=134217728
if not terminal then maxcount=math.floor(maxcount*0.75); maxbytes=math.floor(maxbytes*0.75); teamlimit=100663296 end
local count=redis.call('XLEN',KEYS[1])
local cost=bytes+1024
local receiptcost=0
if ARGV[1]=='1' then receiptcost=string.len(KEYS[4])+256 end
local used=tonumber(redis.call('HGET',KEYS[2],'bytes') or '0')
local streamused=tonumber(redis.call('HGET',KEYS[2],'b:'..KEYS[1]) or '0')
local keys=tonumber(redis.call('HGET',KEYS[2],'keys') or '0')
local newkey=redis.call('HEXISTS',KEYS[2],'k:'..KEYS[1])==0
local newkeys=0; if newkey then newkeys=1 end; if ARGV[1]=='1' then newkeys=newkeys+1 end
if count>=maxcount or streamused+cost>maxbytes or used+cost+receiptcost>teamlimit or keys+newkeys>10000 then return redis.error_reply('CM_CAPACITY_BACKPRESSURE') end
local fields={}; for i=3,#ARGV do fields[#fields+1]=ARGV[i] end
local id=redis.call('XADD',KEYS[1],'*',unpack(fields))
redis.call('HINCRBY',KEYS[2],'bytes',cost+receiptcost)
redis.call('HINCRBY',KEYS[2],'b:'..KEYS[1],cost)
if newkey then redis.call('HSET',KEYS[2],'k:'..KEYS[1],'1'); redis.call('HINCRBY',KEYS[2],'keys',1) end
if ARGV[1]=='1' then redis.call('SET',KEYS[4],id); redis.call('HSET',KEYS[2],'k:'..KEYS[4],'1','b:'..KEYS[4],receiptcost); redis.call('HINCRBY',KEYS[2],'keys',1) end
return id`

// State admission uses replacement deltas, so frequent presence updates do not
// exhaust a budget merely by overwriting the same value. Metadata is refreshed
// from MEMORY USAGE by the bounded reconciler after TTL expiry and safe trimming.
const teamCapacityStateScript = `
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
if redis.call('HGET',KEYS[2],'ready')~='1' then return redis.error_reply('CM_CAPACITY_NOT_READY') end
local cmd=ARGV[1]; local args={KEYS[1]}; local bytes=0
for i=2,#ARGV do args[#args+1]=ARGV[i]; bytes=bytes+string.len(ARGV[i]) end
if bytes>65536 then return redis.error_reply('CM_CAPACITY_MESSAGE_BYTES') end
local exists=redis.call('EXISTS',KEYS[1])==1
local old=tonumber(redis.call('HGET',KEYS[2],'b:'..KEYS[1]) or '0')
local delta=0
if cmd=='SET' then delta=string.len(ARGV[2] or '')+string.len(KEYS[1])+256-old
elseif cmd=='INCR' then delta=string.len(KEYS[1])+288-old
elseif cmd=='HSET' then
 if (#ARGV-1)%2~=0 then return redis.error_reply('ERR wrong number of arguments for HSET') end
 local n=redis.call('HLEN',KEYS[1]); local seen={}
 for i=2,#ARGV,2 do
  local prior=redis.call('HGET',KEYS[1],ARGV[i]); if not prior and not seen[ARGV[i]] then n=n+1 end; seen[ARGV[i]]=true
  delta=delta+string.len(ARGV[i+1])- (prior and string.len(prior) or 0)+(prior and 0 or string.len(ARGV[i])+128)
 end
 if n>1024 then return redis.error_reply('CM_CAPACITY_HASH_FIELDS') end
elseif cmd=='SADD' then
 local n=redis.call('SCARD',KEYS[1]); local seen={}
 for i=2,#ARGV do if not seen[ARGV[i]] and redis.call('SISMEMBER',KEYS[1],ARGV[i])==0 then n=n+1; delta=delta+string.len(ARGV[i])+128 end; seen[ARGV[i]]=true end
 if n>1024 then return redis.error_reply('CM_CAPACITY_SET_MEMBERS') end
elseif cmd~='HDEL' and cmd~='SREM' and cmd~='DEL' and cmd~='EXPIRE' then return redis.error_reply('CM_CAPACITY_UNSUPPORTED_STATE') end
if not exists then delta=delta+string.len(KEYS[1])+256 end
local used=tonumber(redis.call('HGET',KEYS[2],'bytes') or '0')
local tracked=redis.call('HEXISTS',KEYS[2],'k:'..KEYS[1])==1
local keys=tonumber(redis.call('HGET',KEYS[2],'keys') or '0')
if delta>0 and (used+delta>134217728 or (not tracked and keys>=10000)) then return redis.error_reply('CM_CAPACITY_BACKPRESSURE') end
local reply=redis.call(cmd,unpack(args))
local actual=redis.call('MEMORY','USAGE',KEYS[1]) or 0
redis.call('HINCRBY',KEYS[2],'bytes',actual-old)
if actual==0 then
 redis.call('HDEL',KEYS[2],'b:'..KEYS[1],'k:'..KEYS[1]); if tracked then redis.call('HINCRBY',KEYS[2],'keys',-1) end
else
 redis.call('HSET',KEYS[2],'b:'..KEYS[1],actual,'k:'..KEYS[1],'1'); if not tracked then redis.call('HINCRBY',KEYS[2],'keys',1) end
end
return reply`

func teamStateCommand(args []string) bool {
	if len(args) < 2 {
		return false
	}
	if _, ok := teamRedisID(args[1]); !ok || strings.Contains(args[1], teamRedisInternalNamespace) {
		return false
	}
	switch strings.ToUpper(args[0]) {
	case "SET", "INCR", "HSET", "SADD", "HDEL", "SREM", "EXPIRE":
		return true
	case "DEL":
		return len(args) == 2 && !reliableTeamStream(args[1])
	}
	return false
}

func teamStateArgs(args []string) []string {
	id, _ := teamRedisID(args[1])
	out := []string{"EVAL", teamCapacityStateScript, "3", args[1], teamCapacityPrefix(id) + "usage", teamCapacityPrefix(id) + "lock", strings.ToUpper(args[0])}
	return append(out, args[2:]...)
}

const teamCapacityGroupScript = `
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
local registered=redis.call('HEXISTS',KEYS[4],ARGV[1])==1
if not registered and redis.call('HLEN',KEYS[4])>=32 then return redis.error_reply('CM_CAPACITY_GROUPS') end
if not registered and redis.call('HGET',KEYS[4],'__trimmed')=='1' and ARGV[2]~='$' then return redis.error_reply('CM_CAPACITY_HISTORICAL_GROUP') end
local extra=0
for _,key in ipairs({KEYS[1],KEYS[4]}) do if redis.call('HEXISTS',KEYS[2],'k:'..key)==0 then extra=extra+1 end end
local used=tonumber(redis.call('HGET',KEYS[2],'bytes') or '0'); local keys=tonumber(redis.call('HGET',KEYS[2],'keys') or '0')
if not registered and (used+2048>134217728 or keys+extra>10000) then return redis.error_reply('CM_CAPACITY_BACKPRESSURE') end
local result=redis.pcall('XGROUP','CREATE',KEYS[1],ARGV[1],ARGV[2],'MKSTREAM')
if type(result)=='table' and result.err and not string.find(result.err,'BUSYGROUP',1,true) then return result end
redis.call('HSET',KEYS[4],ARGV[1],'1')
for _,key in ipairs({KEYS[1],KEYS[4]}) do
 local old=tonumber(redis.call('HGET',KEYS[2],'b:'..key) or '0'); local actual=redis.call('MEMORY','USAGE',key) or 0
 if redis.call('HEXISTS',KEYS[2],'k:'..key)==0 then redis.call('HINCRBY',KEYS[2],'keys',1) end
 redis.call('HSET',KEYS[2],'k:'..key,'1','b:'..key,actual); redis.call('HINCRBY',KEYS[2],'bytes',actual-old)
end
return result`

func teamGroupArgs(args []string) ([]string, bool) {
	if len(args) < 5 || !strings.EqualFold(args[0], "XGROUP") || !strings.EqualFold(args[1], "CREATE") || !reliableTeamStream(args[2]) {
		return nil, false
	}
	id, _ := teamRedisID(args[2])
	p := teamCapacityPrefix(id)
	if len(args[3]) > 128 || len(args[3]) == 0 {
		return nil, false
	}
	return []string{"EVAL", teamCapacityGroupScript, "4", args[2], p + "usage", p + "lock", p + "groups:" + args[2], args[3], args[4]}, true
}

// Every registered group must exist. A pending record and an undelivered record
// both fence deletion. The projection watermark is independently proven in SQL.
// XDEL is bounded to 500 IDs; no approximate MAXLEN can discard accepted work.
const teamCapacityTrimScript = `
if redis.call('EXISTS',KEYS[3])==1 then return redis.error_reply('CM_CAPACITY_RECONCILING') end
local function less(a,b)
 local am,as=string.match(a,'^(%d+)%-(%d+)$'); local bm,bs=string.match(b,'^(%d+)%-(%d+)$')
 if not am or not bm then return nil end
 if #am~=#bm then return #am<#bm end; if am~=bm then return am<bm end
 if #as~=#bs then return #as<#bs end; return as<bs
end
local function successor(id)
 local ms,seq=string.match(id,'^(%d+)%-(%d+)$'); if not ms then return nil end
 local out=''; local carry=1
 for i=#seq,1,-1 do local digit=tonumber(string.sub(seq,i,i))+carry; if digit>=10 then digit=0;carry=1 else carry=0 end; out=tostring(digit)..out end
 if carry==1 then out='1'..out end
 if #out>20 or (#out==20 and out>'18446744073709551615') then return nil end
 return ms..'-'..out
end
local minimum=ARGV[1]; if minimum~='' then minimum=successor(minimum); if not minimum then return redis.error_reply('CM_CAPACITY_WATERMARK') end end
local groups=redis.call('XINFO','GROUPS',KEYS[1]); local present={}; local n=0
for _,group in ipairs(groups) do
 local name=nil; local last=nil
 for i=1,#group,2 do if group[i]=='name' then name=group[i+1] elseif group[i]=='last-delivered-id' then last=group[i+1] end end
 if not name or not last or redis.call('HEXISTS',KEYS[4],name)==0 then return redis.error_reply('CM_CAPACITY_UNREGISTERED_GROUP') end
 present[name]=true; n=n+1
 if last=='0-0' then return 0 end
 local boundary=successor(last); if not boundary then return redis.error_reply('CM_CAPACITY_GROUP_WATERMARK') end
 if minimum=='' or less(boundary,minimum) then minimum=boundary end
 local pending=redis.call('XPENDING',KEYS[1],name)
 if pending[1]>0 and (minimum=='' or less(pending[2],minimum)) then minimum=pending[2] end
end
for _,name in ipairs(redis.call('HKEYS',KEYS[4])) do if name~='__trimmed' and not present[name] then return redis.error_reply('CM_CAPACITY_MISSING_GROUP') end end
if minimum=='' or minimum=='0-0' or (ARGV[2]=='groups' and n==0) then return 0 end
local rows=redis.call('XRANGE',KEYS[1],'-','('..minimum,'COUNT',500)
if #rows==0 then return 0 end
local ids={}; for _,row in ipairs(rows) do ids[#ids+1]=row[1] end
local deleted=redis.call('XDEL',KEYS[1],unpack(ids))
redis.call('HSET',KEYS[4],'__trimmed','1')
local old=tonumber(redis.call('HGET',KEYS[2],'b:'..KEYS[1]) or '0'); local actual=redis.call('MEMORY','USAGE',KEYS[1]) or 0
redis.call('HSET',KEYS[2],'b:'..KEYS[1],actual); redis.call('HINCRBY',KEYS[2],'bytes',actual-old)
return deleted`

func (b *redisBus) trimReliableTeamStream(ctx context.Context, key, provenWatermark string) (int64, error) {
	id, ok := teamRedisID(key)
	if !ok || !reliableTeamStream(key) {
		return 0, fmt.Errorf("invalid reliable stream")
	}
	if strings.HasSuffix(key, ":dlq") {
		return 0, fmt.Errorf("DLQ requires verified archive receipt")
	}
	mode := "groups"
	if strings.HasSuffix(key, ":events") {
		if provenWatermark == "" {
			return 0, fmt.Errorf("committed projection proof required")
		}
		mode = "projection"
	}
	if err := b.initializeTeamCapacity(ctx, id, false); err != nil {
		return 0, err
	}
	p := teamCapacityPrefix(id)
	result, err := b.doRaw(ctx, "EVAL", teamCapacityTrimScript, "4", key, p+"usage", p+"lock", p+"groups:"+key, provenWatermark, mode)
	if err != nil {
		return 0, err
	}
	n, ok := result.(int64)
	if !ok {
		return 0, errRedisReplyMalformed
	}
	return n, nil
}
