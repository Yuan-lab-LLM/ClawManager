package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const teamProjectionCommandDestination = "internal:team-projection-command"

// Projection uses its own service value: no copied locks, worker maps, or
// temporary mutation of the serving service's repository. The selected shared
// dependencies are read-only on this call chain.
func (s *teamService) projectionService(repo repository.TeamRepository) *teamService {
	return &teamService{repo: repo, power: s.power, instanceService: s.instanceService,
		openClawConfigPlanner: s.openClawConfigPlanner, pvcService: s.pvcService, secretService: s.secretService,
		configMapService: s.configMapService, podService: s.podService, ctx: s.ctx,
		runtimeWorkspaceRoot: s.runtimeWorkspaceRoot, workspaceText: s.workspaceText}
}

type teamProjectionCommands struct {
	commands [][]string
	err      error
}

// Only audited effects are deferred. Conditional locks/admission must never
// get an invented success result. In particular NX/XX SET and arbitrary Lua
// cannot silently become asynchronous operations.
func (q *teamProjectionCommands) intercept(_ context.Context, args []string) (any, bool, error) {
	if len(args) == 0 {
		return nil, false, nil
	}
	switch strings.ToUpper(args[0]) {
	case "GET", "XRANGE", "XREVRANGE", "EXISTS", "MGET", "PING":
		return nil, false, nil
	case "SET":
		for _, arg := range args[3:] {
			if strings.EqualFold(arg, "NX") || strings.EqualFold(arg, "XX") {
				q.err = fmt.Errorf("conditional Redis SET is not allowed during Team projection")
				return nil, true, q.err
			}
		}
		q.commands = append(q.commands, append([]string(nil), args...))
		return "OK", true, nil
	case "XADD":
		if len(args) < 5 || args[2] != "*" {
			q.err = fmt.Errorf("unsupported projection XADD shape")
			return nil, true, q.err
		}
		q.commands = append(q.commands, append([]string(nil), args...))
		return "", true, nil
	case "EVAL":
		if len(args) == 5 && args[1] == publishTeamWorkflowContextScript && args[2] == "1" {
			q.commands = append(q.commands, append([]string(nil), args...))
			return int64(1), true, nil
		}
	}
	q.err = fmt.Errorf("Redis command %s is not allowed during Team projection", args[0])
	return nil, true, q.err
}

func (s *teamService) projectTeamEventAtomic(ctx context.Context, team *models.Team, bus *redisBus, message redisStreamMessage, advance bool, retry *models.TeamEventOutbox) error {
	repo := s.repo.(repository.TeamProjectionRepository)
	business := eventString(mergeRedisEventPayload(message.Fields), "eventId", "event_id")
	committed, err := repo.WithTeamProjection(ctx, team.ID, message.ID, business, advance, func(scoped repository.TeamRepository, current *models.Team) error {
		// Legacy partial projections did not atomically record completion. Their
		// source row alone cannot prove either success or rollback. Preserve them
		// for explicit reconciliation instead of repeating an unknown ledger write.
		if retry != nil && !strings.HasPrefix(retry.SourceEventID, "projection-v2:") {
			exists, e := scoped.EventExistsByStreamID(team.ID, message.ID)
			if e != nil {
				return e
			}
			if exists {
				return fmt.Errorf("legacy Team projection %d has ambiguous partial writes; reconciliation required", retry.ID)
			}
		}
		worker := s.projectionService(scoped)
		queued := &teamProjectionCommands{}
		var deferredBus *redisBus
		if bus != nil {
			copy := *bus
			copy.commandHook = queued.intercept
			deferredBus = &copy
		}
		if err := worker.projectTeamEventAttempt(current, deferredBus, message, false); err != nil {
			return err
		}
		if queued.err != nil {
			return queued.err
		}
		for i, args := range queued.commands {
			encoded, e := json.Marshal(args)
			if e != nil {
				return e
			}
			id := boundedTeamEventID("projection-command", team.ID, message.ID, i)
			if e = scoped.CreateEventOutbox(&models.TeamEventOutbox{TeamID: team.ID, SourceEventID: id, MessageID: id, Destination: teamProjectionCommandDestination, PayloadJSON: string(encoded)}); e != nil {
				return e
			}
			if sources, ok := scoped.(repository.TeamProjectionCommandSourceRepository); ok {
				var source map[string]interface{}
				_ = json.Unmarshal([]byte(message.Fields["payload"]), &source)
				raw := eventString(source, "rootTaskId", "root_task_id", "taskId", "task_id")
				if raw == "" {
					raw = message.Fields["task_id"]
				}
				raw = strings.TrimPrefix(raw, fmt.Sprintf("team-%d-task-", team.ID))
				taskID, _ := strconv.Atoi(raw)
				if e = sources.RegisterProjectionCommandSource(ctx, team.ID, id, message.ID, taskID); e != nil {
					return e
				}
			}
		}
		if retry != nil {
			return scoped.MarkEventOutboxDelivered(retry.ID, time.Now().UTC())
		}
		return nil
	})
	if err != nil {
		return err
	}
	if advance {
		team.RedisEventsLastID = committed.RedisEventsLastID
		team.UpdatedAt = committed.UpdatedAt
	}
	return nil
}

func (s *teamService) projectConsumedTeamEventAtomic(ctx context.Context, team *models.Team, bus *redisBus, message redisStreamMessage) bool {
	if err := s.projectTeamEventAtomic(ctx, team, bus, message, true, nil); err == nil {
		return true
	} else {
		fmt.Printf("Warning: Team %d projection %s rolled back; preserving original event\n", team.ID, message.ID)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return false
	}
	id := boundedTeamEventID("projection-v2", team.ID, message.ID)
	outbox := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: id, MessageID: id, Destination: teamProjectionRetryDestination, PayloadJSON: string(encoded), AvailableAt: time.Now().UTC().Add(15 * time.Second)}
	committed, err := s.repo.(repository.TeamProjectionRepository).PreserveTeamProjection(ctx, team.ID, message.ID, outbox)
	if err != nil {
		return false
	}
	team.RedisEventsLastID = committed.RedisEventsLastID
	team.UpdatedAt = committed.UpdatedAt
	return true
}

func (s *teamService) deliverProjectionCommand(ctx context.Context, team *models.Team, bus *redisBus, outbox *models.TeamEventOutbox) error {
	var args []string
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &args); err != nil {
		return err
	}
	if len(args) < 2 {
		return errors.New("invalid Team projection command")
	}
	// The durable command lane is internal, but fail closed if its destination
	// was corrupted: it must never become an unrestricted Redis command proxy.
	key := args[1]
	if args[0] == "EVAL" {
		if len(args) != 5 || args[1] != publishTeamWorkflowContextScript || args[2] != "1" {
			return errors.New("invalid Team workflow publication")
		}
		key = args[3]
	}
	prefix := fmt.Sprintf("claw:team:%d:", team.ID)
	if !strings.HasPrefix(key, prefix) {
		return errors.New("projection command crosses Team namespace")
	}
	switch args[0] {
	case "XADD":
		if len(args) < 5 || args[2] != "*" || len(args)%2 != 1 {
			return errors.New("invalid projection append")
		}
		fields := map[string]string{}
		for i := 3; i < len(args); i += 2 {
			fields[args[i]] = args[i+1]
		}
		_, err := bus.xAddReliableOnce(ctx, key, fields, prefix+"__cm_capacity:receipt:"+boundedTeamEventID("projection", outbox.MessageID))
		return err
	case "SET":
		if len(args) != 3 && len(args) != 5 {
			return errors.New("invalid projection SET")
		}
		if len(args) == 5 && !strings.EqualFold(args[3], "PX") && !strings.EqualFold(args[3], "EX") {
			return errors.New("conditional projection SET forbidden")
		}
		_, err := bus.do(ctx, args...)
		return err
	case "EVAL":
		_, err := bus.do(ctx, args...)
		return err
	default:
		return errors.New("invalid Team projection command kind")
	}
}
