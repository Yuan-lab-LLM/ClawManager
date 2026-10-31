package northbound

import (
	"errors"
	"mime"
	"net/http"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func (a *OwnerTeamAPI) handleFiles(c *gin.Context, team *models.Team, action string) {
	ctx, uid, id, path := c.Request.Context(), team.UserID, team.ID, c.Query("path")
	var value interface{}
	var err error
	switch action {
	case "files.list":
		value, err = a.teams.ListWorkspaceFiles(ctx, uid, id, path)
	case "files.preview":
		value, err = a.teams.PreviewWorkspaceFile(ctx, uid, id, path)
	case "files.download":
		var file *services.TeamWorkspaceDownloadPayload
		file, err = a.teams.DownloadWorkspaceFile(ctx, uid, id, path)
		if err == nil {
			c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
			c.Header("X-Content-Type-Options", "nosniff")
			c.Data(200, file.ContentType, file.Data)
			return
		}
	case "files.read", "files.save":
		editor, ok := a.teams.(services.TeamWorkspaceTextService)
		if !ok {
			err = apiError(503, "FILE_EDITOR_UNAVAILABLE", "文件编辑暂不可用", nil)
			break
		}
		if action == "files.read" {
			value, err = editor.ReadWorkspaceText(ctx, uid, id, path)
		} else {
			var req struct {
				Path    string  `json:"path"`
				Text    *string `json:"text"`
				Version string  `json:"version"`
			}
			if !ownerTeamBind(c, &req) {
				return
			}
			if req.Text == nil || req.Version == "" {
				err = apiError(422, "VALIDATION_ERROR", "缺少文本或文件版本", nil)
				break
			}
			value, err = editor.SaveWorkspaceText(ctx, uid, id, req.Path, *req.Text, req.Version)
		}
	case "files.mkdir":
		var req services.TeamWorkspaceFolderRequest
		if !ownerTeamBind(c, &req) {
			return
		}
		err = a.teams.CreateWorkspaceFolder(ctx, uid, id, req)
	case "files.rename":
		var req services.TeamWorkspaceRenameRequest
		if !ownerTeamBind(c, &req) {
			return
		}
		err = a.teams.RenameWorkspaceEntry(ctx, uid, id, req)
	case "files.delete":
		err = a.teams.DeleteWorkspaceEntry(ctx, uid, id, path)
	case "files.upload":
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<20)
		if err = c.Request.ParseMultipartForm(8 << 20); err != nil {
			writeError(c, apiError(413, "UPLOAD_TOO_LARGE", "上传内容无效或超过限制", nil))
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		files := c.Request.MultipartForm.File["files"]
		if len(files) == 0 || len(files) > 100 {
			writeError(c, apiError(422, "VALIDATION_ERROR", "请选择 1～100 个文件", nil))
			return
		}
		err = a.teams.UploadWorkspaceFiles(ctx, uid, id, c.PostForm("path"), files, c.Request.MultipartForm.Value["relative_paths"])
	}
	if errors.Is(err, services.ErrWorkspaceTextConflict) {
		writeError(c, apiError(409, "REVISION_CONFLICT", "文件已变化，请重新载入后保存；编辑内容已保留", nil))
		return
	}
	if err != nil {
		ownerTeamError(c, err)
		return
	}
	ownerTeamSuccess(c, value)
}
