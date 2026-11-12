package controller

import (
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// llm_config.go 全局 LLM 提供商手动配置（管理端）
//
// SaaS 兼容要求：LLM API 连接信息禁止走环境变量（多租户各自配置、平台端在线改）。
// 全局默认指向存参数中心 llm.global_override，整份 JSON，api_key 加密落库。

// GetLLMConfig GET /api/llm/llm-config（api_key 脱敏返回）
func GetLLMConfig(ctx *gin.Context) {
	o := llm.GetGlobalLLMOverride()
	if o == nil {
		response.Success(ctx, gin.H{"enabled": false, "base_url": "", "api_key_set": false, "model": "", "api_type": ""}, "ok")
		return
	}
	response.Success(ctx, gin.H{
		"enabled":     o.Enabled,
		"base_url":    o.BaseURL,
		"model":       o.Model,
		"api_type":    o.APIType,
		"api_key_set": o.APIKey != "",
	}, "ok")
}

// UpdateLLMConfig PUT /api/llm/llm-config
//
// @Summary      保存全局 LLM 提供商手动配置
// @Description  整份 JSON 落参数中心 llm.global_override，api_key 加密后入库；保存即让本进程的读取缓存失效
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body   body    object  true  "enabled/base_url/api_key/model/api_type（启用时 base_url 与 model 必填；api_type 空或 openai/azure/anthropic）"
// @Success      200    {object}  response.Response
// @Failure      400    {object}  response.Response  "body 形状不对，或启用时缺 base_url/model"
// @Failure      500    {object}  response.Response  "保存失败（含参数中心写口未注入）"
// @Router       /api/llm/llm-config [put]
func UpdateLLMConfig(ctx *gin.Context) {
	var req struct {
		Enabled bool   `json:"enabled"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
		APIType string `json:"api_type"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	req.APIType = strings.ToLower(strings.TrimSpace(req.APIType))
	switch req.APIType {
	case "", "openai", "azure", "anthropic":
	default:
		response.Error(ctx, http.StatusBadRequest, "api_type 仅支持 openai / azure / anthropic")
		return
	}
	if req.Enabled && (req.BaseURL == "" || req.Model == "") {
		response.Error(ctx, http.StatusBadRequest, "启用时 base_url 与 model 必填")
		return
	}
	cur := llm.GetGlobalLLMOverride()
	if req.APIKey == "" && cur != nil {
		req.APIKey = cur.APIKey // 未传 key 视为沿用已存 key
	}
	// 操作者来自会话：参数中心的变更审计按 user_id 记，谁把 LLM 指到外部端点要查得到。
	// 取不到会话不拦这次写入（装配期自检也要能写），但那条审计会记成系统来路。
	var actorID uint
	if s, ok := contextOperatorID(ctx); ok {
		if n, err := strconv.Atoi(s); err == nil {
			actorID = uint(n)
		}
	}
	if err := llm.SetGlobalLLMOverride(&llm.GlobalLLMOverride{
		Enabled: req.Enabled,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
		APIType: req.APIType,
	}, actorID); err != nil {
		response.ErrorFromDB(ctx, err, "保存失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已保存（本进程立即生效，其余实例最多等一个缓存周期）")
}
