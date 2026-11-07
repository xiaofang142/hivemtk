package controller

import (
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// respondGeoLLMError 统一 GEO 侧「强依赖 LLM」端点的失败口径。
//
// 这些端点此前把 service 返回的 err 直接丢掉、固定回 500「语义扩展失败」：
// 运维既分不清是"没配模型"还是"模型真挂了"，日志里也追不到原因（err 从未打印）。
// 与已收口的 D-20（飞书 test-send 把上游错误回成 500）同属一类问题，故对齐：
//   - 无可用 provider（未配置 / 全部熔断）→ 503 SERVICE_UNAVAILABLE_6003，
//     与 followup「服务未装配」保持同一语义：能力没就绪，不是请求写错了。
//   - 其余 provider 调用失败 → 502 UPSTREAM_FAILURE_5005，客户端/运维知道该看上流。
func respondGeoLLMError(c *gin.Context, err error, action string) {
	if isNoLLMProviderErr(err) {
		logger.Warnf("[GEO] %s失败：无可用 LLM provider: %v", action, err)
		response.ErrorWithCode(c, utils.ErrorCodeServiceUnavailable, http.StatusServiceUnavailable,
			action+"失败：未配置可用的大模型，请先在模型供应商中启用")
		return
	}
	logger.Errorf("[GEO] %s失败: %v", action, err)
	response.ErrorWithCode(c, utils.ErrorCodeUpstreamFailure, http.StatusBadGateway,
		action+"失败：模型调用失败")
}

// isNoLLMProviderErr 识别「一个可用 provider 都没有」的失败。文案来源：
//   - geo/service/llm.go：HasHealthyProvider=false → "LLM provider 已全部熔断，跳过重试（触发降级）"
//   - aiagent/llm/provider_failover.go：调度器兜底落空 → "no available provider for scenario: %s"
//   - dispatcher 未初始化（集成态缺装配）→ "LLM dispatcher 未初始化"
func isNoLLMProviderErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no available provider") ||
		strings.Contains(msg, "已全部熔断") ||
		strings.Contains(msg, "dispatcher 未初始化")
}

// respondGeoConfigError GEO「前置配置缺失」类失败的统一口径。
// 这类错误（ErrNoPushPlatforms 等）不是程序 bug 而是用户还没配凭据，回 500 会让
// 监控误报 INTERNAL_ERROR 且客户端无从补救；对齐 followup「服务未装配」的语义，
// 统一回 503 SERVICE_UNAVAILABLE_6003，文案保留原文（本身就是配置引导）。
func respondGeoConfigError(c *gin.Context, err error, action string) {
	logger.Warnf("[GEO] %s失败（前置配置缺失）: %v", action, err)
	response.ErrorWithCode(c, utils.ErrorCodeServiceUnavailable, http.StatusServiceUnavailable, err.Error())
}
