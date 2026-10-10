package controller

import (
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// CapabilityMatrix GET /api/capabilities 渠道能力矩阵（注册表渲染）
func CapabilityMatrix(ctx *gin.Context) {
	entries := capregistry.All()
	out := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		out = append(out, gin.H{"platform": e.Platform, "capability": string(e.Cap), "via": e.Via, "note": e.Note})
	}
	response.Success(ctx, gin.H{"entries": out, "markdown": capregistry.MatrixMarkdown()}, "ok")
}
