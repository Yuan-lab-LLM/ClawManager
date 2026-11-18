package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

type shutdownDeliveryBatch struct {
	Deliveries []models.RuntimeShutdownDelivery `json:"deliveries"`
}

func readShutdownDeliveries(c *gin.Context) ([]models.RuntimeShutdownDelivery, bool) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var batch shutdownDeliveryBatch
	if decoder.Decode(&batch) != nil {
		utils.Error(c, 400, "Invalid shutdown delivery batch")
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || len(batch.Deliveries) < 1 || len(batch.Deliveries) > 32 {
		utils.Error(c, 400, "Invalid shutdown delivery batch")
		return nil, false
	}
	seen := map[string]bool{}
	for _, item := range batch.Deliveries {
		if item.Validate() != nil || seen[item.ID] {
			utils.Error(c, 400, "Invalid shutdown delivery")
			return nil, false
		}
		seen[item.ID] = true
	}
	return batch.Deliveries, true
}
func acceptShutdownDeliveries(c *gin.Context, repo repository.RuntimeShutdownAuditRepository, source string, deliveries []models.RuntimeShutdownDelivery, allow func(models.RuntimeShutdownEvent) bool) {
	recorder, ok := repo.(repository.RuntimeShutdownDeliveryRecorder)
	if !ok {
		utils.Error(c, 503, "Shutdown delivery storage unavailable")
		return
	}
	accepted := []string{}
	for _, item := range deliveries {
		if !allow(item.Event) {
			continue
		}
		err := recorder.RecordDelivery(c.Request.Context(), source, item)
		if errors.Is(err, models.ErrModelUnavailable) {
			continue
		} // Retain unacknowledged records at source.
		if err != nil {
			utils.Error(c, 503, "Shutdown delivery not confirmed")
			return
		}
		accepted = append(accepted, item.ID)
	}
	utils.Success(c, 200, "Shutdown deliveries processed", gin.H{"version": 1, "diagnostic_only": true, "accepted_ids": accepted})
}

// Do not expose auth-storage/driver details on the new diagnostic endpoints.
func (h *AgentHandler) authenticateShutdownAgentSession(c *gin.Context) (*services.AgentSession, bool) {
	c.Header("Cache-Control", "no-store")
	token := extractBearerToken(c.GetHeader("Authorization"))
	if token == "" {
		utils.Error(c, 401, "Agent session token is required")
		return nil, false
	}
	if h.agentService == nil {
		utils.Error(c, 503, "Agent session authentication unavailable")
		return nil, false
	}
	session, err := h.agentService.AuthenticateSession(token)
	if err != nil {
		utils.Error(c, 401, "Agent session unavailable")
		return nil, false
	}
	if session == nil || session.Instance == nil || session.Agent == nil {
		utils.Error(c, 503, "Agent session authentication unavailable")
		return nil, false
	}
	return session, true
}

func (h *AgentHandler) ReportShutdownDeliveries(c *gin.Context) {
	session, ok := h.authenticateShutdownAgentSession(c)
	if !ok {
		return
	}
	deliveries, ok := readShutdownDeliveries(c)
	if !ok {
		return
	}
	acceptShutdownDeliveries(c, h.shutdownAudits, "pro_agent", deliveries, func(e models.RuntimeShutdownEvent) bool {
		return session != nil && session.Instance != nil && session.Agent != nil && session.Agent.InstanceID == session.Instance.ID && e.InstanceID == session.Instance.ID && e.OwnerUserID == session.Instance.UserID
	})
}
func (h *AgentHandler) ShutdownDeliveryScope(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	session, ok := h.authenticateShutdownAgentSession(c)
	if !ok {
		return
	}
	if session == nil || session.Agent == nil || session.Instance == nil || session.Instance.ID <= 0 || session.Instance.UserID <= 0 || session.Agent.InstanceID != session.Instance.ID {
		utils.Error(c, 403, "Shutdown delivery scope unavailable")
		return
	}
	utils.Success(c, 200, "Shutdown delivery scope retrieved", gin.H{"version": 1, "instance_id": session.Instance.ID, "owner_user_id": session.Instance.UserID})
}

func (h *RuntimeAgentHandler) ReportShutdownDeliveries(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !h.requireAgentToken(c) {
		return
	}
	deliveries, ok := readShutdownDeliveries(c)
	if !ok {
		return
	}
	// The existing pool report credential authenticates this reporter. No
	// missing gateway is recreated and no old Pod assignment is inferred.
	acceptShutdownDeliveries(c, h.shutdownAudits, "lite_agent", deliveries, func(models.RuntimeShutdownEvent) bool { return true })
}
