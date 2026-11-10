// intent_feedback.go TB-09：意图纠错反馈 HTTP 面（管理端，AdminAuth 闸）。
package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// IntentFeedbackController 意图纠错控制器
type IntentFeedbackController struct {
	svc *service.IntentFeedbackService
}

func NewIntentFeedbackController(svc *service.IntentFeedbackService) *IntentFeedbackController {
	return &IntentFeedbackController{svc: svc}
}

// Correct POST /api/intent/feedback/correct
func (c *IntentFeedbackController) Correct(ctx *gin.Context) {
	var in service.IntentFeedbackCorrectInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：log_id 与 corrected_type 为必填")
		return
	}
	if in.Operator == "" {
		if v, ok := ctx.Get("username"); ok {
			if s, ok := v.(string); ok {
				in.Operator = s
			}
		}
	}
	out, err := c.svc.CorrectIntent(ctx.Request.Context(), in)
	if err != nil {
		replyCustomIntegrationError(ctx, err, "纠错失败")
		return
	}
	response.Success(ctx, out, "纠错成功（词面已热生效）")
}

// Override GET /api/intent/feedback/override（当前词面覆盖回显）
func (c *IntentFeedbackController) Override(ctx *gin.Context) {
	out, err := c.svc.ListOverride(ctx.Request.Context())
	if err != nil {
		replyCustomIntegrationError(ctx, err, "获取词面覆盖失败")
		return
	}
	response.Success(ctx, out, "获取成功")
}
