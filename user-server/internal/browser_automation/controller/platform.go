package controller

import (
	"net/http"

	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// PlatformController 平台注册表只读接口（前端平台选择器/能力矩阵用）
type PlatformController struct{}

func NewPlatformController() *PlatformController { return &PlatformController{} }

// platformDTO 平台概要（前端只需 identifier/能力/是否可发评论）
type platformDTO struct {
	Identifier       string   `json:"identifier"`
	Capabilities     []string `json:"capabilities"`
	CanPostComment   bool     `json:"can_post_comment"`
	MaxConcurrentJobs int     `json:"max_concurrent_jobs"`
}

// List GET /browser-automation/platforms —— 列出已注册平台（L3 注册表实时读取）
func (c *PlatformController) List(ctx *gin.Context) {
	list := make([]platformDTO, 0, 4)
	for _, p := range platform.List() {
		caps := make([]string, 0, len(p.Capabilities()))
		for _, cp := range p.Capabilities() {
			caps = append(caps, string(cp))
		}
		list = append(list, platformDTO{
			Identifier:       p.Identifier(),
			Capabilities:     caps,
			CanPostComment:   platform.HasCapability(p, platform.CapPostComment),
			MaxConcurrentJobs: p.MaxConcurrentJobs(),
		})
	}
	response.Success(ctx, list, "ok")
}

// Locators GET /browser-automation/platforms/:id/locators —— 平台定位表（编排面板预设/提示用）
func (c *PlatformController) Locators(ctx *gin.Context) {
	id := ctx.Param("id")
	p, err := platform.Get(id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, p.Locators(), "ok")
}
