package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type CustomerServiceController struct {
	svc *service.CustomerQueueService
}

// NewCustomerServiceController 创建客服队列控制器
func NewCustomerServiceController() *CustomerServiceController {
	return &CustomerServiceController{svc: service.NewCustomerQueueService()}
}

// NewCustomerServiceControllerWithService 注入 Service（测试用）
func NewCustomerServiceControllerWithService(svc *service.CustomerQueueService) *CustomerServiceController {
	return &CustomerServiceController{svc: svc}
}

// QueueSnapshot 队列快照
type QueueSnapshot = service.QueueSnapshot

// GetQueue 获取客服队列长度（等待中的会话数）
// GET /api/customer-service/queue
func (c *CustomerServiceController) GetQueue(ctx *gin.Context) {
	snapshot, err := c.svc.GetQueue(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, 500, "获取队列失败: "+err.Error())
		return
	}
	response.Success(ctx, snapshot, "获取成功")
}

// CapacitySnapshot 坐席容量快照
type CapacitySnapshot = service.CapacitySnapshot

// GetCapacity 获取坐席容量聚合视图
// GET /api/customer-service/capacity
func (c *CustomerServiceController) GetCapacity(ctx *gin.Context) {
	result, err := c.svc.GetCapacity(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, 500, "获取容量失败: "+err.Error())
		return
	}
	response.Success(ctx, result, "获取成功")
}

// AgentStatusItem 简化的坐席状态项（不含敏感字段）
type AgentStatusItem = service.AgentStatusItem

// GetAgents 获取所有坐席的实时状态列表
// GET /api/customer-service/agents
func (c *CustomerServiceController) GetAgents(ctx *gin.Context) {
	items, err := c.svc.GetAgents(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, 500, "查询坐席列表失败: "+err.Error())
		return
	}
	response.SuccessWithList(ctx, items, int64(len(items)))
}
