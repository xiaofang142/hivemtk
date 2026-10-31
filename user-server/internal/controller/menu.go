package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// MenuController 菜单权限控制器
type MenuController struct {
	svc *service.MenuService
}

// NewMenuController 构造
func NewMenuController() *MenuController {
	return &MenuController{svc: service.NewMenuService()}
}

// @Summary GET /api/menus
// @Tags      menu
// @Router    /api/menus [get]
// GetMyMenus GET /api/menus
//
// 当前登录用户可见菜单树（JWT 中 role 决定），供前端渲染导航与路由守卫。
func (ctrl *MenuController) GetMyMenus(c *gin.Context) {
	role := c.GetString("role")
	if role == "" {
		response.Error(c, http.StatusUnauthorized, "未获取到角色信息")
		return
	}
	tree, err := ctrl.svc.GetUserMenuTree(c.Request.Context(), role)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"tree": tree, "all": role == "admin"}, "获取菜单成功")
}

// @Summary GET /api/system/menus
// @Tags      menu
// @Router    /api/system/menus [get]
// AdminGetAllMenus GET /api/system/menus
//
// 完整菜单注册表（管理端分配界面 el-tree 数据源）。
func (ctrl *MenuController) AdminGetAllMenus(c *gin.Context) {
	response.Success(c, gin.H{"tree": ctrl.svc.GetAllMenuTree()}, "获取菜单注册表成功")
}

// @Summary GET /api/system/roles/:code/menus
// @Tags      menu
// @Router    /api/system/roles/{code}/menus [get]
// AdminGetRoleMenus GET /api/system/roles/:code/menus
//
// 某角色已分配的菜单 key 列表。
func (ctrl *MenuController) AdminGetRoleMenus(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		response.Error(c, http.StatusBadRequest, "角色 code 不能为空")
		return
	}
	keys, err := ctrl.svc.GetRoleMenuKeys(c.Request.Context(), code)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"menu_keys": keys}, "获取角色菜单权限成功")
}

// @Summary PUT /api/system/roles/:code/menus
// @Tags      menu
// @Router    /api/system/roles/{code}/menus [put]
// AdminUpdateRoleMenus PUT /api/system/roles/:code/menus
//
// 覆盖式保存某角色的菜单分配。Body: {"menu_keys": ["key1", ...]}
//
// `[]` 是合法值（＝清空该角色的全部菜单，service 侧口径），所以这里不能用
// binding:"required"：go-playground 对切片把"零长度"也判成缺失，那样清空这条路
// 在本接口上根本表达不出来，而能力面守卫的放行判据就挂在这份授予上。
// 缺字段与空数组的区分用 nil 判：JSON 里没写这个键 ⇒ nil ⇒ 400。
func (ctrl *MenuController) AdminUpdateRoleMenus(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		response.Error(c, http.StatusBadRequest, "角色 code 不能为空")
		return
	}
	var req struct {
		MenuKeys *[]string `json:"menu_keys"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误：请求体应为 {\"menu_keys\": [...]}")
		return
	}
	if req.MenuKeys == nil {
		response.Error(c, http.StatusBadRequest, "参数错误：menu_keys 必填（清空请传空数组）")
		return
	}
	if err := ctrl.svc.UpdateRoleMenuKeys(c.Request.Context(), code, *req.MenuKeys); err != nil {
		writeServiceError(c, err)
		return
	}
	// 能力面守卫按这份授予决定接口档位：不撤缓存的话，改完分配最长要等一个 TTL 才生效。
	middleware.InvalidateMenuGrants(code)
	response.Success(c, nil, "保存角色菜单权限成功")
}
