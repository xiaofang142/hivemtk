package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ChannelIdentifyCodeController 识别码管理（T016 仅双写，切换读路径在 US4）。
type ChannelIdentifyCodeController struct {
	svc service.ChannelIdentifyService
}

// NewChannelIdentifyCodeController 创建识别码控制器。
func NewChannelIdentifyCodeController(svc service.ChannelIdentifyService) *ChannelIdentifyCodeController {
	return &ChannelIdentifyCodeController{svc: svc}
}

// @Summary 识别码列表
// @Tags identify-code
// @Router /api/identify-codes [get]
func (c *ChannelIdentifyCodeController) List(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	rows, err := c.svc.List(ctx.Request.Context(), tid)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询识别码失败")
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

func (c *ChannelIdentifyCodeController) Get(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	channelID, err := strconv.ParseUint(ctx.Param("channel_id"), 10, 32)
	if err != nil || channelID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道 ID")
		return
	}
	row, err := c.svc.Get(ctx.Request.Context(), tid, uint(channelID))
	if err != nil {
		if errors.Is(err, service.ErrChannelIdentifyNotFound) {
			response.Error(ctx, http.StatusNotFound, "识别码不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "查询识别码失败")
		return
	}
	response.Success(ctx, row, "ok")
}

// Rotate 生成/重置识别码（version+1 + 双写），T016 阶段不改读路径。
func (c *ChannelIdentifyCodeController) Rotate(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	channelID, err := strconv.ParseUint(ctx.Param("channel_id"), 10, 32)
	if err != nil || channelID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道 ID")
		return
	}
	row, err := c.svc.CreateOrRotate(ctx.Request.Context(), tid, uint(channelID))
	if err != nil {
		response.ErrorFromDB(ctx, err, "生成识别码失败")
		return
	}
	response.Success(ctx, row, "生成成功")
}

// RotateForAccount 生成/重置识别码并把冗余码写给指定账号（CH-06 多账号精确定位）。
func (c *ChannelIdentifyCodeController) RotateForAccount(ctx *gin.Context) {
	tid, ok := channelTenantID(ctx)
	if !ok {
		return
	}
	channelID, err := strconv.ParseUint(ctx.Param("channel_id"), 10, 32)
	if err != nil || channelID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的渠道 ID")
		return
	}
	var accountID uint64
	if v := strings.TrimSpace(ctx.Query("account_id")); v != "" {
		n, perr := strconv.ParseUint(v, 10, 32)
		if perr != nil || n == 0 {
			response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
			return
		}
		accountID = n
	} else {
		var body struct {
			AccountID uint `json:"account_id"`
		}
		if berr := ctx.ShouldBindJSON(&body); berr == nil && body.AccountID > 0 {
			accountID = uint64(body.AccountID)
		}
	}
	if accountID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的账号 ID")
		return
	}
	row, err := c.svc.CreateOrRotateForAccount(ctx.Request.Context(), tid, uint(channelID), uint(accountID))
	if err != nil {
		if errors.Is(err, service.ErrIdentifyAccountRequired) {
			response.Error(ctx, http.StatusBadRequest, "账号与渠道不匹配")
			return
		}
		response.ErrorFromDB(ctx, err, "生成识别码失败")
		return
	}
	response.Success(ctx, row, "生成成功")
}
