package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	opssvc "hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// MarketingFlowSyncController 营销流程 AB 结果回流控制器
type MarketingFlowSyncController struct {
	abService *opssvc.ABExperimentService
}

// NewMarketingFlowSyncController 构造
func NewMarketingFlowSyncController() *MarketingFlowSyncController {
	return &MarketingFlowSyncController{abService: opssvc.NewABExperimentService()}
}

// @Summary      流程未绑定实验
// @Tags      marketing-flow-sync
// @Router    /api/marketing-flows/{id}/sync-ab-results [post]
// SyncABResults POST /api/marketing-flows/:id/sync-ab-results
//
// 语义：查找绑定该流程（source_id=flowID）的实验 → 重算结果并返回
// （流程画布展示实时实验数字；未绑定实验返回空列表）。
func (c *MarketingFlowSyncController) SyncABResults(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的流程 ID")
		return
	}
	sourceID := strconv.FormatUint(id, 10)
	experiment, err := c.abService.GetExperimentBySourceID(ctx.Request.Context(), sourceID)
	if err != nil {
		if utils.IsRecordNotFound(err) {
			response.Success(ctx, gin.H{"synced": false, "results": []any{}}, "流程未绑定实验")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if err := c.abService.CalculateResults(ctx.Request.Context(), experiment.ID); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "实验结果计算失败: "+err.Error())
		return
	}
	results, err := c.abService.GetExperimentResults(ctx.Request.Context(), experiment.ID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"synced": true, "experiment_id": experiment.ID, "results": results}, "实验结果已回流")
}

// CustomerEventBatchController 批量事件控制器
type CustomerEventBatchController struct {
	tracker *service.EventTracker
}

// NewCustomerEventBatchController 构造
func NewCustomerEventBatchController() *CustomerEventBatchController {
	return &CustomerEventBatchController{tracker: service.NewEventTracker(service.NewCustomerService())}
}

// @Summary      批量事件已入库
// @Tags      customer-event-batch
// @Router    /api/customer-events/batch [post]
// TrackBatch POST /api/customer-events/batch {events: [{event, properties, user_id, ...}]}
func (c *CustomerEventBatchController) TrackBatch(ctx *gin.Context) {
	var req struct {
		Events []struct {
			Event      string         `json:"event" binding:"required"`
			Properties map[string]any `json:"properties"`
			UserID     string         `json:"user_id"`
			SessionID  string         `json:"session_id"`
			Timestamp  int64          `json:"ts"`
		} `json:"events" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if len(req.Events) > 200 {
		response.Error(ctx, http.StatusBadRequest, "单批最多 200 条事件")
		return
	}
	accepted := 0
	for i := range req.Events {
		e := &req.Events[i]
		props := e.Properties
		if props == nil {
			props = map[string]any{}
		}
		if e.SessionID != "" {
			props["session_id"] = e.SessionID
		}
		if e.Timestamp > 0 {
			props["client_ts"] = time.UnixMilli(e.Timestamp).Format(time.RFC3339)
		}
		customerID := e.UserID
		if customerID == "" {
			customerID, _ = props["customer_id"].(string)
		}
		dto := &service.EventDTO{
			CustomerID:  customerID,
			EventType:   eventTypeOf(e.Event),
			EventSource: "website",
			EventData:   props,
		}
		if err := c.tracker.Track(context.WithoutCancel(ctx.Request.Context()), dto); err == nil {
			accepted++
		}
	}
	response.Success(ctx, gin.H{"accepted": accepted, "total": len(req.Events)}, "批量事件已入库")
}

func eventTypeOf(name string) model.EventType {
	return model.EventType(name)
}

// WebVitalsController 控制器
type WebVitalsController struct {
	svc *opssvc.WebVitalService
}

// NewWebVitalsController 构造
func NewWebVitalsController() *WebVitalsController {
	return &WebVitalsController{svc: opssvc.NewWebVitalService()}
}

// @Summary POST /api/monitor/web-vitals
// @Tags      web-vitals
// @Router    /api/monitor/web-vitals [post]
// Report POST /api/monitor/web-vitals
func (c *WebVitalsController) Report(ctx *gin.Context) {
	var req struct {
		Name      string  `json:"name" binding:"required"`
		Value     float64 `json:"value"`
		Rating    string  `json:"rating"`
		Page      string  `json:"page"`
		ID        string  `json:"id"`
		SessionID string  `json:"session_id"`
		TS        int64   `json:"ts"`
		UA        string  `json:"ua"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	rec := &model.WebVitalRecord{
		Metric:    req.Name,
		Value:     req.Value,
		Rating:    req.Rating,
		Page:      req.Page,
		SessionID: req.SessionID,
		UserAgent: req.UA,
	}
	if rec.UserAgent == "" {
		rec.UserAgent = ctx.Request.UserAgent()
	}
	if err := c.svc.Report(ctx.Request.Context(), rec); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"recorded": true}, "ok")
}
