// customer_service_plus_controller.go 客服工作台增强控制器（五层 L2）
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CustomerServicePlusController 控制器
type CustomerServicePlusController struct {
	svc *service.CustomerServicePlusService
}

// NewCustomerServicePlusController 构造（DI 在路由装配完成）
func NewCustomerServicePlusController() *CustomerServicePlusController {
	return &CustomerServicePlusController{svc: service.NewCustomerServicePlusServiceFromGlobal()}
}

// @Summary      编辑锁已获取
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/edit-lock [post]
// AcquireEditLock POST /api/customer-sessions/:id/edit-lock {holder}
func (c *CustomerServicePlusController) AcquireEditLock(ctx *gin.Context) {
	sessionID := ctx.Param("id")
	var req struct {
		Holder string `json:"holder"`
	}
	_ = ctx.ShouldBindJSON(&req)
	if req.Holder == "" {
		req.Holder = "user:" + anyToString(ctx.Value("user_id"))
	}
	lock, ok := c.svc.AcquireEditLock(ctx.Request.Context(), sessionID, req.Holder)
	if !ok {
		response.Error(ctx, http.StatusConflict, "会话正被 "+lock.Holder+" 编辑")
		return
	}
	response.Success(ctx, lock, "编辑锁已获取")
}

// @Summary      编辑锁已释放
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/edit-lock [delete]
// ReleaseEditLock DELETE /api/customer-sessions/:id/edit-lock {holder}
//
// holder 缺省按「当前登录人」补：不补就会走到 service 的 `holder != ""` 分支，
// 于是任何人 DELETE 一个不带 body 的请求都能把别人的锁解掉（A7）。
// 补成 user:<id> 后与 AcquireEditLock 同口径——只能解自己的锁，别人的锁等 TTL 过期。
func (c *CustomerServicePlusController) ReleaseEditLock(ctx *gin.Context) {
	sessionID := ctx.Param("id")
	var req struct {
		Holder string `json:"holder"`
	}
	_ = ctx.ShouldBindJSON(&req)
	if req.Holder == "" {
		req.Holder = "user:" + anyToString(ctx.Value("user_id"))
	}
	if !c.svc.ReleaseEditLock(ctx.Request.Context(), sessionID, req.Holder) {
		response.Error(ctx, http.StatusNotFound, "无有效编辑锁或非持有人")
		return
	}
	response.Success(ctx, gin.H{"released": true}, "编辑锁已释放")
}

// @Summary      查询会话编辑锁状态
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/edit-lock [get]
// GetEditLock GET /api/customer-sessions/:id/edit-lock
func (c *CustomerServicePlusController) GetEditLock(ctx *gin.Context) {
	lock, ok := c.svc.GetEditLock(ctx.Request.Context(), ctx.Param("id"))
	if !ok {
		response.Success(ctx, gin.H{"locked": false}, "ok")
		return
	}
	response.Success(ctx, gin.H{"locked": true, "lock": lock}, "ok")
}

// @Summary      内部备注已保存
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/internal-notes [post]
// AddInternalNote POST /api/customer-sessions/:id/internal-notes {content}
func (c *CustomerServicePlusController) AddInternalNote(ctx *gin.Context) {
	sessionID := ctx.Param("id")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：content 必填")
		return
	}
	senderID, senderName := currentStaffInfo(ctx)
	msg, err := c.svc.AddInternalNote(ctx.Request.Context(), sessionID, req.Content, senderID, senderName)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, msg, "内部备注已保存")
}

// @Summary      查询会话内部备注列表
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/internal-notes [get]
// ListInternalNotes GET /api/customer-sessions/:id/internal-notes
func (c *CustomerServicePlusController) ListInternalNotes(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	list, err := c.svc.ListInternalNotes(ctx.Request.Context(), ctx.Param("id"), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      查询会话标签规则列表
// @Tags      customer-service-plus
// @Router    /api/session-tag/rules [get]
// ListTagRules GET /api/session-tag/rules
func (c *CustomerServicePlusController) ListTagRules(ctx *gin.Context) {
	list, err := c.svc.ListTagRules(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      规则已保存
// @Tags      customer-service-plus
// @Router    /api/session-tag/rules [post]
// SaveTagRule POST /api/session-tag/rules {code, rule_condition}
func (c *CustomerServicePlusController) SaveTagRule(ctx *gin.Context) {
	var req service.TagRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if err := c.svc.SaveTagRule(ctx.Request.Context(), &req); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"saved": true}, "规则已保存")
}

// @Summary      标签规则已执行
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/apply-tag-rule [post]
// ApplyTagRule POST /api/customer-sessions/:id/apply-tag-rule
func (c *CustomerServicePlusController) ApplyTagRule(ctx *gin.Context) {
	res, err := c.svc.ApplyTagRule(ctx.Request.Context(), ctx.Param("id"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "标签规则已执行")
}

// @Summary      查询客服座席状态看板
// @Tags      customer-service-plus
// @Router    /api/customer-service/agent-status-board [get]
// GetAgentStatusBoard GET /api/customer-service/agent-status-board
func (c *CustomerServicePlusController) GetAgentStatusBoard(ctx *gin.Context) {
	board, err := c.svc.GetAgentStatusBoard(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": board, "total": len(board)}, "ok")
}

// @Summary      查询快捷回复文件夹列表
// @Tags      customer-service-plus
// @Router    /api/quick-reply/folders [get]
// ListQuickReplyFolders GET /api/quick-reply/folders
func (c *CustomerServicePlusController) ListQuickReplyFolders(ctx *gin.Context) {
	list, err := c.svc.ListFolders(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      文件夹已创建
// @Tags      customer-service-plus
// @Router    /api/quick-reply/folders [post]
// CreateQuickReplyFolder POST /api/quick-reply/folders {name}
func (c *CustomerServicePlusController) CreateQuickReplyFolder(ctx *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name 必填")
		return
	}
	f, err := c.svc.CreateFolder(ctx.Request.Context(), req.Name)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, f, "文件夹已创建")
}

// @Summary      顺序已更新
// @Tags      customer-service-plus
// @Router    /api/quick-reply/folders/{id}/reorder [post]
// ReorderQuickReplyFolder POST /api/quick-reply/folders/:id/reorder {sort_order}
func (c *CustomerServicePlusController) ReorderQuickReplyFolder(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	var req struct {
		SortOrder *int `json:"sort_order" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.SortOrder == nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：sort_order 必填")
		return
	}
	if err := c.svc.ReorderFolder(ctx.Request.Context(), id, *req.SortOrder); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"updated": true}, "顺序已更新")
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if u, ok := v.(uint); ok {
		return strconv.FormatUint(uint64(u), 10)
	}
	return ""
}

func currentStaffInfo(ctx *gin.Context) (string, string) {
	id := anyToString(ctx.Value("user_id"))
	name := ""
	if v := ctx.Value("username"); v != nil {
		name = anyToString(v)
	}
	if name == "" {
		name = "staff-" + id
	}
	return id, name
}

// @Summary      文件夹已删除
// @Tags      customer-service-plus
// @Router    /api/quick-reply/folders/{id} [delete]
func (c *CustomerServicePlusController) DeleteQuickReplyFolder(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.DeleteFolder(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "文件夹已删除")
}

// @Summary      分群已保存
// @Tags      customer-service-plus
// @Router    /api/user-segments [post]
// CreateSegment POST /api/user-segments {name, rules, trigger}
// （where_sql 注入通道已移除，多余字段被绑定层静默忽略）
func (c *CustomerServicePlusController) CreateSegment(ctx *gin.Context) {
	var req service.SegmentSaveRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	seg, err := c.svc.SaveSegment(ctx.Request.Context(), &req)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, seg, "分群已保存")
}

// @Summary      查询用户分群列表
// @Tags      customer-service-plus
// @Router    /api/user-segments [get]
// ListSegments GET /api/user-segments
func (c *CustomerServicePlusController) ListSegments(ctx *gin.Context) {
	limit := 100
	if l := ctx.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	list, err := c.svc.ListSegments(ctx.Request.Context(), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      查询消息中心死信队列列表
// @Tags      customer-service-plus
// @Router    /api/message-hub/dlq [get]
// DLQList GET /api/message-hub/dlq?limit=50
func (c *CustomerServicePlusController) DLQList(ctx *gin.Context) {
	limit := 50
	if l := ctx.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	list, total, err := c.svc.DLQList(ctx.Request.Context(), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary      已重新入队
// @Tags      customer-service-plus
// @Router    /api/message-hub/dlq/{id}/retry [post]
// DLQRetryOne POST /api/message-hub/dlq/:id/retry
func (c *CustomerServicePlusController) DLQRetryOne(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.DLQRetryOne(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"retried": true}, "已重新入队")
}

// @Summary      已丢弃
// @Tags      customer-service-plus
// @Router    /api/message-hub/dlq/{id} [delete]
// DLQDrop DELETE /api/message-hub/dlq/:id
func (c *CustomerServicePlusController) DLQDrop(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.DLQDrop(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"dropped": true}, "已丢弃")
}

// @Summary      批量重试完成
// @Tags      customer-service-plus
// @Router    /api/message-hub/dlq/batch-retry [post]
// DLQBatchRetry POST /api/message-hub/dlq/batch-retry（真实数量返回）
func (c *CustomerServicePlusController) DLQBatchRetry(ctx *gin.Context) {
	n, err := c.svc.DLQBatchRetry(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"requeued": n}, "批量重试完成")
}

// @Summary      查询办公时间策略配置
// @Tags      customer-service-plus
// @Router    /api/office-hours [get]
// GetOfficeHours GET /api/office-hours
func (c *CustomerServicePlusController) GetOfficeHours(ctx *gin.Context) {
	response.Success(ctx, service.GetOfficeHoursService().GetConfig(ctx.Request.Context()), "ok")
}

// @Summary      办公时间策略已保存
// @Tags      customer-service-plus
// @Router    /api/office-hours [put]
// SaveOfficeHours PUT /api/office-hours
func (c *CustomerServicePlusController) SaveOfficeHours(ctx *gin.Context) {
	var cfg service.OfficeHoursConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if err := service.GetOfficeHoursService().SaveConfig(ctx.Request.Context(), &cfg); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, cfg, "办公时间策略已保存")
}

// @Summary      优先级已更新
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/priority [put]
// SetSessionPriority PUT /api/customer-sessions/:id/priority {level}
func (c *CustomerServicePlusController) SetSessionPriority(ctx *gin.Context) {
	var req struct {
		Level *int `json:"level" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Level == nil {
		response.Error(ctx, http.StatusBadRequest, "level 必填(0-3)")
		return
	}
	if err := c.svc.SetSessionPriority(ctx.Request.Context(), ctx.Param("id"), *req.Level); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"priority": *req.Level}, "优先级已更新")
}

// @Summary      会话已暂缓
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/snooze [post]
// SnoozeSession POST /api/customer-sessions/:id/snooze {hours}
func (c *CustomerServicePlusController) SnoozeSession(ctx *gin.Context) {
	var req struct {
		Hours *float64 `json:"hours" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Hours == nil {
		response.Error(ctx, http.StatusBadRequest, "hours 必填")
		return
	}
	until, err := c.svc.SnoozeSession(ctx.Request.Context(), ctx.Param("id"), *req.Hours)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"snoozed_until": until}, "会话已暂缓")
}

// @Summary      已恢复活跃
// @Tags      customer-service-plus
// @Router    /api/customer-sessions/{id}/snooze [delete]
// UnsnoozeSession DELETE /api/customer-sessions/:id/snooze
func (c *CustomerServicePlusController) UnsnoozeSession(ctx *gin.Context) {
	if err := c.svc.UnsnoozeSession(ctx.Request.Context(), ctx.Param("id")); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"unsnoozed": true}, "已恢复活跃")
}
