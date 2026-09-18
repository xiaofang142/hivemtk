package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type WhatsappController struct {
	svc *service.WhatsappService
}

// GetService 返回内部服务，用于其他控制器集成
func (c *WhatsappController) GetService() *service.WhatsappService {
	return c.svc
}

// NewWhatsappController 创建 WhatsApp 控制器
// 修复：移除 controller 越层 new repository，由 service 内部构造
func NewWhatsappController() *WhatsappController {
	return &WhatsappController{svc: service.NewWhatsappService()}
}

// Accounts
func (c *WhatsappController) ListAccounts(ctx *gin.Context) {
	list, err := c.svc.ListAccounts(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取账号列表失败", err.Error())
		return
	}
	response.Success(ctx, list, "获取账号列表成功")
}

type createAccountReq struct {
	Name   string `json:"name"`
	Remark string `json:"remark"`
}

func (c *WhatsappController) CreateAccount(ctx *gin.Context) {
	var req createAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}
	acc, err := c.svc.CreateAccount(ctx.Request.Context(), req.Name, req.Remark)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建账号失败", err.Error())
		return
	}
	response.Success(ctx, acc, "创建账号成功")
}

// Login
func (c *WhatsappController) StartLogin(ctx *gin.Context) {
	idStr := ctx.Param("id")
	accID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "账号ID错误", err.Error())
		return
	}
	qr, err := c.svc.StartLogin(ctx.Request.Context(), accID, 20*time.Second)
	if err != nil {

		if strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "websocket") || strings.Contains(err.Error(), "handshake") {
			response.Error(ctx, 503, "无法连接 WhatsApp 服务器（检查网络/代理后重试）", err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "启动登录失败", err.Error())
		return
	}
	response.Success(ctx, gin.H{"qr": qr}, "登录二维码生成成功")
}

func (c *WhatsappController) LoginStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	accID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "账号ID错误", err.Error())
		return
	}
	loggedIn, err := c.svc.LoginStatus(ctx.Request.Context(), accID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取登录状态失败", err.Error())
		return
	}
	qr, _ := c.svc.GetLoginQR(ctx.Request.Context(), accID)
	response.Success(ctx, gin.H{"logged_in": loggedIn, "qr": qr}, "登录状态获取成功")
}

type createDraftReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (c *WhatsappController) ListDrafts(ctx *gin.Context) {
	list, err := c.svc.ListDrafts(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取草稿失败", err.Error())
		return
	}
	response.Success(ctx, list, "获取草稿成功")
}

func (c *WhatsappController) CreateDraft(ctx *gin.Context) {
	var req createDraftReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}
	d, err := c.svc.CreateDraft(ctx.Request.Context(), req.Title, req.Content)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建草稿失败", err.Error())
		return
	}
	response.Success(ctx, d, "创建草稿成功")
}

type createJobReq struct {
	DraftID string `json:"draft_id"`
}

func (c *WhatsappController) CreateJob(ctx *gin.Context) {
	var req createJobReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}
	dID, err := uuid.Parse(req.DraftID)
	if err != nil {
		response.Error(ctx, 400, "草稿ID错误", err.Error())
		return
	}
	job, err := c.svc.CreateBulkJob(ctx.Request.Context(), dID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建任务失败", err.Error())
		return
	}
	response.Success(ctx, job, "群发任务已创建")
}

// UpdateAccount 更新账号
func (c *WhatsappController) UpdateAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	accID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "账号ID错误", err.Error())
		return
	}
	var req createAccountReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}
	acc, err := c.svc.GetAccount(ctx.Request.Context(), accID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取账号失败", err.Error())
		return
	}
	if acc == nil {
		response.Error(ctx, 404, "账号不存在", "账号不存在")
		return
	}
	acc.Name = req.Name
	acc.Remark = req.Remark
	if err := c.svc.UpdateAccount(ctx.Request.Context(), acc); err != nil {
		response.ErrorFromDB(ctx, err, "更新账号失败", err.Error())
		return
	}
	response.Success(ctx, acc, "更新账号成功")
}

// DeleteAccount 删除账号
func (c *WhatsappController) DeleteAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	accID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "账号ID错误", err.Error())
		return
	}
	if err := c.svc.DeleteAccount(ctx.Request.Context(), accID); err != nil {
		response.ErrorFromDB(ctx, err, "删除账号失败", err.Error())
		return
	}
	response.Success(ctx, nil, "删除账号成功")
}

// UpdateDraft 更新草稿
func (c *WhatsappController) UpdateDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	draftID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "草稿ID错误", err.Error())
		return
	}
	var req createDraftReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "参数错误", err.Error())
		return
	}
	draft, err := c.svc.GetDraft(ctx.Request.Context(), draftID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取草稿失败", err.Error())
		return
	}
	if draft == nil {
		response.Error(ctx, 404, "草稿不存在", "草稿不存在")
		return
	}
	draft.Title = req.Title
	draft.Content = req.Content
	if err := c.svc.UpdateDraft(ctx.Request.Context(), draft); err != nil {
		response.ErrorFromDB(ctx, err, "更新草稿失败", err.Error())
		return
	}
	response.Success(ctx, draft, "更新草稿成功")
}

// DeleteDraft 删除草稿
func (c *WhatsappController) DeleteDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	draftID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "草稿ID错误", err.Error())
		return
	}
	if err := c.svc.DeleteDraft(ctx.Request.Context(), draftID); err != nil {
		response.ErrorFromDB(ctx, err, "删除草稿失败", err.Error())
		return
	}
	response.Success(ctx, nil, "删除草稿成功")
}

// ListJobs 列出群发任务
func (c *WhatsappController) ListJobs(ctx *gin.Context) {
	list, err := c.svc.ListJobs(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取任务列表失败", err.Error())
		return
	}
	response.Success(ctx, list, "获取任务列表成功")
}

// GetJob 获取群发任务详情
func (c *WhatsappController) GetJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "任务ID错误", err.Error())
		return
	}
	job, err := c.svc.GetJob(ctx.Request.Context(), jobID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取任务失败", err.Error())
		return
	}
	if job == nil {
		response.Error(ctx, 404, "任务不存在", "任务不存在")
		return
	}
	response.Success(ctx, job, "获取任务成功")
}

// DeleteJob 删除群发任务
func (c *WhatsappController) DeleteJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(ctx, 400, "任务ID错误", err.Error())
		return
	}
	if err := c.svc.DeleteJob(ctx.Request.Context(), jobID); err != nil {
		response.ErrorFromDB(ctx, err, "删除任务失败", err.Error())
		return
	}
	response.Success(ctx, nil, "删除任务成功")
}
