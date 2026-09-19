// operator.go 登录态操作者身份的取法（待办动作与审批裁决共用）。
//
// 为什么只有一处：这两个域都要把"这个请求是谁"写进一个 text 列
// （human_tasks.assignee_user_id / approval_requests.decided_by），而两列都是各自
// 统计口径的归人依据（坐席工作量、人工放行率）。两处各写一遍四类型 switch，
// 就会有一处先漏掉 float64（JWT claims 经 JSON 解出来时数字全是 float64），
// 表现是"某个入口偶尔记不下人"——这种偏偶的漏法没有集成测试能稳定抓到。
package controller

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// contextOperatorID 取上下文里的 user id 并翻成十进制字符串。
//
// 返回 ok=false 表示没有可用身份，调用方必须回 401，而不是"以空串处理"：
// 空串在 human_tasks 里等于"没人认领"、在 decided_by 里等于"说不清是谁批的"，
// 一路传到服务层只会拿到一句 400 —— 那是把 401 报错了码（权限问题读成参数问题）。
//
// 类型分支对应令牌的三条来源：JWTAuthMiddleware 写 uint，claims 走 JSON 解出 float64，
// 测试与非标装配可能给 int / 字符串。0 与负数都不算身份（0 是"未填"的库内约定）。
func contextOperatorID(ctx *gin.Context) (string, bool) {
	v, exists := ctx.Get("user_id")
	if !exists {
		return "", false
	}
	switch id := v.(type) {
	case uint:
		if id == 0 {
			return "", false
		}
		return strconv.FormatUint(uint64(id), 10), true
	case int:
		if id <= 0 {
			return "", false
		}
		return strconv.Itoa(id), true
	case float64:
		// permission.go 那一路的 claims 是 JSON 解出来的，数字一律 float64。
		if id < 1 || id != float64(int64(id)) {
			return "", false
		}
		return strconv.FormatInt(int64(id), 10), true
	case string:
		s := strings.TrimSpace(id)
		return s, s != ""
	}
	return "", false
}
