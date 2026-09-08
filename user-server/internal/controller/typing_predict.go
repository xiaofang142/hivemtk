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
// 实现说明：
// 与既有 SSEDashboard 不同，这里采用长轮询式 SSE——
// 客户端连接后发送心跳，服务端暂存请求，
// 后端通过 SSEPub 注入预测结果并推送。
//
// 为简化实现，本版本 SSE 仅推送心跳，实际预测走 POST /predict 同步返回。
// 这样避免了跨 goroutine 的连接管理复杂度，但仍预留了 SSE 端点。
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

	fmt.Fprintf(ctx.Writer, "event: connected\ndata: {\"session_id\":\"%s\",\"timestamp\":%d}\n\n",
		sessionID, time.Now().Unix())
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
			fmt.Fprintf(ctx.Writer, "event: ping\ndata: %s\n\n", string(ping))
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
