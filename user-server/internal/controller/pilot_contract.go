package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// PilotContractController 只绑定草稿与响应，不访问平台，不读取仓储。
type PilotContractController struct{ svc service.PilotContractService }

func NewPilotContractController(svc service.PilotContractService) *PilotContractController {
	return &PilotContractController{svc: svc}
}

// Get 返回 200（包括未配置）；缺租户 403；存储或安装身份读取失败 503。
// @Router /api/pilot/contract [get]
func (ctrl *PilotContractController) Get(ctx *gin.Context) {
	out, err := ctrl.svc.Load(ctx.Request.Context())
	if err != nil {
		pilotContractError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Validate 只做本地模拟校验，200 携带阻塞原因；非法输入 400，租户错配 403。
// @Router /api/pilot/contract/validate [post]
func (ctrl *PilotContractController) Validate(ctx *gin.Context) {
	in, ok := bindPilotContract(ctx)
	if !ok {
		return
	}
	out, err := ctrl.svc.Validate(ctx.Request.Context(), in)
	if err != nil {
		pilotContractError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

// Save 200 保存草稿，400 非法边界，403 租户错配，409 修订冲突，503 存储故障。
// @Router /api/pilot/contract [put]
func (ctrl *PilotContractController) Save(ctx *gin.Context) {
	in, ok := bindPilotContract(ctx)
	if !ok {
		return
	}
	out, err := ctrl.svc.Save(ctx.Request.Context(), in, currentUID(ctx))
	if err != nil {
		pilotContractError(ctx, err)
		return
	}
	response.Success(ctx, out, "ok")
}

func bindPilotContract(ctx *gin.Context) (dto.PilotContract, bool) {
	var in dto.PilotContract
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return in, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return in, false
	}
	return in, true
}

func pilotContractError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPilotTenant):
		response.Error(ctx, http.StatusForbidden, response.ErrPermissionDenied)
	case errors.Is(err, service.ErrPilotInvalid):
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
	case errors.Is(err, service.ErrPilotConflict):
		response.Error(ctx, http.StatusConflict, response.ErrBusinessError)
	default:
		response.Error(ctx, http.StatusServiceUnavailable, response.ErrBusinessError)
	}
}
