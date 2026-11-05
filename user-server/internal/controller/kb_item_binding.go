package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// KBItemBindingController 知识库成员绑定端点（D-8 方案 A：kb_item_bindings 成员表）。
type KBItemBindingController struct {
	svc *service.KBItemBindingService
}

func NewKBItemBindingController(svc *service.KBItemBindingService) *KBItemBindingController {
	return &KBItemBindingController{svc: svc}
}

// RegisterRoutes 在 /knowledge-bases 组下注册成员端点（挂在既有 KB 组，不新增顶层域名）。
func (c *KBItemBindingController) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/:id/items", c.List)
	g.POST("/:id/items", c.Bind)
	g.DELETE("/:id/items", c.Unbind)
}

// pathUint 解析 :id 路径参数。
func (c *KBItemBindingController) pathUint(ctx *gin.Context, key string) (uint, bool) {
	v, err := strconv.ParseUint(ctx.Param(key), 10, 64)
	if err != nil || v == 0 {
		response.ValidationError(ctx, "路径参数 "+key+" 必须是正整数")
		return 0, false
	}
	return uint(v), true
}

// tenantID 从请求上下文取租户：与 GORM 隔离 callback 同一来源（tenant.FromContext），
// 缺绑定一律 fail-closed —— 不退化成默认租户 1，否则 on 模式会把操作落到别人库里。
func (c *KBItemBindingController) tenantID(ctx *gin.Context) (uint, bool) {
	id, ok := tenant.FromContext(ctx.Request.Context())
	if !ok {
		response.AuthError(ctx, "缺少租户归属，请重新登录")
		return 0, false
	}
	return id, true
}

// fail 把 service 哨兵错误映射到传输语义：不存在 ⇒ 404，其余 ⇒ 500。
func (c *KBItemBindingController) fail(ctx *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, service.ErrKBNotFound):
		response.Error(ctx, http.StatusNotFound, "知识库不存在")
	case errors.Is(err, service.ErrItemNotFound):
		response.Error(ctx, http.StatusNotFound, "内容不存在或不属于当前租户")
	default:
		response.ErrorFromDB(ctx, err, action+"失败，请稍后重试")
	}
}

// List godoc
// @Summary      知识库成员列表
// @Description  按 item_type 过滤分页列出某知识库绑定的内容（faq|sop|rag），附内容标题
// @Tags         Knowledge Base
// @Produce      json
// @Security     BearerAuth
// @Param        id         path   int     true  "知识库 ID"
// @Param        type       query  string  false "内容类型：faq/sop/rag，空为全部"
// @Param        page       query  int     false "页码（默认 1）"
// @Param        page_size  query  int     false "页大小（默认 50，上限 200）"
// @Success      200  {object}  response.Response  "list + total"
// @Router       /api/knowledge-bases/{id}/items [get]
func (c *KBItemBindingController) List(ctx *gin.Context) {
	kbID, ok := c.pathUint(ctx, "id")
	if !ok {
		return
	}
	tid, ok := c.tenantID(ctx)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	list, total, err := c.svc.ListItems(ctx.Request.Context(), tid, kbID,
		ctx.Query("type"), page, pageSize)
	if err != nil {
		c.fail(ctx, err, "查询知识库成员")
		return
	}
	response.SuccessWithList(ctx, list, total)
}

type bindItemReq struct {
	Items []struct {
		Type   string `json:"type"`
		ItemID uint64 `json:"item_id"`
	} `json:"items"`
}

// Bind godoc
// @Summary      批量绑定内容进知识库
// @Description  把 faq/sop/rag 内容批量绑定到知识库；同租户校验 + 内容存在校验，重复绑定幂等
// @Tags         Knowledge Base
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path int          true "知识库 ID"
// @Param        body  body bindItemReq true "items: [{type, item_id}]"
// @Success      200  {object}  response.Response
// @Router       /api/knowledge-bases/{id}/items [post]
func (c *KBItemBindingController) Bind(ctx *gin.Context) {
	kbID, ok := c.pathUint(ctx, "id")
	if !ok {
		return
	}
	tid, ok := c.tenantID(ctx)
	if !ok {
		return
	}
	var req bindItemReq
	if err := ctx.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		response.ValidationError(ctx, "body 需为 {items:[{type,item_id}]} 且至少一条")
		return
	}
	items := make([]service.BindItem, 0, len(req.Items))
	for i, it := range req.Items {
		if !model.IsValidKBItemType(strings.ToLower(strings.TrimSpace(it.Type))) {
			response.ValidationError(ctx, "第 "+strconv.Itoa(i+1)+" 条 type 只支持 faq|sop|rag，收到 "+it.Type)
			return
		}
		if it.ItemID == 0 {
			response.ValidationError(ctx, "第 "+strconv.Itoa(i+1)+" 条 item_id 必须是正整数")
			return
		}
		items = append(items, service.BindItem{Type: it.Type, ItemID: it.ItemID})
	}
	boundBy := ""
	if v, exists := ctx.Get("username"); exists {
		boundBy, _ = v.(string)
	}
	bound, err := c.svc.BindMany(ctx.Request.Context(), tid, kbID, items, boundBy)
	if err != nil {
		c.fail(ctx, err, "绑定")
		return
	}
	response.Success(ctx, gin.H{"bound": bound}, "绑定成功")
}

// Unbind godoc
// @Summary      解绑知识库成员
// @Description  按查询参数 type+item_id 解绑；未绑定时同样返回成功（幂等）
// @Tags         Knowledge Base
// @Produce      json
// @Security     BearerAuth
// @Param        id       path   int    true "知识库 ID"
// @Param        type     query  string true "内容类型：faq/sop/rag"
// @Param        item_id  query  int    true "内容 ID"
// @Success      200  {object}  response.Response
// @Router       /api/knowledge-bases/{id}/items [delete]
func (c *KBItemBindingController) Unbind(ctx *gin.Context) {
	kbID, ok := c.pathUint(ctx, "id")
	if !ok {
		return
	}
	tid, ok := c.tenantID(ctx)
	if !ok {
		return
	}
	itemType := ctx.Query("type")
	itemID, err := strconv.ParseUint(ctx.Query("item_id"), 10, 64)
	if err != nil || itemID == 0 {
		response.ValidationError(ctx, "item_id 必须是正整数")
		return
	}
	if !model.IsValidKBItemType(strings.ToLower(strings.TrimSpace(itemType))) {
		response.ValidationError(ctx, "type 只支持 faq|sop|rag")
		return
	}
	if err := c.svc.Unbind(ctx.Request.Context(), tid, kbID, itemType, itemID); err != nil {
		c.fail(ctx, err, "解绑")
		return
	}
	response.Success(ctx, gin.H{"unbound": true}, "解绑成功")
}
