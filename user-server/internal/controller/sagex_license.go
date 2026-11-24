package controller

import (
	"net/http"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type SageXLicenseController struct {
	svc *service.SageXLicenseService
}

func NewSageXLicenseController() *SageXLicenseController {
	return &SageXLicenseController{svc: service.NewSageXLicenseService()}
}

// --- client (public) endpoints ---

func (c *SageXLicenseController) Activate(ctx *gin.Context) {
	var req service.SageXActivateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	status, err := c.svc.Activate(ctx.Request.Context(), req)
	if err != nil {
		switch {
		case err == service.ErrSageXLicenseNotFound:
			response.Error(ctx, http.StatusNotFound, "授权码不存在")
		case err == service.ErrSageXLicenseInvalidated:
			response.Error(ctx, http.StatusForbidden, "授权码已失效")
		case err == service.ErrSageXLicenseDeviceMismatch:
			response.Error(ctx, http.StatusForbidden, "授权码已绑定其他设备")
		default:
			response.Error(ctx, http.StatusBadRequest, err.Error())
		}
		return
	}
	response.Success(ctx, gin.H{"status": status}, "ok")
}

func (c *SageXLicenseController) Verify(ctx *gin.Context) {
	var req struct {
		Code              string `json:"code"`
		DeviceFingerprint string `json:"device_fingerprint"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	ok, status, err := c.svc.Verify(ctx.Request.Context(), req.Code, req.DeviceFingerprint)
	if err != nil {
		switch {
		case err == service.ErrSageXLicenseNotFound:
			response.Error(ctx, http.StatusNotFound, "授权码不存在")
		case err == service.ErrSageXLicenseInvalidated:
			response.Error(ctx, http.StatusForbidden, "授权码已失效")
		case err == service.ErrSageXLicenseDeviceMismatch:
			response.Error(ctx, http.StatusForbidden, "授权码已绑定其他设备")
		default:
			response.Error(ctx, http.StatusBadRequest, err.Error())
		}
		return
	}
	response.Success(ctx, gin.H{"valid": ok, "status": status}, "ok")
}

func (c *SageXLicenseController) Heartbeat(ctx *gin.Context) {
	var req service.SageXHeartbeatRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	// 采集服务端 IP 作为补充
	if req.IP == "" {
		req.IP = ctx.ClientIP()
	}
	if err := c.svc.Heartbeat(ctx.Request.Context(), req); err != nil {
		switch {
		case err == service.ErrSageXLicenseNotFound:
			response.Error(ctx, http.StatusNotFound, "授权码不存在")
		case err == service.ErrSageXLicenseInvalidated:
			response.Error(ctx, http.StatusForbidden, "授权码已失效")
		case err == service.ErrSageXLicenseDeviceMismatch:
			response.Error(ctx, http.StatusForbidden, "授权码已绑定其他设备")
		default:
			response.Error(ctx, http.StatusBadRequest, err.Error())
		}
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "ok")
}

// --- admin endpoints ---

func (c *SageXLicenseController) Issue(ctx *gin.Context) {
	code, err := c.svc.IssueCode(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "签发失败")
		return
	}
	response.Success(ctx, gin.H{"code": code}, "ok")
}

func (c *SageXLicenseController) ListCodes(ctx *gin.Context) {
	status := strings.TrimSpace(ctx.Query("status"))
	codes, err := c.svc.ListCodes(ctx.Request.Context(), status)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询失败")
		return
	}
	response.Success(ctx, codes, "ok")
}

func (c *SageXLicenseController) Invalidate(ctx *gin.Context) {
	code := strings.TrimSpace(ctx.Param("code"))
	var req struct {
		Reason string `json:"reason"`
	}
	_ = ctx.ShouldBindJSON(&req)
	if err := c.svc.InvalidateCode(ctx.Request.Context(), code, req.Reason); err != nil {
		if err == service.ErrSageXLicenseNotFound {
			response.Error(ctx, http.StatusNotFound, "授权码不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"code": code, "status": "invalidated"}, "ok")
}

func (c *SageXLicenseController) ListHeartbeats(ctx *gin.Context) {
	code := strings.TrimSpace(ctx.Query("code"))
	hbs, err := c.svc.ListHeartbeats(ctx.Request.Context(), code)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询失败")
		return
	}
	response.Success(ctx, hbs, "ok")
}

// RegisterAdminRoutes registers under admin group.
func (c *SageXLicenseController) RegisterAdminRoutes(auth *gin.RouterGroup) {
	g := auth.Group("/sagex/licenses")
	g.POST("/issue", c.Issue)
	g.GET("", c.ListCodes)
	g.POST("/:code/invalidate", c.Invalidate)
	g.GET("/heartbeats", c.ListHeartbeats)
}

// RegisterClientRoutes registers public client endpoints.
func (c *SageXLicenseController) RegisterClientRoutes(public *gin.RouterGroup) {
	g := public.Group("/integrations/sagex")
	g.POST("/activate", c.Activate)
	g.POST("/verify", c.Verify)
	g.POST("/heartbeat", c.Heartbeat)
}

var _ = model.SageXActivationCode{}
