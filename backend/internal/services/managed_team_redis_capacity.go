package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errManagedRedisKeyDenied = errors.New("managed Team Redis key access denied")

// Only CM emits this command after the client-script allowlist has been checked.
// Redis serializes the fixed error with all earlier/later pipeline replies.
const managedRedisKeyDeniedScript = "return redis.error_reply('NOPERM managed Team key access denied')"

// Runtime supplies only the two exact allowlisted scripts. A read-only first
// pass checks every write before the original atomic script can change state.
// This matters for deferred dispatch: rejecting its second append must not leave
// its first append committed (Redis script errors do not roll back writes).
func capacityGuardedScript(script string, keyCount int, terminal bool) string {
	limit := "100663296"
	fraction := "0.75"
	if terminal {
		limit = "134217728"
		fraction = "1"
	}
	return fmt.Sprintf(`
local native=redis
local meta=KEYS[%d]; local lock=KEYS[%d]
local changed={}; local growth=0; local newkeys=0
local function tracked(key)
 if not changed[key] then
  changed[key]=true
  if native.call('HEXISTS',meta,'k:'..key)==0 then newkeys=newkeys+1 end
 end
end
local redis={call=function(command,...)
 local cmd=string.upper(command); local a={...}; local key=a[1]
 if cmd=='GET' or cmd=='HGET' then return native.call(command,unpack(a)) end
 if cmd=='XADD' then
  local bytes=0; for i=3,#a do bytes=bytes+string.len(a[i]) end
  if a[2]~='*' or bytes>65536 then error('CM_CAPACITY_MESSAGE_BYTES') end
  local count=native.call('XLEN',key); local maximum=2000; local maxbytes=33554432
  if string.match(key,':events$') then maximum=10000; maxbytes=67108864 end
  local used=tonumber(native.call('HGET',meta,'b:'..key) or '0')
  if count>=math.floor(maximum*%s) or used+bytes+1024>maxbytes*%s then error('CM_CAPACITY_BACKPRESSURE') end
  growth=growth+bytes+1024; tracked(key); return '0-0'
 elseif cmd=='SET' then
  if string.len(a[2])>65536 then error('CM_CAPACITY_MESSAGE_BYTES') end
  growth=growth+string.len(key)+string.len(a[2])+256; tracked(key); return 'OK'
 elseif cmd=='HSET' then
  local prior=native.call('HGET',key,a[2]); local count=native.call('HLEN',key)
  if not prior and count>=1024 then error('CM_CAPACITY_HASH_FIELDS') end
  if string.len(a[2])+string.len(a[3])>65536 then error('CM_CAPACITY_MESSAGE_BYTES') end
  growth=growth+math.max(0,string.len(a[3])-(prior and string.len(prior) or 0))+string.len(a[2])+256; tracked(key); return 1
 elseif cmd=='HDEL' then native.call('HLEN',key); tracked(key); return 0
 elseif cmd=='EXPIRE' then return 1 end
 error('CM_CAPACITY_UNSUPPORTED_SCRIPT')
end}
local function run()
%s
end
local checked=run()
if not next(changed) then return checked end
if native.call('EXISTS',lock)==1 then return native.error_reply('CM_CAPACITY_RECONCILING') end
if native.call('HGET',meta,'ready')~='1' then return native.error_reply('CM_CAPACITY_NOT_READY') end
local used=tonumber(native.call('HGET',meta,'bytes') or '0'); local keys=tonumber(native.call('HGET',meta,'keys') or '0')
if used+growth>%s or keys+newkeys>10000 then return native.error_reply('CM_CAPACITY_BACKPRESSURE') end
redis=native
local result=run()
for key,_ in pairs(changed) do
 local old=tonumber(native.call('HGET',meta,'b:'..key) or '0'); local actual=native.call('MEMORY','USAGE',key) or 0
 local known=native.call('HEXISTS',meta,'k:'..key)==1
 native.call('HINCRBY',meta,'bytes',actual-old)
 if actual==0 then native.call('HDEL',meta,'b:'..key,'k:'..key); if known then native.call('HINCRBY',meta,'keys',-1) end
 else native.call('HSET',meta,'b:'..key,actual,'k:'..key,'1'); if not known then native.call('HINCRBY',meta,'keys',1) end end
end
return result`, keyCount+1, keyCount+2, fraction, fraction, script, limit)
}

func (s *ManagedTeamRedisSession) capacityCommand(ctx context.Context, args []string) ([]string, error) {
	keys, err := managedTeamCommandKeys(args)
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("claw:team:%d:", s.teamID)
	for _, key := range keys {
		proofRead := strings.EqualFold(args[0], "GET") && key == teamCapacityPrefix(s.teamID)+"replay-source-closed"
		if !strings.HasPrefix(key, prefix) || len(key) > 512 || (strings.Contains(key, teamRedisInternalNamespace) && !proofRead) {
			return nil, errManagedRedisKeyDenied
		}
	}
	cmd := strings.ToUpper(args[0])
	if cmd == "DEL" || cmd == "EXPIRE" {
		for _, key := range keys {
			if reliableTeamStream(key) || strings.Contains(key, ":completions:") {
				return nil, errManagedRedis
			}
		}
	}
	if cmd == "XADD" {
		if len(args) < 5 || !reliableTeamStream(args[1]) || args[2] != "*" || (len(args)-3)%2 != 0 {
			return nil, errManagedRedis
		}
		if err := s.bus.initializeTeamCapacity(ctx, s.teamID, false); err != nil {
			return nil, err
		}
		p := teamCapacityPrefix(s.teamID)
		fields := map[string]string{}
		for i := 3; i < len(args); i += 2 {
			fields[args[i]] = args[i+1]
		}
		terminal := "0"
		if reliableTerminal(fields) {
			terminal = "1"
		}
		out := []string{"EVAL", teamCapacityAppendScript, "4", args[1], p + "usage", p + "lock", p + "unused", "0", terminal}
		return append(out, args[3:]...), nil
	}
	if teamStateCommand(args) {
		if err := s.bus.initializeTeamCapacity(ctx, s.teamID, false); err != nil {
			return nil, err
		}
		return teamStateArgs(args), nil
	}
	if out, ok := teamGroupArgs(args); ok {
		if err := s.bus.initializeTeamCapacity(ctx, s.teamID, false); err != nil {
			return nil, err
		}
		return out, nil
	}
	if cmd == "XGROUP" {
		return nil, errManagedRedis
	}
	if cmd == "EVAL" {
		n, _ := strconv.Atoi(args[2])
		if n != 2 && n != 4 {
			return nil, errManagedRedis
		}
		if err := s.bus.initializeTeamCapacity(ctx, s.teamID, false); err != nil {
			return nil, err
		}
		p := teamCapacityPrefix(s.teamID)
		out := []string{"EVAL", capacityGuardedScript(args[1], n, n == 2), strconv.Itoa(n + 2)}
		out = append(out, args[3:3+n]...)
		out = append(out, p+"usage", p+"lock")
		return append(out, args[3+n:]...), nil
	}
	return args, nil
}

func managedTeamCommandKeys(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errManagedRedis
	}
	switch strings.ToUpper(args[0]) {
	case "PING", "CLIENT", "SELECT":
		return nil, nil
	case "EVAL":
		if len(args) < 3 {
			break
		}
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 1 || len(args) < 3+n {
			break
		}
		return args[3 : 3+n], nil
	case "XGROUP":
		if len(args) > 2 {
			return args[2:3], nil
		}
	case "XREAD", "XREADGROUP":
		for i := 1; i < len(args); i++ {
			if strings.EqualFold(args[i], "STREAMS") {
				n := (len(args) - i - 1) / 2
				if n < 1 || (len(args)-i-1)%2 != 0 {
					break
				}
				return args[i+1 : i+1+n], nil
			}
		}
	case "DEL":
		if len(args) > 1 {
			return args[1:], nil
		}
	default:
		if len(args) > 1 {
			return args[1:2], nil
		}
	}
	return nil, errManagedRedis
}
