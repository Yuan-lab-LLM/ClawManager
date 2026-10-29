package handlers

import (
	"context"
	"log"
	"net/http"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type runtimeGatewayInventory struct {
	models.InstanceRuntimeBinding
	InstanceName  string  `json:"instance_name,omitempty"`
	Owner         *string `json:"owner,omitempty"`
	InstanceState string  `json:"instance_state"`
}

func (h *RuntimePoolHandler) GetPodEvents(c *gin.Context) {
	id, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	pod, err := h.podRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if pod == nil {
		utils.Error(c, http.StatusNotFound, "runtime pod not found")
		return
	}
	if h.scheduler == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Kubernetes runtime events are unavailable")
		return
	}
	events, err := h.scheduler.RuntimePodEvents(c.Request.Context(), pod.Namespace, pod.PodName)
	if err != nil {
		utils.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	items := make([]gin.H, 0, len(events))
	for _, event := range events {
		items = append(items, gin.H{"type": event.Type, "reason": event.Reason, "message": event.Message, "count": event.Count, "first_seen_at": event.FirstTimestamp, "last_seen_at": event.LastTimestamp})
	}
	utils.Success(c, http.StatusOK, "Runtime pod events retrieved successfully", gin.H{"events": items})
}

func (h *RuntimePoolHandler) runtimeGatewayInventory(binding models.InstanceRuntimeBinding) runtimeGatewayInventory {
	gateway := runtimeGatewayInventory{InstanceRuntimeBinding: binding, InstanceState: binding.State}
	if h.scheduler != nil {
		instance, err := h.scheduler.RuntimeInstanceInfo(binding.InstanceID)
		if err != nil {
			log.Printf("runtime inventory instance %d: %v", binding.InstanceID, err)
		}
		if instance != nil {
			gateway.InstanceName = instance.Name
			gateway.Owner = instance.Owner
			gateway.InstanceState = instance.Status
		}
	}
	return gateway
}

func (h *RuntimePoolHandler) enrichRuntimeInventory(ctx context.Context, items []runtimePoolPodListItem) {
	byPod := map[int64][]models.InstanceRuntimeBinding{}
	reported := map[int64]bool{}
	if h.bindingRepo != nil {
		ids := make([]int64, 0, len(items))
		for _, pod := range items {
			if pod.ID > 0 {
				ids = append(ids, pod.ID)
			}
		}
		if len(ids) > 0 {
			bindings, err := h.bindingRepo.ListByRuntimePodIDs(ctx, ids)
			if err != nil {
				log.Printf("runtime inventory batch bindings: %v", err)
				// Retain per-Pod availability when one batch query fails.
				for _, id := range ids {
					perPod, err := h.bindingRepo.ListByRuntimePodID(ctx, id)
					if err != nil {
						log.Printf("runtime inventory bindings for pod %d: %v", id, err)
						continue
					}
					byPod[id], reported[id] = perPod, true
				}
			} else {
				for _, id := range ids {
					reported[id] = true
				}
				for _, binding := range bindings {
					byPod[binding.RuntimePodID] = append(byPod[binding.RuntimePodID], binding)
				}
			}
		}
	}
	for i := range items {
		pod := &items[i]
		if pod.SchedulingPaused {
			enabled := false
			pod.SchedulingEnabled = &enabled
		}
		// Legacy process pools have no per-container health signal.
		if len(pod.Slots) == 0 {
			pod.Slots = make([]models.RuntimeSlot, 0, pod.Capacity)
			for index := 1; index <= pod.Capacity; index++ {
				pod.Slots = append(pod.Slots, models.RuntimeSlot{Index: index, State: "unknown"})
			}
		}
		if !reported[pod.ID] {
			continue
		}
		pod.BindingsReported = true
		configuredPorts := false
		for _, slot := range pod.Slots {
			if slot.GatewayPort != nil {
				configuredPorts = true
				break
			}
		}
		for _, binding := range byPod[pod.ID] {
			block := services.RuntimeGatewayPortBlockSize(pod.RuntimeType)
			index := -1
			for slotIndex, slot := range pod.Slots {
				if slot.GatewayPort == nil {
					continue
				}
				if *slot.GatewayPort == binding.GatewayPort {
					index = slotIndex
					break
				}
			}
			if !configuredPorts && binding.GatewayPort >= services.RuntimeGatewayPortStart && (binding.GatewayPort-services.RuntimeGatewayPortStart)%block == 0 {
				index = (binding.GatewayPort - services.RuntimeGatewayPortStart) / block
			}
			if index < 0 || index >= len(pod.Slots) {
				continue
			}
			slot := &pod.Slots[index]
			gateway := h.runtimeGatewayInventory(binding)
			id, port, generation := binding.InstanceID, binding.GatewayPort, binding.Generation
			slot.InstanceID = &id
			slot.GatewayPort = &port
			slot.Generation = &generation
			slot.GatewayID = binding.GatewayID
			slot.InstanceName = gateway.InstanceName
			slot.Owner = gateway.Owner
			slot.InstanceState = gateway.InstanceState
			slot.ErrorMessage = binding.ErrorMessage
		}
	}
}
