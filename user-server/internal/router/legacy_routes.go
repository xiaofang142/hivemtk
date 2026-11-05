package router

// 已下线的 /api/knowledge 顶层域名（D-10 选 B）：老客户端此前拿到的是 NoRoute 的 404，
// 分不清"接口不存在"和"地址写错了"。这里补 410 Gone，让它们拿到明确的迁移指引。
//
// 为什么是逐条注册而不是 GET /knowledge/*path 的 catch-all：gin 1.12 的 radix tree
// 不允许 catch-all 与同前缀的静态子节点共存，而 auth 档里仍有两条 GET 别名
// （/api/knowledge/playground/presets、/api/knowledge/help-center/top，见
// frontend_aliases.go）。注册 catch-all 会让 Setup panic，或反过来让那两条别名注册
// 失败（TestFrontendAliases_NoFailedRegistration 会红），代价大于收益。因此只覆盖
// 老客户端实际会打的两个入口：域名根与历史搜索口。
//
// 410 是终点态：不读库、不读租户数据、不写任何状态，所以挂在 public 组匿名可达；
// 匿名面登记与判定见 route_policy_gate_test.go 的 anonymousAllowedRoutes。
// 2026-11-05 二次审计：处理器已迁入 controller.LegacyController。

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
)

const (
	legacyKnowledgeRootPath   = "/knowledge"
	legacyKnowledgeSearchPath = "/knowledge/search"
)

func setupLegacyRoutes(public *gin.RouterGroup) {
	lc := controller.NewLegacyController()
	public.GET(legacyKnowledgeRootPath, lc.KnowledgeGone)
	public.GET(legacyKnowledgeSearchPath, lc.KnowledgeGone)
}
