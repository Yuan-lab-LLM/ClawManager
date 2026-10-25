package northbound

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

// Workspace routes use the normal app JWT, never a caller-supplied UserID or
// Owner. They reuse the durable single-instance lifecycle worker.
func (s *CoreService) RegisterBatchRoutes(group *gin.RouterGroup) {
	group.POST("/batch/lifecycle", s.createBatchHTTP)
	group.GET("/batch/lifecycle", s.listBatchesHTTP)
	group.GET("/batch/lifecycle/:batch", s.batchHTTP)
	group.POST("/batch/lifecycle/:batch/:action", s.controlBatchHTTP)
}

func normalizeBatchRequest(ids []int, action string) ([]int, error) {
	if action != "restart" && action != "reset" {
		return nil, fmt.Errorf("action must be restart or reset")
	}
	if len(ids) == 0 || len(ids) > 500 {
		return nil, fmt.Errorf("select 1 to 500 Lite instances")
	}
	seen := map[int]bool{}
	out := []int{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid instance id")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out, nil
}

func (s *CoreService) createBatchHTTP(c *gin.Context) {
	var req struct {
		InstanceIDs         []int  `json:"instance_ids"`
		Action              string `json:"action"`
		RequestID           string `json:"request_id"`
		ConfirmDataDeletion bool   `json:"confirm_data_deletion"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	ids, err := normalizeBatchRequest(req.InstanceIDs, req.Action)
	if err != nil {
		utils.Error(c, 400, err.Error())
		return
	}
	if len(req.RequestID) < 8 || len(req.RequestID) > 128 {
		utils.Error(c, 400, "request_id must contain 8 to 128 characters")
		return
	}
	if req.Action == "reset" && !req.ConfirmDataDeletion {
		utils.Error(c, 400, "Reset deletes all old instance data; explicit confirmation required")
		return
	}
	userID := c.GetInt("userID")
	payload, _ := json.Marshal(struct {
		IDs    []int
		Action string
	}{ids, req.Action})
	now := time.Now().UTC()
	batch := &models.InstanceLifecycleBatch{BatchID: "batch_" + sha256Hex(fmt.Sprintf("%d:%s", userID, req.RequestID))[:40], UserID: userID, Action: req.Action, Status: "running", RequestHash: sha256Hex(string(payload)), CreatedAt: now, UpdatedAt: now}
	batch.PolicyVersion = 2
	items := []models.InstanceLifecycleBatchItem{}
	ops := []*models.NorthboundOperation{}
	for _, id := range ids {
		instance, err := s.instances.GetByID(id)
		if err != nil {
			utils.Error(c, 503, "Unable to verify instance")
			return
		}
		if instance == nil || (instance.UserID != userID && c.GetString("userRole") != "admin") {
			utils.Error(c, 404, "Instance not found")
			return
		}
		if instance.InstanceMode != "lite" || !isSupportedNorthboundType(instance.Type) {
			utils.Error(c, 400, "Only standalone supported Lite instances are supported")
			return
		}
		opID := "op_" + sha256Hex(fmt.Sprintf("%s:%d", batch.BatchID, id))[:40]
		body, _ := json.Marshal(InstanceLifecycleRequest{InstanceID: id})
		ops = append(ops, &models.NorthboundOperation{OperationID: opID, UserID: instance.UserID, SessionID: batch.BatchID, OperationType: "lite_instance_" + req.Action, IdempotencyKeyHash: sha256Hex(opID), RequestHash: sha256Hex(string(body)), RequestPayload: string(body), Status: "batch_pending", AvailableAt: now, InstanceID: &ids[len(items)], CreatedAt: now, UpdatedAt: now})
		items = append(items, models.InstanceLifecycleBatchItem{BatchID: batch.BatchID, SourceID: id, SourceName: instance.Name, RuntimeType: instance.Type, OperationID: opID, State: "pending"})
	}
	if err := s.repo.CreateLifecycleBatch(c.Request.Context(), batch, items, ops, c.GetString("userRole") == "admin"); err != nil {
		utils.Error(c, 409, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Batch queued", batch)
}

func (s *CoreService) listBatchesHTTP(c *gin.Context) {
	items, err := s.repo.ListLifecycleBatches(c.GetInt("userID"))
	if err != nil {
		utils.Error(c, 503, "Unable to load batches")
		return
	}
	utils.Success(c, 200, "Batches", items)
}

func (s *CoreService) batchHTTP(c *gin.Context) {
	b, err := s.repo.GetLifecycleBatch(c.Param("batch"), c.GetInt("userID"))
	if err != nil {
		utils.Error(c, 404, "Batch not found")
		return
	}
	items, err := s.repo.LifecycleBatchItems(b.BatchID)
	if err != nil {
		utils.Error(c, 503, "Unable to load batch items")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	start := (page - 1) * 50
	counts := map[string]int{}
	details := []gin.H{}
	opIDs := []string{}
	itemIDs := []int64{}
	for i, item := range items {
		if i >= start && i < start+50 {
			opIDs = append(opIDs, item.OperationID)
			itemIDs = append(itemIDs, item.ID)
		}
	}
	ops, err := s.repo.LifecycleBatchOperations(opIDs)
	if err != nil {
		utils.Error(c, 503, "Unable to load operation progress")
		return
	}
	byID := map[string]models.NorthboundOperation{}
	histories, err := s.repo.LifecycleAttemptHistories(itemIDs)
	if err != nil {
		utils.Error(c, 503, "Unable to load attempt history")
		return
	}
	for _, op := range ops {
		byID[op.OperationID] = op
	}
	for i, item := range items {
		counts[item.State]++
		if i >= start && i < start+50 {
			op := byID[item.OperationID]
			details = append(details, gin.H{"item": item, "operation": op, "attempts": histories[item.ID]})
		}
	}
	utils.Success(c, 200, "Batch progress", gin.H{"batch": b, "counts": counts, "total": len(items), "page": page, "page_size": 50, "items": details, "limits": gin.H{"per_action": 5, "global": 10, "per_pod": 5}})
}

func (s *CoreService) controlBatchHTTP(c *gin.Context) {
	if err := s.repo.ControlLifecycleBatch(c.Request.Context(), c.Param("batch"), c.GetInt("userID"), c.Param("action")); err != nil {
		utils.Error(c, 409, err.Error())
		return
	}
	utils.Success(c, 200, "Batch updated", nil)
}

// Reject existing app mutations while the batch owns an instance. The same
// reservation is visible to northbound SubmitLifecycle via pending operations.
func (s *CoreService) BatchMutationGuard(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		c.Next()
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		c.Next()
		return
	}
	path := c.FullPath()
	if strings.HasSuffix(path, "/access") {
		busy, err := s.repo.HasBatchReservation(id)
		if err != nil || busy {
			utils.Error(c, 409, "Instance lifecycle operation is in progress")
			c.Abort()
			return
		}
		c.Next()
		return
	}
	if !(c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodPut || strings.HasSuffix(path, "/restart") || strings.HasSuffix(path, "/stop") || strings.HasSuffix(path, "/start") || strings.Contains(path, "/:id/runtime/")) {
		c.Next()
		return
	}
	release, err := s.repo.ReserveInstanceMutations([]int{id})
	if err != nil {
		utils.Error(c, 409, "Instance belongs to an unfinished batch; pause/cancel pending work first")
		c.Abort()
		return
	}
	defer release()
	c.Next()
}
