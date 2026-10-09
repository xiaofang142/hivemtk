package controller

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/sla"
	"hivemtk-user/internal/pkg/utils/response"
)

// SLOController SLO 读口控制器
type SLOController struct{}

// NewSLOController 构造 SLO 控制器
func NewSLOController() *SLOController {
	return &SLOController{}
}

// List 获取所有 SLO 状态
// GET /api/manage/slo
func (ctrl *SLOController) List(c *gin.Context) {
	states := sla.Default().AllStates()
	if states == nil {
		states = []sla.SLOState{}
	}
	response.Success(c, gin.H{"list": states}, "ok")
}
