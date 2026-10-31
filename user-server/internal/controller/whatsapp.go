package controller

import (
	"errors"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
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

// NewWhatsappController 创建 WhatsApp 控制器（服务由装配点注入）
func NewWhatsappController(svc *service.WhatsappService) *WhatsappController {
	return &WhatsappController{svc: svc}
}

// @Summary GET /api/whatsapp/accounts
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts [get]
// Accounts
func (c *WhatsappController) ListAccounts(ctx *gin.Context) {
	list, err := c.svc.ListAccounts(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取账号列表失败", err.Error())
		return
	}
	// 出参脱敏：token / app_secret 是企业微信侧的接入**密钥**，
	// 直接序列化 ⇒ 任何登录用户都能把商户的接入凭证整个读走。
	// （app_id 是业务标识不是密钥，与 whatsapp-cloud 保留 whatsapp_business_id
	//   明文的口径一致，不在这里掩。）
	// 兄弟模块 whatsapp-cloud 早就这么做了（access_token_masked / app_secret_masked，
	// 见 whatsapp_cloud_account.go 的 maskFeishuSecret），这条是漏的。
	//
	// 为什么可以就地脱敏、且不需要"写侧认掩码"那一步：
	// 凭证是**扫码登录流程**写进去的（CreateAccount 只收 name/remark，
	// UpdateAccount 也只改 name/remark），前端既不读它也不回传它 ⇒
	// 不存在"把掩码原样提交回来覆盖真值"的路径（对比企业微信 / 短信配置那两处）。
	maskWhatsappAccountSecrets(list)
	response.Success(ctx, list, "获取账号列表成功")
}

// maskWhatsappAccountSecrets 就地把接入密钥换成掩码。
//
// 掩码格式与 whatsapp-cloud 一致（maskFeishuSecret）：前 4 位 + **** + 后 4 位，
// 够运维认是哪一把钥匙，又不泄密。空串保持空（表示"还没配"）。
//
// 只掩密钥（token / app_secret），不掩业务标识（app_id）——
// 与 whatsapp-cloud 保留 whatsapp_business_id 明文的口径一致。
func maskWhatsappAccountSecrets(list []*model.WhatsappAccount) {
	for _, acc := range list {
		if acc == nil {
			continue
		}
		acc.Token = maskFeishuSecret(acc.Token)
		acc.AppSecret = maskFeishuSecret(acc.AppSecret)
	}
}

type createAccountReq struct {
	Name   string `json:"name"`
	Remark string `json:"remark"`
}

// @Summary POST /api/whatsapp/accounts
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts [post]
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
	// 回显同样脱敏（账号模型带着 token / app_secret 字段）。
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{acc})
	response.Success(ctx, acc, "创建账号成功")
}

// @Summary POST /api/whatsapp/accounts/{id}/login/start
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts/{id}/login/start [post]
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
		// 个人版扫码登录已下线（W-3）：410 让老前端拿到"搬家了"，而不是 500 服务故障。
		if errors.Is(err, service.ErrWhatsAppProviderUnsupported) {
			response.ErrorWithCode(ctx, utils.ErrorCodeWhatsappLoginRetired, http.StatusGone, err.Error())
			return
		}

		if strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "websocket") || strings.Contains(err.Error(), "handshake") {
			response.Error(ctx, 503, "无法连接 WhatsApp 服务器（检查网络/代理后重试）", err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "启动登录失败", err.Error())
		return
	}
	response.Success(ctx, gin.H{"qr": qr}, "登录二维码生成成功")
}

// @Summary GET /api/whatsapp/accounts/{id}/login/status
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts/{id}/login/status [get]
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

// @Summary GET /api/whatsapp/drafts
// @Tags      whatsapp
// @Router    /api/whatsapp/drafts [get]
// ListDrafts 分页口径与前端 page-sizes [10,20,50] 对齐：缺省 10、上限 50，
// 返回 {list,total} 形态（前端 load() 双形态兼容已就位，S-31）。
func (c *WhatsappController) ListDrafts(ctx *gin.Context) {
	page, pageSize := normalizeWhatsappPaging(ctx)
	list, total, err := c.svc.ListDrafts(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取草稿失败")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "获取草稿成功")
}

// normalizeWhatsappPaging 归一化分页入参：page<1→1、page_size 缺省 10、封顶 50。
func normalizeWhatsappPaging(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

// @Summary POST /api/whatsapp/drafts
// @Tags      whatsapp
// @Router    /api/whatsapp/drafts [post]
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

// @Summary      群发任务已创建
// @Tags      whatsapp
// @Router    /api/whatsapp/jobs [post]
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

// @Summary 更新账号
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts/{id} [put]
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
	// 回显同样脱敏（下面这条 account 带着 token / app_secret）。
	maskWhatsappAccountSecrets([]*model.WhatsappAccount{acc})
	response.Success(ctx, acc, "更新账号成功")
}

// @Summary 删除账号
// @Tags      whatsapp
// @Router    /api/whatsapp/accounts/{id} [delete]
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

// @Summary 更新草稿
// @Tags      whatsapp
// @Router    /api/whatsapp/drafts/{id} [put]
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

// @Summary 删除草稿
// @Tags      whatsapp
// @Router    /api/whatsapp/drafts/{id} [delete]
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

// @Summary 列出群发任务
// @Tags      whatsapp
// @Router    /api/whatsapp/jobs [get]
// ListJobs 列出群发任务（分页口径同 ListDrafts，S-31）
func (c *WhatsappController) ListJobs(ctx *gin.Context) {
	page, pageSize := normalizeWhatsappPaging(ctx)
	list, total, err := c.svc.ListJobs(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取任务列表失败")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "获取任务列表成功")
}

// @Summary 获取群发任务详情
// @Tags      whatsapp
// @Router    /api/whatsapp/jobs/{id} [get]
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

// @Summary 删除群发任务
// @Tags      whatsapp
// @Router    /api/whatsapp/jobs/{id} [delete]
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
