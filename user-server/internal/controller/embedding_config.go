package controller

import (
	"net/http"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// embedding_config.go 全局 Embedding 提供商手动配置（管理端）
//
// 小内存服务器跑不动本地 bge-m3：把全局 embedding 指向云端 OpenAI 兼容端点
// （硅基流动 bge-m3 / 阿里 DashScope text-embedding-v3 等，均原生 1024 维），
// 在线改、免重启、免登服务器。维度硬约束 1024（pgvector 兼容），配置时选
// 非 1024 维模型会在调用侧被强制回落并告警。

// GetEmbeddingConfig GET /api/llm/embedding-config（api_key 脱敏返回）
func GetEmbeddingConfig(ctx *gin.Context) {
	o := llm.GetGlobalEmbeddingOverride()
	if o == nil {
		response.Success(ctx, gin.H{"enabled": false, "base_url": "", "api_key_set": false, "model": ""}, "ok")
		return
	}
	response.Success(ctx, gin.H{
		"enabled":      o.Enabled,
		"base_url":     o.BaseURL,
		"model":        o.Model,
		"api_key_set":  o.APIKey != "",
	}, "ok")
}

// UpdateEmbeddingConfig PUT /api/llm/embedding-config
func UpdateEmbeddingConfig(ctx *gin.Context) {
	var req struct {
		Enabled bool   `json:"enabled"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.Enabled && (req.BaseURL == "" || req.Model == "") {
		response.Error(ctx, http.StatusBadRequest, "启用时 base_url 与 model 必填（模型需输出 1024 维向量）")
		return
	}
	cur := llm.GetGlobalEmbeddingOverride()
	if req.APIKey == "" && cur != nil {
		req.APIKey = cur.APIKey // 未传 key 视为沿用已存 key
	}
	if err := llm.SetGlobalEmbeddingOverride(&llm.GlobalEmbeddingOverride{
		Enabled: req.Enabled,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
	}); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已保存（60s 内全集群生效，无需重启）")
}
