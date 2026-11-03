package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func ownerTeamMemberProvisionID(teamID int, memberKey string) string {
	key := sha256.Sum256([]byte(fmt.Sprintf("owner-team-%d-member-%s", teamID, memberKey)))
	return hex.EncodeToString(key[:])
}

func (s *teamService) provisionOwnerTeamK8s(team *models.Team, redisURL string) (*teamRuntimeSecrets, error) {
	ctx := context.Background()
	pvc, err := s.pvcService.CreateTeamSharedPVC(ctx, team.UserID, team.ID, 10, "")
	if err != nil {
		return nil, err
	}
	name := s.pvcService.GetClient().GetTeamSecretName(team.ID)
	token, err := generatePrefixedToken("team")
	if err != nil {
		return nil, err
	}
	values, err := s.secretService.CreateOwnerTeamSecret(ctx, team.UserID, team.ID, name, map[string]string{teamRedisURLSecretKey: redisURL, teamTokenSecretKey: token})
	if err != nil {
		return nil, err
	}
	team.RedisURLSecretName = &name
	team.RedisURLSecretKey = optionalString(teamRedisURLSecretKey)
	team.TeamTokenSecretName = &name
	team.TeamTokenSecretKey = optionalString(teamTokenSecretKey)
	team.SharedPVCName = &pvc.Name
	team.SharedPVCNamespace = &pvc.Namespace
	team.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateTeam(team); err != nil {
		return nil, err
	}
	return &teamRuntimeSecrets{RedisURL: values[teamRedisURLSecretKey], Token: values[teamTokenSecretKey]}, nil
}

// OwnerTeamProvisioner reuses the existing runtime/roster/instance builders.
// The caller must reserve the team and frozen request in the operation ledger.
type OwnerTeamProvisioner interface {
	ProvisionOwnerTeam(context.Context, *models.Team, CreateTeamRequest, string, *repository.OwnerTeamRepository) error
	CheckCMTeam(userID, teamID int) error
}

func (s *teamService) ValidateOwnerTeamCreate(userID int, owner string, req CreateTeamRequest) error {
	plans, err := planTeamMembers(req.Name, req.Members)
	if err != nil {
		return err
	}
	team := &models.Team{UserID: userID, Name: "owner-team-admission", Owner: &owner, ResourceScope: models.TeamScopeNorthbound}
	return s.instanceService.ValidateCreateRequests(userID, s.buildTeamMemberInstanceRequests(team, plans))
}

func (s *teamService) deleteTeamMemberInstance(team *models.Team, instanceID int) error {
	if team.ResourceScope == models.TeamScopeNorthbound {
		deleter, ok := s.instanceService.(interface{ DeleteForOwnerTeam(int, int) error })
		if !ok {
			return fmt.Errorf("Owner Team deletion guard unavailable")
		}
		return deleter.DeleteForOwnerTeam(team.ID, instanceID)
	}
	return s.instanceService.Delete(instanceID)
}

func (s *teamService) CheckCMTeam(userID, teamID int) error {
	team, err := s.repo.GetTeamByID(teamID)
	if err != nil {
		return err
	}
	if team == nil || team.ValidateScope() != nil || !team.IsCM() {
		return fmt.Errorf("team not found")
	}
	if team.UserID != userID {
		return fmt.Errorf("access denied")
	}
	return nil
}

func (s *teamService) ProvisionOwnerTeam(ctx context.Context, team *models.Team, req CreateTeamRequest, operationID string, steps *repository.OwnerTeamRepository) error {
	if team == nil || team.IsCM() || team.ValidateScope() != nil || steps == nil || strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("invalid Owner Team provisioning identity")
	}
	plans, err := planTeamMembers(team.VisibleName(), req.Members)
	if err != nil {
		return err
	}
	keys := []string{"infrastructure"}
	for _, plan := range plans {
		keys = append(keys, "member:"+plan.MemberKey)
	}
	if done, err := steps.ProvisionStepsDone(team.ID, operationID, keys); err != nil {
		return err
	} else if done {
		return s.finishOwnerTeamProvision(ctx, team)
	}
	var pending []CreateInstanceRequest
	for _, plan := range plans {
		member, err := s.repo.GetMemberByTeamKey(team.ID, plan.MemberKey)
		if err != nil {
			return err
		}
		if member == nil || member.InstanceID == nil {
			if lookup, ok := s.instanceService.(interface {
				GetByProvisioningOperationID(string) (*models.Instance, error)
			}); ok {
				existing, err := lookup.GetByProvisioningOperationID(ownerTeamMemberProvisionID(team.ID, plan.MemberKey))
				if err != nil {
					return err
				}
				if existing != nil {
					if existing.UserID != team.UserID || existing.Owner == nil || !strings.EqualFold(*existing.Owner, *team.Owner) {
						return fmt.Errorf("member provisioning ownership conflict")
					}
					continue
				}
			}
			pending = append(pending, s.buildTeamMemberInstanceRequest(team, plan))
		}
	}
	if len(pending) > 0 {
		if err := s.instanceService.ValidateCreateRequests(team.UserID, pending); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	start, err := steps.StartStep(team.ID, "infrastructure", operationID)
	if err != nil {
		// Persisted secret/PVC references prove this step reached its commit.
		// Otherwise do not rotate secrets or claim arbitrary existing resources.
		if team.SharedPVCName == nil || team.TeamTokenSecretName == nil || team.RedisURLSecretName == nil {
			return err
		}
		if err := steps.CompleteStep(team.ID, "infrastructure", operationID); err != nil {
			return err
		}
	}
	var secrets *teamRuntimeSecrets
	if start {
		redisURL := defaultTeamRedisURL()
		if redisURL == "" {
			_ = steps.FailStep(team.ID, "infrastructure", operationID)
			return fmt.Errorf("team Redis is not configured")
		}
		secrets, err = s.provisionOwnerTeamK8s(team, redisURL)
		if err != nil {
			_ = steps.FailStep(team.ID, "infrastructure", operationID)
			return err
		}
		if err := steps.CompleteStep(team.ID, "infrastructure", operationID); err != nil {
			return err
		}
	} else {
		if team.RedisURLSecretName == nil || team.RedisURLSecretKey == nil || team.TeamTokenSecretName == nil || team.TeamTokenSecretKey == nil {
			return fmt.Errorf("Team secret references are incomplete")
		}
		client := s.pvcService.GetClient()
		redisURL, err := s.secretService.GetSecretValue(ctx, client.GetNamespace(team.UserID), *team.RedisURLSecretName, *team.RedisURLSecretKey)
		if err != nil {
			return err
		}
		token, err := s.secretService.GetSecretValue(ctx, client.GetNamespace(team.UserID), *team.TeamTokenSecretName, *team.TeamTokenSecretKey)
		if err != nil {
			return err
		}
		secrets = &teamRuntimeSecrets{RedisURL: redisURL, Token: token}
	}
	// Roster writes are deterministic, use the frozen spec, and do not call models.
	rosterJSON, err := s.upsertTeamRosterConfig(team.UserID, team, plans)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := "member:" + plan.MemberKey
		start, stepErr := steps.StartStep(team.ID, key, operationID)
		member, err := s.repo.GetMemberByTeamKey(team.ID, plan.MemberKey)
		if err != nil {
			return err
		}
		if !start {
			// Idle is written only after managed identity files have committed.
			// A running instance alone is not proof of a complete member step.
			if stepErr != nil && (member == nil || member.Status != models.TeamMemberStatusIdle) {
				return stepErr
			}
			if member == nil || member.InstanceID == nil {
				if stepErr != nil {
					return stepErr
				}
				return fmt.Errorf("member binding missing")
			}
			instance, err := s.instanceService.GetByID(*member.InstanceID)
			if err != nil {
				return err
			}
			if instance == nil || instance.Owner == nil || !strings.EqualFold(*instance.Owner, *team.Owner) || instance.UserID != team.UserID || instance.ProvisioningOperationID == nil || *instance.ProvisioningOperationID != ownerTeamMemberProvisionID(team.ID, plan.MemberKey) {
				return fmt.Errorf("member runtime ownership or readiness is not confirmed")
			}
			if err := steps.CompleteStep(team.ID, key, operationID); err != nil {
				return err
			}
			continue
		}
		member, err = s.createTeamMemberInstance(team.UserID, team, plan, secrets, rosterJSON)
		if err != nil {
			_ = steps.FailStep(team.ID, key, operationID)
			return err
		}
		member.Status, member.UpdatedAt = models.TeamMemberStatusIdle, time.Now().UTC()
		if err := s.repo.UpdateMember(member); err != nil {
			return err
		}
		if err := steps.CompleteStep(team.ID, key, operationID); err != nil {
			return err
		}
	}
	return s.finishOwnerTeamProvision(ctx, team)
}
