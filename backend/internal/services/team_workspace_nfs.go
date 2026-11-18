package services

import "clawreef/internal/repository"

// ConfigureTeamWorkspaceNFSIO runs after all Team options have selected the
// concrete text service and before requests or Team reconciliation begin.
func ConfigureTeamWorkspaceNFSIO(team TeamService, profile, root string, writes repository.WorkspaceWriteRepository, guard WorkspaceUpgradeMutationGuard) {
	if s, ok := team.(*teamService); ok {
		ConfigureWorkspaceNFSIO(s.workspaceText, profile, root, writes)
		ConfigureWorkspaceUpgradeGuard(s.workspaceText, guard)
	}
}
