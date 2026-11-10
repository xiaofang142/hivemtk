package middleware

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// OwnerLookupFunc 查一行数据的归属值。
//
// 三个标识符参数只会来自 model.DataScopeRow（编译期常量表），值本身走占位符绑定；
// 返回 found=false 表示这一行不存在，owner 为归属列的字符串形态（空串/“0” 表示未分配）。
//
// 用注入而不是直接依赖 service 包：middleware 被 router 与多处测试装配，
// 把数据源留在装配层才能让"没接数据源"这件事显式可见（见 SetOwnerLookup 的 nil 语义）。
type OwnerLookupFunc func(ctx context.Context, table, idColumn, ownerColumn, idValue string) (owner string, found bool, err error)

var (
	ownerLookupMu  sync.RWMutex
	ownerLookup    OwnerLookupFunc
	narrowScopeLog sync.Once
)

// SetOwnerLookup 注入归属查询；传 nil 撤掉（撤掉后所有登记口按"查不到归属"拦）。
func SetOwnerLookup(fn OwnerLookupFunc) {
	ownerLookupMu.Lock()
	defer ownerLookupMu.Unlock()
	ownerLookup = fn
}

func currentOwnerLookup() OwnerLookupFunc {
	ownerLookupMu.RLock()
	defer ownerLookupMu.RUnlock()
	return ownerLookup
}

// DataScopeDeniedMessage 行级隔离拒绝文案，测试按它断言拒绝来自这一层而不是别的守卫。
const DataScopeDeniedMessage = "无权访问该数据（它不属于你）"

// dataScopeLookup 登记表读取口，生产恒指向 model.DataScopeFor。
//
// 单独留一个变量而不是直接调用：登记表里当前只有"归属列存 user id + 未分配放行"这一种
// 形状，而判据有另外两档（存 username、未分配不放行）。把它们做成注入点，这两档在写出来
// 的当天就能被测；否则要等一次真实登记才有覆盖，而那时代码已经变了。
var dataScopeLookup = model.DataScopeFor

// DataScopeGuard 行级数据隔离守卫。
//
// 位置：挂在鉴权组上、JWT 之后（要读 user_id/role/data_scope），与能力面守卫相互独立：
// 能力面判"这个角色有没有这个功能的门"，这里判"这一行数据是不是你的"。两道都过才到 handler。
//
// 判据（按顺序，任何一步拒绝都立刻出声）：
//  1. 路由没登记 ⇒ 不属本层职责，放行给后续守卫与 handler；
//  2. 调用者是超管 ⇒ 全量可见；其余角色一律按行判归属；
//  3. 没注入查询源 ⇒ fail-closed（漏装配的后果必须是拦，不是放）；
//  4. 取定位参数：参数缺失说明登记表与路由形状对不上，按装配错拦并出声；
//  5. 查归属：DB 错 ⇒ 500；行不存在 ⇒ 404（不区分"没这行"和"不是你的行"给外人看）；
//  6. 归属为空 ⇒ 未分配池，AllowUnowned 决定放行还是拦；
//  7. 归属与自己不一致 ⇒ 403。
//
// 为什么这里只认角色、不认 data_scope=all：同一批会话接口里有两处**先于本层存在**的行级
// scope（internal/service/customer_session_scope.go 的 SessionScope、
// internal/service/inbox_scope.go 的 InboxScope），它们放开全量的条件都只有"角色是超管"。
// 守卫若额外认 data_scope=all，就会出现"列表看不到别人的会话、按 id 却读得到"——同一域内
// 两套口径，窄的那套被宽的那套旁路。要按部门/团队放宽，得先把
// 成员关系事实源建回来，并让三处消费点同时改（见下）。
//
// data_scope 的 department/team 在本仓**当前无法兑现**：system_users 有 department_id/
// team_id 两列，但成员关系表 team_users 已被单表化重构 DROP（见
// internal/migration/migrations/a_domain_p1_migration.go），没有"谁和谁同部门"的事实源。
// 所以这两种取值按 self 收（向窄处收），并打一条进程级告警——把它当 all 是越权，
// 把它当"未实现所以不管"是静默失效，两者都比收窄更坏。
func DataScopeGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		row, registered := dataScopeLookup(c.Request.Method, c.FullPath())
		if !registered {
			c.Next()
			return
		}

		role := utils.GetRole(c)
		scope := c.GetString("data_scope")
		if role == model.SystemRoleCodeAdmin {
			c.Next()
			return
		}
		if scope == model.DataScopeDepartment || scope == model.DataScopeTeam {
			narrowScopeLog.Do(func() {
				logger.Warnf("行级隔离：data_scope=%s 没有成员关系事实源（team_users 已 DROP），按 self 收口执行",
					scope)
			})
		}

		lookup := currentOwnerLookup()
		if lookup == nil {
			logger.Warnf("行级隔离没拿到归属查询源（装配缺 SetOwnerLookup），按无归属拦 path=%s role=%s",
				c.FullPath(), role)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": http.StatusForbidden, "message": DataScopeDeniedMessage,
			})
			return
		}

		idValue := c.Param(row.Param)
		if idValue == "" {
			logger.Warnf("行级隔离登记表与路由形状不符：path=%s 上没有参数 :%s（装配错，按拦处理）",
				c.FullPath(), row.Param)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": http.StatusForbidden, "message": DataScopeDeniedMessage,
			})
			return
		}

		owner, found, err := lookup(c.Request.Context(), row.Table, row.IDColumn, row.OwnerColumn, idValue)
		if err != nil {
			logger.Warnf("行级隔离查归属失败 table=%s %s=%s: %v", row.Table, row.IDColumn, idValue, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code": http.StatusInternalServerError, "message": "数据权限校验失败",
			})
			return
		}
		if !found {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"code": http.StatusNotFound, "message": "数据不存在",
			})
			return
		}

		unowned := owner == "" || owner == "0"
		if unowned {
			if row.AllowUnowned {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": http.StatusForbidden, "message": DataScopeDeniedMessage,
			})
			return
		}

		var caller string
		if row.OwnerIsUsername {
			caller = utils.GetUsername(c)
		} else {
			caller = strconv.FormatUint(uint64(utils.GetUID(c)), 10)
		}
		if caller == "" || caller == "0" {
			// 行上有归属值却读不到调用者身份：守卫挂在没跑 JWT 的组上，或令牌缺字段。
			// 两种都是装配错，向拒的方向收（401）——把"读不到自己是谁"当成不匹配会给出 403，
			// 当成放行则是整层静默失效。
			logger.Warnf("行级隔离读不到调用者身份 path=%s usernameKind=%v（守卫挂在 JWT 之前？）",
				c.FullPath(), row.OwnerIsUsername)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized, "message": "未授权",
			})
			return
		}
		if caller != owner {
			logger.Warnf("行级隔离拒 path=%s table=%s %s=%s 归属=%s 调用者=%s role=%s",
				c.FullPath(), row.Table, row.IDColumn, idValue, owner, caller, role)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": http.StatusForbidden, "message": DataScopeDeniedMessage,
			})
			return
		}
		c.Next()
	}
}
