package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type TypingPredictController struct {
	svc *service.TypingPredictService
}

// NewTypingPredictController 创建打字预测控制器
func NewTypingPredictController() *TypingPredictController {
	return &TypingPredictController{
		svc: service.GetTypingPredictService(),
	}
}

// PredictRequest 预测请求体
type PredictRequest struct {
	Text      string `json:"text" binding:"required"`
	SessionID string `json:"session_id,omitempty"`
}

// Predict 同步预测接口（最简化）
// POST /api/chat/typing-predict/predict
//
// 同步返回 IntentPrediction，前端拿到后直接渲染建议回复 UI。
// 不自动发送，仅展示。
func (c *TypingPredictController) Predict(ctx *gin.Context) {
	var req PredictRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	if len(req.Text) > 500 {
		response.Error(ctx, http.StatusBadRequest, "输入过长，请控制在 500 字以内")
		return
	}
	pred, err := c.svc.Predict(ctx.Request.Context(), req.Text, req.SessionID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "预测失败: "+err.Error())
		return
	}
	response.Success(ctx, pred, "预测成功")
}

// SSEStream SSE 推送模式（可选，用于高实时性场景）
// GET /api/chat/typing-predict/sse?session_id=xxx
//
// 实现说明：这条端点只产出两帧——连上时的 connected，与每 30s 一帧的 ping。
// 没有第三个生产者：预测结果不会从这条流里出来，本包和 service 层都没有
// 按 session_id 往这里推送的入口，所以客户端收到"连接活着"不等于"有预测可取"。
// 预测的实际通路是 POST /chat/typing-predict/predict 的同步返回。
//
// 要把它变成真推送，需要补一个按 session_id 分组的发布端，并在下面的 select
// 里加上它的 channel；在补齐之前，前端不要订阅这条流来等结果（当前无消费者）。
func (c *TypingPredictController) SSEStream(ctx *gin.Context) {
	sessionID := ctx.Query("session_id")
	if sessionID == "" {
		response.Error(ctx, http.StatusBadRequest, "session_id 必填")
		return
	}

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("X-Accel-Buffering", "no")

	flusher, ok := ctx.Writer.(http.Flusher)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "不支持流式响应")
		return
	}

	connected, _ := json.Marshal(map[string]any{"session_id": sessionID, "timestamp": time.Now().Unix()})
	if _, err := fmt.Fprintf(ctx.Writer, "event: connected\ndata: %s\n\n", connected); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Request.Context().Done():

			return
		case <-ticker.C:
			ping, _ := json.Marshal(map[string]any{
				"type":      "ping",
				"timestamp": time.Now().Unix(),
			})
			if _, err := fmt.Fprintf(ctx.Writer, "event: ping\ndata: %s\n\n", string(ping)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ManageTypingPredictController 管理端打字意图预测控制器
type ManageTypingPredictController struct {
	svc *service.TypingPredictService
}

// NewManageTypingPredictController 构造
func NewManageTypingPredictController() *ManageTypingPredictController {
	return &ManageTypingPredictController{svc: service.GetTypingPredictService()}
}

// Predict GET /api/manage/typing-predict?text=&session_id=
func (c *ManageTypingPredictController) Predict(ctx *gin.Context) {
	text := ctx.Query("text")
	sessionID := ctx.Query("session_id")
	if text == "" {
		response.Error(ctx, 400, "text 不能为空")
		return
	}
	pred, err := c.svc.Predict(ctx.Request.Context(), text, sessionID)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, pred, "ok")
}
