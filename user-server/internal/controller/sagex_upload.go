package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type SageXUploadController struct {
	svc *service.SageXUploadService
}

func NewSageXUploadController(svc *service.SageXUploadService) *SageXUploadController {
	return &SageXUploadController{svc: svc}
}

func (c *SageXUploadController) Upload(ctx *gin.Context) {
	const maxBody = int64(service.SageXMaxRequestBytes + 4096)
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxBody)

	raw, err := ctx.GetRawData()
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || len(raw) > service.SageXMaxRequestBytes {
			sagexWriteError(ctx, http.StatusRequestEntityTooLarge, "request_too_large")
			return
		}
		sagexWriteError(ctx, http.StatusBadRequest, "invalid_body")
		return
	}
	if len(raw) > service.SageXMaxRequestBytes {
		sagexWriteError(ctx, http.StatusRequestEntityTooLarge, "request_too_large")
		return
	}

	var req service.SageXUploadRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		sagexWriteError(ctx, http.StatusBadRequest, "invalid_json")
		return
	}
	identifyCode := strings.TrimSpace(req.IdentifyCode)
	if identifyCode == "" {
		identifyCode = strings.TrimSpace(ctx.GetHeader("X-Channel-Identify-Code"))
	}
	if identifyCode == "" {
		sagexWriteError(ctx, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := c.svc.HandleUpload(ctx.Request.Context(), identifyCode, &req, len(raw))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSageXUnauthorized):
			sagexWriteError(ctx, http.StatusUnauthorized, "unauthorized")
		case errors.Is(err, service.ErrSageXInvalidInstallationID):
			sagexWriteError(ctx, http.StatusBadRequest, "invalid_installation_id")
		case errors.Is(err, service.ErrSageXInstallationForbidden):
			sagexWriteError(ctx, http.StatusForbidden, "installation_forbidden")
		case errors.Is(err, service.ErrSageXInstallationDisabled):
			sagexWriteError(ctx, http.StatusForbidden, "installation_disabled")
		case errors.Is(err, service.ErrSageXInstallationConflict):
			sagexWriteError(ctx, http.StatusConflict, "installation_conflict")
		case errors.Is(err, service.ErrSageXUnsupportedSchema):
			sagexWriteError(ctx, http.StatusBadRequest, "unsupported_schema")
		case errors.Is(err, service.ErrSageXTooManyEvents), errors.Is(err, service.ErrSageXEmptyEvents), errors.Is(err, service.ErrSageXInvalidEnvelope):
			sagexWriteError(ctx, http.StatusBadRequest, "invalid_envelope")
		default:
			sagexWriteError(ctx, http.StatusInternalServerError, "internal_error")
		}
		return
	}

	body, err := json.Marshal(resp)
	if err != nil {
		sagexWriteError(ctx, http.StatusInternalServerError, "internal_error")
		return
	}
	ctx.Header("X-Compat-Route", "sagex-upload; prefer /api/bridge/gateway")
	response.Protocol(ctx, http.StatusOK, "application/json; charset=utf-8", body)
}

func (c *SageXUploadController) RegisterAdminRoutes(auth *gin.RouterGroup) {
	g := auth.Group("/sagex/installations")
	g.POST("", c.CreateInstallation)
	g.GET("", c.ListInstallations)
	g.GET("/:id", c.GetInstallation)
	g.PUT("/:id", c.UpdateInstallation)
}

type sagexInstallationRequest struct {
	Name           string `json:"name"`
	InstallationID string `json:"installation_id"`
	AccountID      string `json:"account_id"`
	Token          string `json:"token"`
	Status         *int   `json:"status"`
}

func (c *SageXUploadController) CreateInstallation(ctx *gin.Context) {
	var req sagexInstallationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数无效")
		return
	}
	inst := &model.SageXInstallation{
		Name:           strings.TrimSpace(req.Name),
		InstallationID: strings.TrimSpace(req.InstallationID),
		AccountID:      strings.TrimSpace(req.AccountID),
		Token:          strings.TrimSpace(req.Token),
		Status:         1,
	}
	if req.Status != nil {
		inst.Status = *req.Status
	}
	if err := c.svc.CreateInstallation(ctx.Request.Context(), inst); err != nil {
		response.ErrorFromDB(ctx, err, "创建 SageX 安装绑定失败")
		return
	}
	// Token 的 model JSON tag 是 "-"，不会随 inst 序列化；创建时单独返回一次，
	// 否则调用方留空时服务端生成的 token 将永久不可读。
	response.Success(ctx, gin.H{"installation": inst, "token": inst.Token}, "ok")
}

func (c *SageXUploadController) ListInstallations(ctx *gin.Context) {
	// minimal: return empty list for now if repo doesn't have List; implement simple query via DB in service later
	// For now, provide Get by installation_id query
	installationID := strings.TrimSpace(ctx.Query("installation_id"))
	if installationID != "" {
		inst, err := c.svc.GetInstallationByInstallationID(ctx.Request.Context(), installationID)
		if err != nil {
			response.ErrorFromDB(ctx, err, "查询 SageX 安装绑定失败")
			return
		}
		if inst == nil {
			response.Success(ctx, []any{}, "ok")
			return
		}
		response.Success(ctx, []*model.SageXInstallation{inst}, "ok")
		return
	}
	response.Success(ctx, []any{}, "ok")
}

func (c *SageXUploadController) GetInstallation(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "ID 无效")
		return
	}
	inst, err := c.svc.GetInstallationByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询 SageX 安装绑定失败")
		return
	}
	if inst == nil {
		response.NotFound(ctx, "SageX 安装绑定不存在")
		return
	}
	response.Success(ctx, inst, "ok")
}

func (c *SageXUploadController) UpdateInstallation(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "ID 无效")
		return
	}
	existing, err := c.svc.GetInstallationByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询 SageX 安装绑定失败")
		return
	}
	if existing == nil {
		response.NotFound(ctx, "SageX 安装绑定不存在")
		return
	}
	var req sagexInstallationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数无效")
		return
	}
	if req.Name != "" {
		existing.Name = strings.TrimSpace(req.Name)
	}
	if req.InstallationID != "" {
		existing.InstallationID = strings.TrimSpace(req.InstallationID)
	}
	if req.AccountID != "" {
		existing.AccountID = strings.TrimSpace(req.AccountID)
	}
	if req.Token != "" {
		existing.Token = strings.TrimSpace(req.Token)
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	if err := c.svc.UpdateInstallation(ctx.Request.Context(), existing); err != nil {
		response.ErrorFromDB(ctx, err, "更新 SageX 安装绑定失败")
		return
	}
	response.Success(ctx, existing, "ok")
}

func (c *SageXUploadController) Bootstrap(ctx *gin.Context) {
	var req struct {
		InstallationID string `json:"installation_id"`
		IdentifyCode   string `json:"identify_code"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		sagexBootstrapError(ctx, http.StatusBadRequest, "invalid_request", "请求参数无效")
		return
	}
	if strings.TrimSpace(req.IdentifyCode) == "" {
		sagexBootstrapError(ctx, http.StatusBadRequest, "invalid_identify_code", "identify_code 不能为空")
		return
	}
	inst, err := c.svc.BootstrapInstallation(ctx.Request.Context(), req.InstallationID, req.IdentifyCode)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSageXInvalidInstallationID):
			sagexBootstrapError(ctx, http.StatusBadRequest, "invalid_installation_id", "installation_id 无效")
		case errors.Is(err, service.ErrSageXUnauthorized):
			sagexBootstrapError(ctx, http.StatusUnauthorized, "unauthorized", "bootstrap 未授权")
		case errors.Is(err, service.ErrSageXInstallationForbidden):
			sagexBootstrapError(ctx, http.StatusForbidden, "installation_forbidden", "installation_id 不属于当前账号")
		case errors.Is(err, service.ErrSageXInstallationDisabled):
			sagexBootstrapError(ctx, http.StatusForbidden, "installation_disabled", "installation_id 已禁用")
		case errors.Is(err, service.ErrSageXInstallationConflict):
			sagexBootstrapError(ctx, http.StatusConflict, "installation_conflict", "installation_id 状态冲突")
		default:
			sagexBootstrapError(ctx, http.StatusInternalServerError, "internal_error", "bootstrap 服务暂不可用")
		}
		return
	}
	response.Success(ctx, gin.H{"installation": inst, "token": inst.Token}, "ok")
}

func sagexBootstrapError(ctx *gin.Context, status int, reason, message string) {
	response.Error(ctx, status, message, gin.H{"reason": reason})
}

func sagexWriteError(ctx *gin.Context, status int, code string) {
	body, _ := json.Marshal(map[string]any{
		"schema_version": service.SageXSchemaVersion,
		"error":          code,
	})
	response.Protocol(ctx, status, "application/json; charset=utf-8", body)
}
