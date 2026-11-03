package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
)

func (r *teamRepository) TeamMemberRuntimeFacts(ctx context.Context, ids []int) (map[int]models.TeamMemberRuntimeFacts, error) {
	result := map[int]models.TeamMemberRuntimeFacts{}
	if len(ids) == 0 {
		return result, nil
	}
	sess := r.sess.WithContext(ctx)
	var instances []models.Instance
	if err := sess.Collection("instances").Find(db.Cond{"id IN": ids}).All(&instances); err != nil {
		return nil, err
	}
	for _, i := range instances {
		result[i.ID] = models.TeamMemberRuntimeFacts{Instance: i}
	}
	var bindings []models.InstanceRuntimeBinding
	if err := sess.Collection("instance_runtime_bindings").Find(db.Cond{"instance_id IN": ids}).All(&bindings); err != nil {
		return nil, err
	}
	var podIDs []int64
	for idx := range bindings {
		b := bindings[idx]
		f, ok := result[b.InstanceID]
		if !ok {
			continue
		}
		f.Binding = &b
		result[b.InstanceID] = f
		podIDs = append(podIDs, b.RuntimePodID)
	}
	if len(podIDs) == 0 {
		return result, nil
	}
	var pods []models.RuntimePod
	if err := sess.Collection("runtime_pods").Find(db.Cond{"id IN": podIDs}).All(&pods); err != nil {
		return nil, err
	}
	byID := map[int64]*models.RuntimePod{}
	for idx := range pods {
		p := pods[idx]
		byID[p.ID] = &p
	}
	for id, f := range result {
		if f.Binding != nil {
			f.Runtime = byID[f.Binding.RuntimePodID]
			result[id] = f
		}
	}
	return result, nil
}
