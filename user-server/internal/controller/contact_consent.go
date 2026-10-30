package controller

import (
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ContactConsentController D04：客户授权时间线与偏好管理。
// 写操作的 actor 一律取登录上下文（user_id），不接受请求体自报身份。
type ContactConsentController struct {
	svc *service.ContactConsentService
}

// NewContactConsentController 构造
func NewContactConsentController(svc *service.ContactConsentService) *ContactConsentController {
	return &ContactConsentController{svc: svc}
}

type consentRecordRequest struct {
	Decision         string `json:"decision" binding:"required"` // grant / revoke
	Purpose          string `json:"purpose"`
	ChannelScope     string `json:"channel_scope"`
	StatementVersion string `json:"statement_version"`
	Source           string `json:"source"`
	EvidenceRef      string `json:"evidence_ref"`
}

// Timeline GET /api/contacts/:customer_id/consents — 授权时间线
func (ctrl *ContactConsentController) Timeline(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	events, err := ctrl.svc.Timeline(ctx.Request.Context(), ctx.Param("customer_id"), limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": events, "total": len(events)}, "查询成功")
}

// Record POST /api/contacts/:customer_id/consents — 追加授权/撤回事实
func (ctrl *ContactConsentController) Record(ctx *gin.Context) {
	var req consentRecordRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：decision 必填（grant / revoke），其余字段可选")
		return
	}
	actor := ginContextUserID(ctx)
	err := ctrl.svc.Record(ctx.Request.Context(), service.ConsentRecordInput{
		OneID:            ctx.Param("customer_id"),
		Purpose:          req.Purpose,
		ChannelScope:     req.ChannelScope,
		Decision:         req.Decision,
		StatementVersion: req.StatementVersion,
		Source:           req.Source,
		Actor:            actor,
		EvidenceRef:      req.EvidenceRef,
		OccurredAt:       time.Now(),
	})
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"recorded": true, "actor": actor}, "已记录")
}

type preferenceRequest struct {
	Purpose     string `json:"purpose" binding:"required"`
	Channel     string `json:"channel"`
	Paused      bool   `json:"paused"`
	PausedUntil string `json:"paused_until"` // RFC3339，可空
	Timezone    string `json:"timezone"`
}

// SetPreference PUT /api/contacts/:customer_id/preferences — 写偏好快照
func (ctrl *ContactConsentController) SetPreference(ctx *gin.Context) {
	var req preferenceRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：purpose 必填（marketing / support / transactional），paused/paused_until/timezone 可选")
		return
	}
	var pausedUntil *time.Time
	if req.PausedUntil != "" {
		t, err := time.Parse(time.RFC3339, req.PausedUntil)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "paused_until 必须是 RFC3339 时刻")
			return
		}
		pausedUntil = &t
	}
	if err := ctrl.svc.SetPreference(ctx.Request.Context(), ctx.Param("customer_id"), req.Purpose, req.Channel, req.Paused, pausedUntil, req.Timezone); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"updated": true}, "已更新")
}

// ListPreferences GET /api/contacts/:customer_id/preferences — 偏好列表
func (ctrl *ContactConsentController) ListPreferences(ctx *gin.Context) {
	prefs, err := ctrl.svc.Preferences(ctx.Request.Context(), ctx.Param("customer_id"))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": prefs, "total": len(prefs)}, "查询成功")
}

// ginContextUserID 从登录上下文取操作者；取不到时给可追溯的占位（写路径拒绝匿名自报）。
func ginContextUserID(ctx *gin.Context) string {
	if v, ok := ctx.Get("user_id"); ok {
		switch id := v.(type) {
		case string:
			if id != "" {
				return id
			}
		case uint:
			if id != 0 {
				return "user:" + strconv.FormatUint(uint64(id), 10)
			}
		case float64:
			if id != 0 {
				return "user:" + strconv.FormatUint(uint64(id), 10)
			}
		}
	}
	return "system:unauthenticated"
}

// 编译期防呆：模型常量被引用（防 import 未用告警的同时固定契约字面量）
var _ = model.ConsentDecisionGrant
