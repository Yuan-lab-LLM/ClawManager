package services

import (
	"clawreef/internal/repository"
	"context"
	"strings"
	"unicode"
)

// Optional interface keeps third-party TeamService implementations backward compatible.
type TeamWorkspaceTextService interface {
	ReadWorkspaceText(context.Context, int, int, string) (*WorkspaceText, error)
	SaveWorkspaceText(context.Context, int, int, string, string, string) (*WorkspaceText, error)
}

func WithTeamWorkspaceAuditRepository(repo repository.TeamWorkspaceFileAuditRepository) TeamServiceOption {
	return func(s *teamService) { s.workspaceText = &workspaceFileService{teamAuditRepo: repo} }
}
func (s *teamService) teamTextScope(ctx context.Context, userID, teamID int, relative string) (WorkspaceFileScope, string, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceFileScope{}, "", err
	}
	if _, err := s.requireOwnedTeam(userID, teamID); err != nil {
		return WorkspaceFileScope{}, "", err
	}
	if strings.IndexFunc(relative, unicode.IsControl) >= 0 {
		return WorkspaceFileScope{}, "", ErrWorkspacePathInvalid
	}
	raw := strings.TrimSpace(relative)
	// Accept only the documented /team/ alias; never clean away traversal first.
	raw = strings.TrimPrefix(raw, "/team/")
	rel, err := cleanWorkspaceRelativePath(raw)
	return WorkspaceFileScope{TeamID: teamID, UserID: userID, WorkspacePath: s.teamRuntimeSharedPathFor(userID, teamID)}, rel, err
}
func (s *teamService) ReadWorkspaceText(ctx context.Context, userID, teamID int, relative string) (*WorkspaceText, error) {
	scope, rel, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return nil, err
	}
	return s.workspaceText.ReadText(ctx, scope, rel)
}
func (s *teamService) SaveWorkspaceText(ctx context.Context, userID, teamID int, relative, text, version string) (*WorkspaceText, error) {
	scope, rel, err := s.teamTextScope(ctx, userID, teamID, relative)
	if err != nil {
		return nil, err
	}
	return s.workspaceText.SaveText(ctx, scope, rel, text, version)
}
