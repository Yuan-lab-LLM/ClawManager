package services

import (
	"clawreef/internal/models"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const teamWorkspaceHelperCommand = "team-workspace-helper"

type teamNativeRequest struct {
	Root, RuntimeType, Action              string
	UserID, TeamID, InstanceID, Generation int
	Directories                            []string
	Identity, Context                      map[string]string
}

func validateTeamNativeRequest(r teamNativeRequest) (uid, gid int, path string, err error) {
	invalid := func() (int, int, string, error) { return 0, 0, "", ErrWorkspacePathInvalid }
	if !filepath.IsAbs(r.Root) || filepath.Clean(r.Root) != r.Root || r.Root == "/" || r.UserID < 1 || r.Generation < 0 {
		return invalid()
	}
	for name, text := range r.Identity {
		if name != teamAgentsFileName && name != teamSoulFileName || len(text) > 1<<20 {
			return invalid()
		}
	}
	for name, text := range r.Context {
		if name != teamConfigFileName && name != teamIntroductionFileName || len(text) > 1<<20 {
			return invalid()
		}
	}
	for _, directory := range r.Directories {
		clean, err := cleanWorkspaceRelativePath(directory)
		if err != nil || clean != filepath.ToSlash(directory) {
			return invalid()
		}
	}
	if r.InstanceID > 0 {
		if r.TeamID != 0 {
			return invalid()
		}
		switch r.RuntimeType {
		case RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness:
		default:
			return invalid()
		}
		uid = RuntimeLinuxID(r.InstanceID)
		if uid <= RuntimeLinuxIDBase || uint64(uid) >= 1<<32-1 {
			return invalid()
		}
		gid = uid
		path = RuntimeWorkspacePathWithRoot(r.Root, r.RuntimeType, r.UserID, r.InstanceID)
		switch r.Action {
		case "grant_instance":
			if len(r.Identity)+len(r.Context)+len(r.Directories) != 0 {
				return invalid()
			}
			uid = 10001
		case "identity", "contexts":
			if len(r.Directories) != 0 {
				return invalid()
			}
		default:
			return invalid()
		}
		return uid, gid, path, nil
	}
	if r.TeamID < 1 || r.RuntimeType != "" || r.Action != "shared" || len(r.Identity) != 0 {
		return invalid()
	}
	return 10001, teamSharedGID, filepath.Join(r.Root, "teams", fmt.Sprintf("user-%d", r.UserID), fmt.Sprintf("team-%d-shared", r.TeamID)), nil
}
func (s *teamService) nativeTeamIO() bool {
	return s != nil && s.workspaceText != nil && s.workspaceText.nfsProfile == "nfs-rootless-v1"
}

// SQL intents outlive a failed/lost response. Only reaping the fixed helper (it
// spawns no subprocesses) proves that this independent filesystem writer ended.
func (s *teamService) runNativeTeamMutation(ctx context.Context, scope WorkspaceFileScope, request teamNativeRequest) error {
	files := s.workspaceText
	if files == nil || files.nfsWrites == nil || files.mutationGuard == nil {
		return fmt.Errorf("NFS Team write fencing is not configured")
	}
	request.Root = files.nfsRoot
	if _, _, _, err := validateTeamNativeRequest(request); err != nil {
		return err
	}
	return files.withMutation(ctx, scope, func() error {
		scopes := []WorkspaceFileScope{scope}
		if scope.TeamID > 0 {
			resolver, ok := files.mutationGuard.(interface {
				WorkspaceTeamWriteScopes(context.Context, WorkspaceFileScope) ([]WorkspaceFileScope, error)
			})
			if !ok {
				return fmt.Errorf("NFS Team member fencing is unavailable")
			}
			var err error
			scopes, err = resolver.WorkspaceTeamWriteScopes(ctx, scope)
			if err != nil {
				return err
			}
		}
		var intents []models.WorkspaceWriteIntent
		for _, member := range scopes {
			var nonce [16]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return errors.Join(err, files.settleNFSIntents(ctx, intents))
			}
			intent := models.WorkspaceWriteIntent{Operation: hex.EncodeToString(nonce[:]), InstanceID: member.InstanceID, OwnerID: member.UserID, Generation: member.RuntimeGeneration, Action: "edit"}
			if err := files.nfsWrites.Begin(ctx, intent); err != nil {
				return errors.Join(err, files.settleNFSIntents(ctx, intents))
			}
			intents = append(intents, intent)
		}
		ended := false
		var err error
		if request.InstanceID > 0 {
			grant := request
			grant.Action = "grant_instance"
			grant.Identity = nil
			grant.Context = nil
			ended, err = runTeamNativeHelper(ctx, grant)
			if err != nil {
				if ended && !errors.Is(err, ErrWorkspaceNFSUncertain) {
					err = errors.Join(err, files.settleNFSIntents(ctx, intents))
				}
				return err
			}
		}
		ended, err = runTeamNativeHelper(ctx, request)
		if ended && !errors.Is(err, ErrWorkspaceNFSUncertain) {
			err = errors.Join(err, files.settleNFSIntents(ctx, intents))
		}
		return err
	})
}
func (s *teamService) nativeTeamShared(ctx context.Context, userID, teamID int, directories []string, documents map[string]string) error {
	return s.runNativeTeamMutation(ctx, WorkspaceFileScope{UserID: userID, TeamID: teamID, WorkspacePath: s.teamRuntimeSharedPathFor(userID, teamID)}, teamNativeRequest{UserID: userID, TeamID: teamID, Action: "shared", Directories: directories, Context: documents})
}
func (s *teamService) nativeTeamPrivate(ctx context.Context, instance *models.Instance, action string, identity, documents map[string]string) error {
	if instance == nil || instance.WorkspacePath == nil {
		return fmt.Errorf("NFS Team instance binding is unavailable")
	}
	expected := RuntimeWorkspacePathWithRoot(s.workspaceText.nfsRoot, instance.Type, instance.UserID, instance.ID)
	if filepath.Clean(*instance.WorkspacePath) != expected {
		return ErrWorkspacePathEscape
	}
	scope := WorkspaceFileScope{UserID: instance.UserID, InstanceID: instance.ID, RuntimeGeneration: instance.RuntimeGeneration, WorkspacePath: expected}
	return s.runNativeTeamMutation(ctx, scope, teamNativeRequest{UserID: instance.UserID, InstanceID: instance.ID, Generation: instance.RuntimeGeneration, RuntimeType: instance.Type, Action: action, Identity: identity, Context: documents})
}
func teamNativeDocuments(roster, introduction string) map[string]string {
	documents := map[string]string{}
	if strings.TrimSpace(roster) != "" {
		documents[teamConfigFileName] = roster
	}
	if strings.TrimSpace(introduction) != "" {
		documents[teamIntroductionFileName] = introduction
	}
	return documents
}
func (s *teamService) syncNativeLeaderContext(userID int, team *models.Team, members []models.TeamMember, roster, introduction string) error {
	if s == nil || team == nil {
		return nil
	}
	documents := teamNativeDocuments(roster, introduction)
	if err := s.nativeTeamShared(context.Background(), userID, team.ID, nil, documents); err != nil {
		return err
	}
	leader := findTeamLeader(members)
	if leader == nil {
		return fmt.Errorf("Team %d has no active Leader", team.ID)
	}
	instance, err := s.teamMemberInstance(leader)
	if err != nil {
		return err
	}
	if teamMemberPromptWorkspace(instance) == "" {
		return nil
	}
	if modeForExistingInstance(instance) == InstanceModeLite {
		return s.nativeTeamPrivate(context.Background(), instance, "contexts", nil, documents)
	}
	for name, text := range documents {
		if err = writeManagedTeamContextFile(filepath.Join(teamMemberPromptWorkspace(instance), name), []byte(text), 0644); err != nil {
			return err
		}
	}
	return nil
}

// Bootstrap reports use the same confined, audited writer as Team file uploads.
// Existing reports use the text version check, so retries cannot silently replace
// a concurrent edit. Each mutator retains the configured NFS SQL write intent.
func (s *teamService) writeNativeBootstrapReport(ctx context.Context, userID, teamID int, relative, text string) error {
	scope := WorkspaceFileScope{UserID: userID, TeamID: teamID, WorkspacePath: s.teamRuntimeSharedPathFor(userID, teamID)}
	directory := path.Dir(relative)
	if _, err := s.workspaceText.Mkdir(ctx, scope, directory); err != nil && !errors.Is(err, ErrWorkspaceEntryExists) {
		return err
	}
	document, err := s.workspaceText.ReadText(ctx, scope, relative)
	if err == nil {
		_, err = s.workspaceText.SaveText(ctx, scope, relative, text, document.Version)
		return err
	}
	if !errors.Is(err, ErrWorkspacePathNotFound) && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = s.workspaceText.Upload(ctx, scope, directory, path.Base(relative), strings.NewReader(text), int64(len(text)))
	return err
}
