package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// DictionaryController 业务字典管理端。
//
// 权限：全部挂在 AdminAuthMiddleware 下（router 层保证）——字典描述的是存量行的
// 值域，改它会让管理端与历史数据的对应关系静默错位，不是普通运维该碰的东西。
type DictionaryController struct {
	svc *service.DictionaryService
}

func NewDictionaryController(svc *service.DictionaryService) *DictionaryController {
	return &DictionaryController{svc: svc}
}

// @Summary      字典服务未装配
// @Tags      dictionary
// @Router    /api/manage/dictionaries [get]
// ListParents GET /api/manage/dictionaries
// 目录：全部字典域。
func (c *DictionaryController) ListParents(ctx *gin.Context) {
	if !c.ready() {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "字典服务未装配"})
		return
	}
	parents, err := c.svc.ListParents(ctx.Request.Context())
	if err != nil {
		logger.Warnf("[Dictionary] ListParents err=%v", err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": parents, "message": "ok"})
}

// @Summary      字典服务未装配
// @Tags      dictionary
// @Router    /api/manage/dictionaries/{parent} [get]
// GetDictionary GET /api/manage/dictionaries/:parent
// 一个域的完整定义（项 + 跃迁）。
func (c *DictionaryController) GetDictionary(ctx *gin.Context) {
	if !c.ready() {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "字典服务未装配"})
		return
	}
	parent := ctx.Param("parent")
	items, trans, err := c.svc.GetDictionary(ctx.Request.Context(), parent)
	if err == service.ErrDictionaryParentUnknown {
		ctx.JSON(http.StatusOK, gin.H{"code": 404, "message": "字典域不存在: " + parent})
		return
	}
	if err != nil {
		logger.Warnf("[Dictionary] GetDictionary %s err=%v", parent, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"parent":      parent,
		"items":       items,
		"transitions": trans,
	}, "message": "ok"})
}

// @Summary      字典服务未装配
// @Tags      dictionary
// @Router    /api/config/dictionaries/{parent} [get]
// PublicDictionary GET /api/config/dictionaries/:parent
// 终端用户读一个域的值域（只读）。
//
// 为什么与管理端的 GetDictionary 分开两条路由而不是共用：管理端那条挂 admin，
// 而终端页面（客服工作台的列表页、筛选下拉）渲染存量行时需要同一份值域。
// 两条返回体完全一致，差别只在鉴权与路径。
func (c *DictionaryController) PublicDictionary(ctx *gin.Context) {
	if !c.ready() {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "字典服务未装配"})
		return
	}
	parent := ctx.Param("parent")
	items, trans, err := c.svc.GetDictionary(ctx.Request.Context(), parent)
	if err == service.ErrDictionaryParentUnknown {
		ctx.JSON(http.StatusOK, gin.H{"code": 404, "message": "字典域不存在: " + parent})
		return
	}
	if err != nil {
		logger.Warnf("[Dictionary] PublicDictionary %s err=%v", parent, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"parent":      parent,
		"items":       items,
		"transitions": trans,
	}, "message": "ok"})
}

// @Summary PUT /api/manage/dictionaries/:parent/items
// @Tags      dictionary
// @Router    /api/manage/dictionaries/{parent}/items [put]
// UpsertItem PUT /api/manage/dictionaries/:parent/items
// 建/改一条字典项。请求体即 model.Dictionary（除 parent_code 从路径取）。
func (c *DictionaryController) UpsertItem(ctx *gin.Context) {
	parent := ctx.Param("parent")
	var body struct {
		Code        string `json:"code" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	if !c.ready() {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "字典服务未装配"})
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	d := &model.Dictionary{
		ParentCode:  parent,
		Code:        body.Code,
		Name:        body.Name,
		Description: body.Description,
		SortOrder:   body.SortOrder,
		Enabled:     enabled,
	}
	if err := c.svc.UpsertItem(ctx.Request.Context(), d, c.currentUserID(ctx)); err == service.ErrDictionarySystemLocked {
		ctx.JSON(http.StatusOK, gin.H{"code": 403,
			"message": "该项由代码种子锁定，不能改名或停用（代码是权威的那一侧）"})
		return
	} else if err != nil {
		logger.Warnf("[Dictionary] UpsertItem %s.%s err=%v", parent, body.Code, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// @Summary PUT /api/manage/dictionaries/:parent/transitions
// @Tags      dictionary
// @Router    /api/manage/dictionaries/{parent}/transitions [put]
// ReplaceTransitions PUT /api/manage/dictionaries/:parent/transitions
// 整体替换一个域的跃迁。请求体 { "transitions": [{from,to,description}] }。
func (c *DictionaryController) ReplaceTransitions(ctx *gin.Context) {
	parent := ctx.Param("parent")
	var body struct {
		Transitions []model.DictionaryTransition `json:"transitions"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	if !c.ready() {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "字典服务未装配"})
		return
	}
	if err := c.svc.ReplaceTransitions(ctx.Request.Context(), parent, body.Transitions, c.currentUserID(ctx)); err != nil {
		logger.Warnf("[Dictionary] ReplaceTransitions %s err=%v", parent, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// currentUserID 与 config_param 控制器同一口径：从 ctx 取 user_id，取不到返回 0
// （0 = 未知操作者，比记一个错的 actor 好）。
// ready 字典服务是否已装配。装配顺序出错（setupDictionaryRoutes 早于 DB 就绪）
// 时返回 500 而不是 panic：字典端点全在管理端，panic 只是让这一次请求失败，
// 而 500 至少告诉调用方"服务端没装好"而不是一个连接被重置。
func (c *DictionaryController) ready() bool { return c != nil && c.svc != nil }

func (c *DictionaryController) currentUserID(ctx *gin.Context) uint {
	if id, exists := ctx.Get("user_id"); exists {
		switch v := id.(type) {
		case uint:
			return v
		case int:
			return uint(v)
		case string:
			n, _ := strconv.Atoi(v)
			return uint(n)
		case float64:
			return uint(v)
		}
	}
	return 0
}
