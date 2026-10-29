package handlers

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type runtimePodPageQuery struct {
	page, size               int
	search, node, scheduling string
}

func parseRuntimePodPage(c *gin.Context) (*runtimePodPageQuery, error) {
	values, present := c.Request.URL.Query()["page"]
	if !present {
		return nil, nil
	}
	page, err := strconv.Atoi(values[0])
	if err != nil || page <= 0 {
		return nil, errors.New("page must be a positive integer")
	}
	size := 4
	if values, present := c.Request.URL.Query()["page_size"]; present {
		size, err = strconv.Atoi(values[0])
		if err != nil || size < 1 || size > 100 {
			return nil, errors.New("page_size must be between 1 and 100")
		}
	}
	scheduling := strings.TrimSpace(c.Query("scheduling"))
	if scheduling != "" && scheduling != "all" && scheduling != "enabled" && scheduling != "disabled" {
		return nil, errors.New("scheduling must be all, enabled, or disabled")
	}
	return &runtimePodPageQuery{page: page, size: size, search: strings.ToLower(strings.TrimSpace(c.Query("search"))), node: strings.TrimSpace(c.Query("node")), scheduling: scheduling}, nil
}

func runtimePodSchedulable(pod runtimePoolPodListItem) bool {
	return pod.ID > 0 && pod.AgentReported && pod.State == "ready" && !pod.Draining && !pod.SchedulingPaused && (pod.SchedulingEnabled == nil || *pod.SchedulingEnabled) && pod.PoolPurpose != "openclaw-upgrade-lab" && (pod.PoolRole == "" || pod.PoolRole == "upgrade-target" && pod.SchedulingEnabled != nil && *pod.SchedulingEnabled)
}

func runtimeInventorySlotState(slot models.RuntimeSlot) string {
	if !slot.Ready {
		return "unavailable"
	}
	if slot.InstanceID == nil || *slot.InstanceID == 0 {
		return "idle"
	}
	switch slot.InstanceState {
	case "running", "ready", "active":
		return "running"
	case "starting", "creating", "pending", "reserved":
		return "starting"
	case "migrating", "upgrading", "draining", "stopping":
		return "migrating"
	default:
		return "error"
	}
}

func runtimePodMatchesSearch(pod runtimePoolPodListItem, search string) bool {
	if search == "" {
		return true
	}
	fields := []string{pod.Namespace, pod.PodName, pod.RuntimeType, strconv.FormatInt(pod.ID, 10)}
	for _, value := range []*string{pod.NodeName, pod.PodIP} {
		if value != nil {
			fields = append(fields, *value)
		}
	}
	for _, slot := range pod.Slots {
		fields = append(fields, slot.InstanceName)
		if slot.Owner != nil {
			fields = append(fields, *slot.Owner)
		}
		if slot.InstanceID != nil {
			fields = append(fields, strconv.Itoa(*slot.InstanceID))
		}
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), search) {
			return true
		}
	}
	return false
}

func runtimePodPage(items []runtimePoolPodListItem, query runtimePodPageQuery, runtimeType string, now time.Time) gin.H {
	summary := map[string]int{}
	for _, key := range []string{"pods", "online", "schedulable", "capacity", "ready", "occupied", "available", "running", "starting", "migrating", "error", "idle", "unavailable"} {
		summary[key] = 0
	}
	nodeSet := map[string]bool{}
	filtered := make([]runtimePoolPodListItem, 0, len(items))
	for _, pod := range items {
		summary["pods"]++
		if pod.State == "ready" || pod.State == "draining" {
			summary["online"]++
		}
		schedulable := runtimePodSchedulable(pod)
		if schedulable {
			summary["schedulable"]++
		}
		summary["capacity"] += pod.Capacity
		ready, bound, idle := 0, 0, 0
		for _, slot := range pod.Slots {
			if slot.Ready {
				ready++
			}
			if slot.InstanceID != nil && *slot.InstanceID != 0 {
				bound++
			}
			state := runtimeInventorySlotState(slot)
			if state == "idle" {
				idle++
			}
			if state != "unavailable" {
				summary[state]++
			}
		}
		summary["ready"] += ready
		summary["occupied"] += max(bound, pod.UsedSlots)
		summary["unavailable"] += max(0, pod.Capacity-ready)
		if schedulable && pod.BindingsReported && bound >= pod.UsedSlots {
			summary["available"] += idle
		}
		if pod.NodeName != nil && strings.TrimSpace(*pod.NodeName) != "" {
			nodeSet[*pod.NodeName] = true
		}
		if runtimeType != "" && pod.RuntimeType != runtimeType || query.node != "" && query.node != "all" && (pod.NodeName == nil || *pod.NodeName != query.node) || query.scheduling != "" && query.scheduling != "all" && schedulable != (query.scheduling == "enabled") || !runtimePodMatchesSearch(pod, query.search) {
			continue
		}
		filtered = append(filtered, pod)
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.PodName != b.PodName {
			return a.PodName < b.PodName
		}
		return a.ID < b.ID
	})
	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	total := len(filtered)
	totalPages := (total + query.size - 1) / query.size
	page := make([]runtimePoolPodListItem, 0)
	// Compare pages before multiplying so an arbitrarily large page cannot overflow.
	if query.page <= totalPages {
		start := (query.page - 1) * query.size
		page = filtered[start:min(start+query.size, total)]
	}
	return gin.H{"pods": page, "pagination": gin.H{"page": query.page, "page_size": query.size, "total": total, "total_pages": totalPages}, "summary": summary, "nodes": nodes, "updated_at": now.UTC().Format(time.RFC3339)}
}
