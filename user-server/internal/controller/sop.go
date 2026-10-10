package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SOPController SOP 智能体控制器
type SOPController struct {
	svc *service.SOPService
}

// NewSOPController 创建 SOP 控制器
func NewSOPController(svc *service.SOPService) *SOPController {
	return &SOPController{svc: svc}
}

// Create godoc
// @Summary      创建 SOP 智能体
// @Description  创建一个新的销冠 SOP 智能体，关联场景、动作链、知识库
// @Tags         SOP
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  service.CreateRequest  true  "SOP 创建参数"
// @Success      200   {object}  response.Response  "创建成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/sop [post]
func (c *SOPController) Create(ctx *gin.Context) {
	var req service.CreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	agent, err := c.svc.Create(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, agent, "创建成功")
}

// Update godoc
// @Summary      更新 SOP 智能体
// @Description  更新指定 ID 的 SOP 智能体配置
// @Tags         SOP
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path   int                       true  "SOP ID"
// @Param        body  body   service.CreateRequest  true  "更新参数"
// @Success      200   {object}  response.Response  "更新成功"
// @Router       /api/sop/{id} [put]
func (c *SOPController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	var req service.CreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agent, err := c.svc.Update(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, agent, "更新成功")
}

// Get godoc
// @Summary      获取 SOP 详情
// @Description  根据 ID 返回 SOP 完整定义
// @Tags         SOP
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "SOP ID"
// @Success      200  {object}  response.Response  "成功"
// @Failure      404  {object}  response.Response  "未找到"
// @Router       /api/sop/{id} [get]
func (c *SOPController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	agent, err := c.svc.Get(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "SOP 不存在")
		return
	}
	response.Success(ctx, agent, "查询成功")
}

// List godoc
// @Summary      SOP 智能体列表
// @Description  按场景分页查询 SOP 智能体
// @Tags         SOP
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        scenario  query  string  false  "场景编码"
// @Param        page      query  int     false  "页码"  default(1)
// @Param        page_size query  int     false  "每页"   default(20)
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/sop [get]
func (c *SOPController) List(ctx *gin.Context) {
	scenario := ctx.Query("scenario")
	status := ctx.Query("status")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	list, total, err := c.svc.List(ctx.Request.Context(), scenario, status, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// Delete 删除
func (c *SOPController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

// Activate 启用
func (c *SOPController) Activate(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	if err := c.svc.Activate(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "启用成功")
}

// Deactivate 停用
func (c *SOPController) Deactivate(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	if err := c.svc.Deactivate(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "停用成功")
}

// Execute 启动执行
func (c *SOPController) Execute(ctx *gin.Context) {
	var req dto.ExecuteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	exec, err := c.svc.Execute(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, redactExecution(exec), "执行成功")
}

// Step 单步推进
func (c *SOPController) Step(ctx *gin.Context) {
	var req dto.StepRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	exec, err := c.svc.Step(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, redactExecution(exec), "推进成功")
}

// Pause 暂停
func (c *SOPController) Pause(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.Pause(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "暂停成功")
}

// Resume 恢复
func (c *SOPController) Resume(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.Resume(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "恢复成功")
}

// Cancel 取消
func (c *SOPController) Cancel(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	if err := c.svc.Cancel(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "取消成功")
}

// GetExecution 详情
func (c *SOPController) GetExecution(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID")
		return
	}
	exec, err := c.svc.GetExecution(ctx.Request.Context(), uint(id))
	if err != nil {
		// "查不到"与"这次读失败"分两格回（服务侧本来就分了 ErrSOPExecNotFound 与故障）：
		// 都写成 404 时，一次 DB 抖动在界面上读起来就是"这条执行被人删了"，
		// 而运维会去查谁删的。与待办层、审批层同一口径。
		if errors.Is(err, service.ErrSOPExecNotFound) {
			response.NotFound(ctx, "执行不存在")
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, redactExecution(exec), "查询成功")
}

// ListExecutions 列表
func (c *SOPController) ListExecutions(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")
	status := ctx.Query("status")
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	list, total, err := c.svc.ListExecutions(ctx.Request.Context(), customerID, status, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	// 这里就地换 map 而不复制切片：这一份 list 是本次请求刚从仓储扫出来的私有数据，
	// 而详情那条要复制副本是因为 service 手上还留着同一个对象（见 redactApprovalToken）。
	for i := range list {
		list[i].ExecutionData = redactApprovalToken(list[i].ExecutionData)
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// approvalTokenMask 执行数据里恢复凭证的对外替换值。
//
// 为什么这批出口必须遮：resume_token 在 approval_requests 上是 `json:"-"`，model 层
// 还有一整条用例钉住"它不出现在任何响应体里"（internal/router/approval_routes_test.go
// 列的第 1 条边界）。但审批桥会把裁决结论**连同凭证**并进 sop_executions.execution_data
// —— 那是卡面要求（流程要能在 checkpoint 里凭它续跑，见 service.ApprovalOutcomeTokenKey
// 与 TestApprovalResumeE2E 里那条 checkpoint 断言）。于是同一个值换了一张表就又从
// /sop/executions* 出去了，而这两个读端点任何登录用户都能读：换表不等于换了信任边界。
//
// 为什么遮成固定串而不是删掉这一格：删掉后"这条流程从没等过审批"与"等过、凭证被遮"
// 在响应里长得一模一样，而运维要问的正好是后者。
const approvalTokenMask = "[redacted]"

// redactApprovalToken 返回遮掉恢复凭证的执行数据。
//
// 没有这一格时原样返回：绝大多数执行记录根本没有审批产物，为它们各建一份 map 是白付的分配。
// 有则**必须复制**：入参可能是 service 手上那份活对象（Execute/Step 返回的就是它），
// 就地改键会把续跑要读的那一格一起改掉，而那是一次跨请求的进程内副作用。
func redactApprovalToken(data model.JSONMap) model.JSONMap {
	if _, ok := data[service.ApprovalOutcomeTokenKey]; !ok {
		return data
	}
	out := make(model.JSONMap, len(data))
	for k, v := range data {
		out[k] = v
	}
	out[service.ApprovalOutcomeTokenKey] = approvalTokenMask
	return out
}

// redactExecution 返回一份遮了凭证的执行记录副本。
func redactExecution(exec *model.SOPExecution) *model.SOPExecution {
	if exec == nil {
		return nil
	}
	out := *exec
	out.ExecutionData = redactApprovalToken(exec.ExecutionData)
	return &out
}

// MatchByIntent 意图匹配
func (c *SOPController) MatchByIntent(ctx *gin.Context) {
	intent := ctx.Query("intent")
	list, err := c.svc.MatchByIntent(ctx.Request.Context(), intent)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, list, "查询成功")
}

// Stats 统计
func (c *SOPController) Stats(ctx *gin.Context) {
	stats, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, stats, "查询成功")
}

// GetABTestStats 查询 SOP 的 A/B 测试 variant 统计
func (c *SOPController) GetABTestStats(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	stats, err := c.svc.GetABTestStats(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, stats, "查询成功")
}

// UpdateABTestConfig 更新 SOP 的 A/B 测试配置
func (c *SOPController) UpdateABTestConfig(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 SOP ID")
		return
	}
	var cfg service.SOPABTestConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agent, err := c.svc.UpdateABTestConfig(ctx.Request.Context(), uint(id), cfg)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, agent, "更新成功")
}
