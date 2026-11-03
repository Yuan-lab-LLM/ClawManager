package services

import (
	"clawreef/internal/models"
	"sort"
)

func applyModelAccessPolicy(model, current *models.LLMModel, req SaveLLMModelRequest) error {
	model.VisibilityScope = models.ModelVisibilityAll
	if current != nil {
		model.VisibilityScope = current.VisibilityScope
		model.AllowedUserIDs = append([]int{}, current.AllowedUserIDs...)
		model.AccessPolicyVersion = current.AccessPolicyVersion
	}
	if req.VisibilityScope == nil && req.AllowedUserIDs == nil {
		return nil
	}
	model.UpdateAccessPolicy = true
	if current != nil {
		if req.AccessPolicyVersion == nil || *req.AccessPolicyVersion != current.AccessPolicyVersion {
			return models.ErrModelAccessConflict
		}
	}
	if req.VisibilityScope != nil {
		model.VisibilityScope = *req.VisibilityScope
	}
	if req.AllowedUserIDs != nil {
		model.AllowedUserIDs = append([]int{}, (*req.AllowedUserIDs)...)
	}
	switch model.VisibilityScope {
	case models.ModelVisibilityAll:
		model.AllowedUserIDs = []int{}
	case models.ModelVisibilitySelected:
		if len(model.AllowedUserIDs) == 0 || len(model.AllowedUserIDs) > 10000 {
			return models.ErrInvalidModelAccess
		}
		seen := map[int]bool{}
		ids := []int{}
		for _, id := range model.AllowedUserIDs {
			if id <= 0 {
				return models.ErrInvalidModelAccess
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		sort.Ints(ids)
		model.AllowedUserIDs = ids
	default:
		return models.ErrInvalidModelAccess
	}
	// An unchanged policy is a metadata edit, not a grant replacement. In
	// particular, disabling an account must not prevent editing model pricing.
	if current != nil && model.VisibilityScope == current.VisibilityScope {
		previous := make(map[int]bool, len(current.AllowedUserIDs))
		for _, id := range current.AllowedUserIDs {
			previous[id] = true
		}
		same := len(previous) == len(model.AllowedUserIDs)
		for _, id := range model.AllowedUserIDs {
			if !previous[id] {
				same = false
			}
		}
		if same {
			model.UpdateAccessPolicy = false
		}
	}
	return nil
}
