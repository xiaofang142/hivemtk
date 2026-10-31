package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// ConfigParamController 动态阈值参数管理端 CRUD
//
// 权限层级：所有路由都挂在 AdminAuthMiddleware 下（router 层保证）。
// 路由映射见 config_param_routes.go。
type ConfigParamController struct {
	svc *service.ConfigParamService
}

func NewConfigParamController(svc *service.ConfigParamService) *ConfigParamController {
	return &ConfigParamController{svc: svc}
}

// @Summary      查询配置参数列表（可按分组/含已下架）
// @Tags      config-param
// @Router    /api/manage/config-params [get]
// List GET /api/manage/config-params
// 全部参数（分组按 group 字段排序）
//
// include_deprecated=1 时连已下架的旋钮一起返回（查历史值用）。默认不返回：
// 页面上挂着一个改了不生效的旋钮，比没有这个旋钮更糟。
func (c *ConfigParamController) List(ctx *gin.Context) {
	group := ctx.Query("group")
	includeDeprecated := ctx.Query("include_deprecated") == "1"
	var (
		list interface{}
		err  error
	)
	if group != "" {
		list, err = c.svc.ListByGroup(ctx.Request.Context(), group, includeDeprecated)
	} else {
		list, err = c.svc.List(ctx.Request.Context(), includeDeprecated)
	}
	if err != nil {
		logger.Warnf("[ConfigParam] List group=%s err=%v", group, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": list, "message": "ok"})
}

// @Summary      查询配置分组元数据（唯一事实源）
// @Tags      config-param
// @Router    /api/manage/config-params/groups [get]
// ListGroups GET /api/manage/config-params/groups
// 分组元数据（中文名 / 说明 / 排序）。
//
// 前端原来硬编码了一份 29 组的 GROUP_LABELS，那份清单与 param_group 是两份事实：
// 新增一个组前端不知道中文名，页面上就显示 "inbox_sales" 这种英文 key，像出了 bug。
// 这里下发唯一事实源，前端只负责渲染。
func (c *ConfigParamController) ListGroups(ctx *gin.Context) {
	groups, err := c.svc.Groups(ctx.Request.Context())
	if err != nil {
		logger.Warnf("[ConfigParam] ListGroups err=%v", err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	// nil slice 序列化成 null，前端 map/Object.entries 会炸；空集合必须是 []。
	if groups == nil {
		groups = []model.ConfigParamGroup{}
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": groups, "message": "ok"})
}

// @Summary PUT /api/manage/config-params/:group/:key
// @Tags      config-param
// @Router    /api/manage/config-params/{group}/{key} [put]
// Update PUT /api/manage/config-params/:group/:key
// 更新单个参数值（请求体 { "value": <标量> }）
func (c *ConfigParamController) Update(ctx *gin.Context) {
	group := ctx.Param("group")
	key := ctx.Param("key")

	var body struct {
		Value json.RawMessage `json:"value" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	value, err := decodeScalarValue(body.Value)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	actorID := c.currentUserID(ctx)
	if err := c.svc.UpdateValue(ctx.Request.Context(), group, key, value, actorID); err != nil {
		logger.Warnf("[ConfigParam] Update %s.%s err=%v", group, key, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// decodeScalarValue 把请求体里的 JSON 标量折成参数表要存的那个字符串。
//
// 管理端各类型编辑器发出的形状并不统一：el-input-number 发数字（{"value":60}）、
// el-switch 发布尔（{"value":true}）、文本框发字符串。参数表通存字符串，
// 所以绑定层必须按类型折算——原来把它绑成 string 字段，等于让 int/float/duration/bool
// 四类共 212 条参数在页面上一个都改不动，而所有 service 层用例都是直接传字符串调的，
// 谁都测不到（HTTP 层用例 TestUpdateAcceptsEveryScalarJSONShape 钉住这点）。
//
// 数字走 json.Number 取**原始字面量**：折成 float64 再格式化会把 67108864 变成
// "6.7108864e+07"，那是人眼读不懂的值，也是 int 解析不了的字符串。
// 对象/数组/null 一律拒绝——把它们静默转成某种文本，等于把「前端发错了」
// 变成一条看不出问题的库内脏值。
func decodeScalarValue(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("value 不是一段合法的 JSON：%w", err)
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case json.Number:
		return t.String(), nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	default:
		return "", fmt.Errorf("value 只接受字符串、数字或布尔，收到的是 %s", firstTokenOf(raw))
	}
}

// firstTokenOf 从原始 JSON 里取首个非空白字符，用于把「发错了」说成人话
// （对象报 JSON 对象、数组报 JSON 数组），而不是把整段载荷抄回响应里。
func firstTokenOf(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "空值"
	}
	switch s[0] {
	case '{':
		return "一个 JSON 对象"
	case '[':
		return "一个 JSON 数组"
	}
	if s == "null" {
		return "null"
	}
	return "无法识别的 JSON 值"
}

// @Summary      重置单条配置参数为默认值
// @Tags      config-param
// @Router    /api/manage/config-params/{group}/{key}/reset [post]
// ResetToDefault POST /api/manage/config-params/:group/:key/reset
// 重置单条为默认值
func (c *ConfigParamController) ResetToDefault(ctx *gin.Context) {
	group := ctx.Param("group")
	key := ctx.Param("key")
	actorID := c.currentUserID(ctx)
	if err := c.svc.ResetToDefault(ctx.Request.Context(), group, key, actorID); err != nil {
		logger.Warnf("[ConfigParam] ResetToDefault %s.%s err=%v", group, key, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// @Summary      整组重置配置参数为默认值
// @Tags      config-param
// @Router    /api/manage/config-params/{group}/reset [post]
// BulkResetGroup POST /api/manage/config-params/:group/reset
// 整组重置为默认值
func (c *ConfigParamController) BulkResetGroup(ctx *gin.Context) {
	group := ctx.Param("group")
	actorID := c.currentUserID(ctx)
	if err := c.svc.BulkResetGroup(ctx.Request.Context(), group, actorID); err != nil {
		logger.Warnf("[ConfigParam] BulkResetGroup %s err=%v", group, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// @Summary      查询配置参数变更日志（审计回溯）
// @Tags      config-param
// @Router    /api/manage/config-params/audit-logs [get]
// AuditLogs GET /api/manage/config-params/audit-logs?limit=100
// 变更日志（管理端只读，用于审计回溯）
func (c *ConfigParamController) AuditLogs(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	logs, err := c.svc.AuditLogs(ctx.Request.Context(), limit)
	if err != nil {
		logger.Warnf("[ConfigParam] AuditLogs err=%v", err)
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": logs, "message": "ok"})
}

func (c *ConfigParamController) currentUserID(ctx *gin.Context) uint {
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
