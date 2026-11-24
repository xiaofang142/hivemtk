package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type LeadMiningClueActionController struct {
	svc *service.ClueService
}

func NewLeadMiningClueActionController() *LeadMiningClueActionController {
	return &LeadMiningClueActionController{svc: service.NewClueServiceDefault(nil)}
}

func (c *LeadMiningClueActionController) RegisterRoutes(auth *gin.RouterGroup, admin *gin.RouterGroup) {
	auth.GET("/lead-mining/clues/:id", c.GetClue)
	admin.POST("/lead-mining/clues/:id/status", c.UpdateStatus)
	admin.POST("/lead-mining/clues/:id/merge", c.Merge)
	admin.POST("/lead-mining/clues/:id/convert", c.Convert)
}

func (c *LeadMiningClueActionController) GetClue(ctx *gin.Context) {
	id := strings.TrimSpace(ctx.Param("id"))
	clue, err := c.svc.GetLeadMiningClueByID(ctx.Request.Context(), id)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索失败")
		return
	}
	response.Success(ctx, clue, "ok")
}

func (c *LeadMiningClueActionController) UpdateStatus(ctx *gin.Context) {
	var req struct {
		Status   string `json:"status"`
		Reason   string `json:"reason"`
		Operator string `json:"operator"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	id := strings.TrimSpace(ctx.Param("id"))
	if req.Operator == "" {
		if v, ok := ctx.Get("user_id"); ok {
			req.Operator = strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(leadMiningToString(v)), " ", ""))
		}
	}
	if err := c.svc.UpdateLeadMiningClueStatus(ctx.Request.Context(), id, req.Status, req.Reason, req.Operator); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": req.Status}, "状态已更新")
}

func (c *LeadMiningClueActionController) Merge(ctx *gin.Context) {
	var req struct {
		TargetID string `json:"target_id"`
		Reason   string `json:"reason"`
		Operator string `json:"operator"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	sourceID := strings.TrimSpace(ctx.Param("id"))
	targetID := strings.TrimSpace(req.TargetID)
	if targetID == "" {
		targetID = strings.TrimSpace(ctx.Query("target_id"))
	}
	if req.Operator == "" {
		if v, ok := ctx.Get("user_id"); ok {
			req.Operator = leadMiningToString(v)
		}
	}
	if err := c.svc.MergeLeadMiningClue(ctx.Request.Context(), targetID, sourceID, req.Reason, req.Operator); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"target_id": targetID, "source_id": sourceID, "status": "duplicate"}, "合并成功")
}

func (c *LeadMiningClueActionController) Convert(ctx *gin.Context) {
	id := strings.TrimSpace(ctx.Param("id"))
	operator := ""
	if v, ok := ctx.Get("user_id"); ok {
		operator = leadMiningToString(v)
	}
	if err := c.svc.MarkLeadMiningClueConverted(ctx.Request.Context(), id, operator); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id, "status": "qualified"}, "已标记为可转商机")
}

func leadMiningToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case uint:
		return strings.TrimSpace(strings.TrimLeft(strings.ReplaceAll(fmtUint(t), " ", ""), "0"))
	case uint64:
		return fmtUint64(t)
	case int:
		if t < 0 {
			return ""
		}
		return fmtUint64(uint64(t))
	case int64:
		if t < 0 {
			return ""
		}
		return fmtUint64(uint64(t))
	default:
		return ""
	}
}

func fmtUint(v uint) string { return fmtUint64(uint64(v)) }

func fmtUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
