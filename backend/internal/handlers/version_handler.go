package handlers

import (
	"net/http"

	"clawreef/internal/buildinfo"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type VersionHandler struct{}

func NewVersionHandler() *VersionHandler {
	return &VersionHandler{}
}

func (h *VersionHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	utils.Success(c, http.StatusOK, "ClawManager build information retrieved successfully", buildinfo.Current())
}
