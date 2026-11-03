package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"time"
)

func (h *TeamHandler) WorkspaceText(c *gin.Context) {
	userID, ok := c.Get("userID")
	id, valid := userID.(int)
	if !ok || !valid || id <= 0 {
		utils.Error(c, 401, "Authentication required")
		return
	}
	teamID, ok := parseTeamID(c)
	if !ok {
		return
	}
	editor, ok := h.teamService.(services.TeamWorkspaceTextService)
	if !ok {
		utils.Error(c, 503, "Team text editing is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	c.Header("Cache-Control", "no-store")
	var doc *services.WorkspaceText
	var err error
	if c.Request.Method == http.MethodGet {
		doc, err = editor.ReadWorkspaceText(ctx, id, teamID, c.Query("path"))
	} else {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
		var req struct {
			Path    string  `json:"path" binding:"required"`
			Text    *string `json:"text" binding:"required"`
			Version string  `json:"version" binding:"required"`
		}
		if e := c.ShouldBindJSON(&req); e != nil {
			utils.Error(c, 400, "Invalid text save request (maximum 1 MiB UTF-8 text)")
			return
		}
		doc, err = editor.SaveWorkspaceText(ctx, id, teamID, req.Path, *req.Text, req.Version)
	}
	switch {
	case errors.Is(err, services.ErrWorkspaceTextConflict):
		utils.Error(c, 409, "文件已被修改，请重新载入后再保存；当前编辑内容已保留。")
	case errors.Is(err, services.ErrWorkspaceTextUnsupported):
		utils.Error(c, 422, "仅支持普通 UTF-8 文本文件，不支持二进制、链接或特殊文件。")
	case os.IsNotExist(err):
		utils.Error(c, 404, "Team workspace file not found")
	case os.IsPermission(err):
		utils.Error(c, 403, "Access denied")
	case err != nil:
		handleWorkspaceFileError(c, err)
	default:
		utils.Success(c, 200, "Team workspace text retrieved or saved", doc)
	}
}
