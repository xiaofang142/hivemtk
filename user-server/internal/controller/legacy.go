package controller

// 墓碑路由控制器（2026-11-05 二次审计：从 router 迁入）。

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
)

// KnowledgeMigrationTarget 是前端已迁移到的权威路径（规划 §2.2 契约第 1 条：
// 不再有 /api/knowledge 顶层域名）。message 与 data.migrate_to 共用这一份字面量。
const KnowledgeMigrationTarget = "/api/knowledge-bases/:id/items"

const LegacyKnowledgeGoneMessage = "该接口已废弃，请改用 " + KnowledgeMigrationTarget +
	"（/api/knowledge 顶层域名已下线，不再提供服务）"

// LegacyController 已下线接口的墓碑响应。
type LegacyController struct{}

func NewLegacyController() *LegacyController { return &LegacyController{} }

// KnowledgeGone 已下线的 /api/knowledge 顶层域名（410 Gone）。
//
// 传强类型码而不是 http.StatusGone：int 路径的反向映射没有 410，会落
// UNKNOWN_1000（"未知错误"），N-5 注册 LEGACY_GONE_4100 后两者等价，
// 但强类型码不依赖反向映射表维持正确。
func (l *LegacyController) KnowledgeGone(c *gin.Context) {
	response.Error(c, utils.ErrorCodeLegacyGone, LegacyKnowledgeGoneMessage, gin.H{
		"migrate_to": KnowledgeMigrationTarget,
	})
}
