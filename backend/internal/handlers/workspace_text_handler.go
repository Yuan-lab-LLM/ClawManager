package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (h *WorkspaceFileHandler) Text(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Header("Cache-Control", "no-store")
	_, service, scope, ok := h.workspaceScope(c)
	if !ok {
		return
	}
	editor, ok := service.(services.WorkspaceTextService)
	if !ok {
		utils.Error(c, 503, "Text editing is unavailable")
		return
	}
	var doc *services.WorkspaceText
	var err error
	if c.Request.Method == http.MethodGet {
		doc, err = editor.ReadText(c.Request.Context(), scope, c.Query("path"))
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
		doc, err = editor.SaveText(c.Request.Context(), scope, req.Path, *req.Text, req.Version)
	}
	if errors.Is(err, services.ErrWorkspaceTextConflict) {
		utils.Error(c, 409, "文件已被修改，请重新载入后再保存；当前编辑内容已保留。")
		return
	}
	if errors.Is(err, services.ErrWorkspaceTextUnsupported) {
		utils.Error(c, 422, "仅支持普通 UTF-8 文本文件，不支持二进制、链接或特殊文件。")
		return
	}
	if err != nil {
		handleWorkspaceFileError(c, err)
		return
	}
	utils.Success(c, 200, "Workspace text retrieved or saved", doc)
}

func (h *IEISystemHandler) WorkspaceText(c *gin.Context) {
	if c.Request.Method == http.MethodPut && !ownerWriteRequest(c) {
		return
	}
	h.handleWorkspace(c, (*WorkspaceFileHandler).Text)
}
