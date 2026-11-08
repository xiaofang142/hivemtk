package controller

import (
	"bytes"
	"encoding/json"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SystemConfigController 系统配置控制器
type SystemConfigController struct {
	svc *service.SystemConfigService
}

// NewSystemConfigController 创建系统配置控制器实例
func NewSystemConfigController() *SystemConfigController {
	return &SystemConfigController{svc: service.NewSystemConfigService()}
}

// @Summary      获取系统配置
// @Tags      system-config
// @Router    /api/system/config [get]
// GetConfig 获取系统配置
func (c *SystemConfigController) GetConfig(ctx *gin.Context) {
	config, err := c.svc.GetConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, config, "success")
}

// @Summary      保存系统配置
// @Tags      system-config
// @Router    /api/system/config [post]
// SaveConfig 保存系统配置
//
// #968：repo 层是每租户一行的**整行回写**（SaveWholeRow），而 bool/int 字段
// 无法用零值区分"没传"和"传了 false/0"——只传 {site_name} 的部分保存会把
// enable_register/enable_rag 等静默清成 false（线上实证过并现场恢复）。
// 守卫：按原始 JSON 的**键存在性**补齐——缺键的 bool/int 字段从既有行取值，
// 显式传 false/0 仍然生效（PATCH 语义，而不是一刀切拒掉部分保存）。
func (c *SystemConfigController) SaveConfig(ctx *gin.Context) {
	raw, _ := ctx.GetRawData()
	// GetRawData 消费过请求流：重新灌回 body 供 ShouldBindJSON 读取。
	ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var req *model.SystemConfig
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if req == nil || isEmptySystemConfigRequest(req) {
		response.Error(ctx, http.StatusBadRequest, "请求体不能为空：至少提供一个配置字段（如 site_name）")
		return
	}
	// 键存在性探测：bool/int 零值无法自证"没传"，只有原始键能。
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	req = c.svc.FillUnsetFromExisting(ctx.Request.Context(), req, keys)
	config, err := c.svc.SaveConfig(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, config, "success")
}

// isEmptySystemConfigRequest 判断请求是否未携带任何业务字段。
// 防御空 body {} 把站点配置（site_name 等）整体清空。
func isEmptySystemConfigRequest(cfg *model.SystemConfig) bool {
	return cfg.Name == "" && cfg.WebsiteURL == "" && cfg.LogoURL == "" &&
		cfg.ThemeColor == "" && cfg.SEOKeywords == "" && cfg.SEODescription == "" &&
		cfg.ServicePhone == "" && cfg.ServiceEmail == "" &&
		cfg.ICPRecord == "" && cfg.PoliceRecord == "" &&
		!cfg.EnableRegister && !cfg.EnableEmailMarketing && !cfg.EnableRAG &&
		!cfg.MaintenanceMode && cfg.MaxUsers == 0 && cfg.MaxUploadSizeMB == 0
}
