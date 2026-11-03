package northbound

import "github.com/gin-gonic/gin"

const (
	ScopeTeamsRead             = "teams:read"
	ScopeTeamsCreate           = "teams:create"
	ScopeTeamsManage           = "teams:manage"
	ScopeTeamsDelete           = "teams:delete"
	ScopeTeamTasksRead         = "teams:tasks:read"
	ScopeTeamTasksWrite        = "teams:tasks:write"
	ScopeTeamFilesRead         = "teams:files:read"
	ScopeTeamFilesWrite        = "teams:files:write"
	ScopeTeamTemplatesRead     = "team-templates:read"
	ScopeTeamTemplatesWrite    = "team-templates:write"
	ScopeTeamTemplatesGenerate = "team-templates:generate"
)

type OwnerTeamRoute struct{ Method, Path, Scope, Action string }

// The same explicit allowlist is registered at the edge, Core and owner portal.
// It cannot forward arbitrary CM routes.
var OwnerTeamRoutes = []OwnerTeamRoute{
	{"GET", "/team-capabilities", ScopeTeamsRead, "capabilities"},
	{"GET", "/teams", ScopeTeamsRead, "teams.list"},
	{"POST", "/teams", ScopeTeamsCreate, "teams.create"},
	{"GET", "/teams/:id", ScopeTeamsRead, "teams.get"},
	{"PATCH", "/teams/:id", ScopeTeamsManage, "teams.update"},
	{"DELETE", "/teams/:id", ScopeTeamsDelete, "teams.delete"},
	{"GET", "/teams/:id/members", ScopeTeamsRead, "members.list"},
	{"DELETE", "/teams/:id/members/:memberID", ScopeTeamsManage, "members.delete"},
	{"GET", "/teams/:id/tasks", ScopeTeamTasksRead, "tasks.list"},
	{"POST", "/teams/:id/tasks", ScopeTeamTasksWrite, "tasks.dispatch"},
	{"GET", "/teams/:id/events", ScopeTeamTasksRead, "events.list"},
	{"GET", "/teams/:id/workspace/files", ScopeTeamFilesRead, "files.list"},
	{"GET", "/teams/:id/workspace/preview", ScopeTeamFilesRead, "files.preview"},
	{"GET", "/teams/:id/workspace/download", ScopeTeamFilesRead, "files.download"},
	{"GET", "/teams/:id/workspace/text", ScopeTeamFilesRead, "files.read"},
	{"PUT", "/teams/:id/workspace/text", ScopeTeamFilesWrite, "files.save"},
	{"POST", "/teams/:id/workspace/upload", ScopeTeamFilesWrite, "files.upload"},
	{"POST", "/teams/:id/workspace/folders", ScopeTeamFilesWrite, "files.mkdir"},
	{"PATCH", "/teams/:id/workspace/entries", ScopeTeamFilesWrite, "files.rename"},
	{"DELETE", "/teams/:id/workspace/entries", ScopeTeamFilesWrite, "files.delete"},
	{"GET", "/team-templates", ScopeTeamTemplatesRead, "catalog.list"},
	{"GET", "/team-templates/:key", ScopeTeamTemplatesRead, "catalog.get"},
	{"GET", "/custom-team-templates", ScopeTeamTemplatesRead, "templates.list"},
	{"POST", "/custom-team-templates", ScopeTeamTemplatesGenerate, "templates.generate"},
	{"GET", "/custom-team-templates/:id", ScopeTeamTemplatesRead, "templates.get"},
	{"PATCH", "/custom-team-templates/:id", ScopeTeamTemplatesWrite, "templates.update"},
	{"DELETE", "/custom-team-templates/:id", ScopeTeamTemplatesWrite, "templates.delete"},
	{"POST", "/custom-team-templates/:id/revise", ScopeTeamTemplatesGenerate, "templates.revise"},
	{"POST", "/custom-team-templates/:id/regenerate", ScopeTeamTemplatesGenerate, "templates.regenerate"},
	{"POST", "/custom-team-templates/:id/members/:memberID/adjust", ScopeTeamTemplatesGenerate, "templates.adjust"},
	{"POST", "/custom-team-templates/:id/members/:memberID/regenerate", ScopeTeamTemplatesGenerate, "templates.regenerate-member"},
	{"PUT", "/custom-team-templates/:id/members/:memberID/icon", ScopeTeamTemplatesWrite, "templates.icon"},
	{"GET", "/team-operations/:operationID", "", "operations.get"},
	{"POST", "/team-operations/:operationID/retry", "", "operations.retry"},
}

type OwnerTeamIdentity struct {
	UserID                              int // zero is allowed only for verified IEI owner reads
	Owner, SessionID, Source, RequestID string
	Principal                           *Principal
}

func RegisterOwnerTeamRoutes(group *gin.RouterGroup, api func() *OwnerTeamAPI, identity func(*gin.Context) (*OwnerTeamIdentity, bool)) {
	for _, route := range OwnerTeamRoutes {
		route := route
		group.Handle(route.Method, route.Path, func(c *gin.Context) {
			id, ok := identity(c)
			if !ok {
				return
			}
			service := api()
			if service == nil {
				writeError(c, apiError(503, "TEAM_UNAVAILABLE", "Team 服务尚未配置", nil))
				return
			}
			if id.Principal != nil && route.Scope != "" && !id.Principal.HasScope(route.Scope) {
				writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
				return
			}
			service.Handle(c, *id, route)
		})
	}
}
