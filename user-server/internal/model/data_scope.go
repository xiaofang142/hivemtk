package model

import (
	"regexp"
	"strings"
)

// DataScopeRow 一条"这个接口动的是某一行的数据"的行级隔离登记。
//
// 为什么需要登记表面而不是在每个 handler 里各写一遍判据：同一个资源主键常被十几条
// 子资源接口（备注/转写/优先级/暂缓/CSAT……）各自解析一遍，逐条加检查必然漏，而漏一条
// 就等于这个资源没有隔离。登记成表后执行点只有一处，新增接口只需在此登记一行，
// 且"登记了没有"可被门静态核对（见 internal/router/data_scope_gate_test.go）。
//
// 三条硬约束：
//   - Table / IDColumn / OwnerColumn 必须是编译期常量且过标识符白名单，永不来自请求；
//   - 定位值一律按字符串等值比较：本仓的 id 列有 bigint 也有 text/varchar，
//     在守卫里做数字解析会把 text 主键的资源整片判成"不存在"，而 PostgreSQL 对
//     `WHERE id = '12'` 本就会按列类型隐式转换，字符串比较两边都成立；
//   - 归属列为空（数值 0 或空串）时的语义由 AllowUnowned 显式声明，不靠默认值猜。
type DataScopeRow struct {
	// Table 真实表名（与 TableName() 一致，不是结构体名）。
	Table string
	// IDColumn 这一行的定位列：主键 id，或业务主键（如 session_id）。
	IDColumn string
	// Param URL 路径参数名，其值就是 IDColumn 要匹配的值。
	Param string
	// OwnerColumn 归属列。
	OwnerColumn string
	// OwnerIsUsername true=归属列存 system_users.username（varchar 列常见），
	// false=存 system_users.id（bigint 列）。两类在本仓都存在，猜错会让所有人都读不到。
	OwnerIsUsername bool
	// AllowUnowned 归属为空时是否放行。
	//
	// 待领取池（未分配的会话/任务）必须放行：把池子关掉等于把工作流关掉，
	// 客服再也领不到新会话。但"共享配置"类资源不该享受这条，故逐行声明。
	AllowUnowned bool
	// Note 为什么这一行需要隔离／为什么是这个口径（给下一个读代码的人，不参与判定）。
	Note string
}

// dataScopeIdentifierRe 标识符白名单：登记表里的三个列名/表名会拼进 SQL，
// 即便它们全是编译期常量，也要在装配期自证没有奇怪形状（防手滑，也防门被绕过）。
var dataScopeIdentifierRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{1,62}$`)

// dataScopeRoutes 键为 "METHOD /ginFullPath"。
//
// 只登记"非超管可达 + 按行寻址 + 归属列真能对上 system_users"的接口；
// 归属列对不上登录用户的表（知识文档的 agent_id 是 AI 代理、会话表 user_id 是外部访客）
// 不登记——那条口径一旦被当成归属，结果是所有人都读不到数据。
var dataScopeRoutes = map[string]DataScopeRow{
	// 客服会话的子资源：主入口（列表/详情/消息）走 service 层的 SessionScope，
	// 这些挂在 :id 上的旁路接口此前完全不认调用者，任何人都能用会话 ID 读别人名下
	// 客户的转写、备注，或改优先级、解除人工接管。
	// 这批路由的 :id 是业务主键 session_id（字符串），不是自增 id：
	// controller 把 ctx.Param("id") 原样交给 service，service 按 session_id 查。
	"GET /api/customer-sessions/:id/transcript": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "整段对话转写，读到即是客户隐私全量",
	},
	"GET /api/customer-sessions/:id/internal-notes": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "内部备注只对该会话的处理人可见",
	},
	"POST /api/customer-sessions/:id/internal-notes": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "往别人名下的会话写内部备注",
	},
	"GET /api/customer-sessions/:id/ai-summary": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
	},
	"POST /api/customer-sessions/:id/ai-summary": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "生成会花 LLM 额度，替别人的会话生成等于用别人的配额",
	},
	"GET /api/customer-sessions/:id/edit-lock": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
	},
	"POST /api/customer-sessions/:id/edit-lock": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "抢锁能让别人正在写的回复发不出去",
	},
	"DELETE /api/customer-sessions/:id/edit-lock": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
	},
	"PUT /api/customer-sessions/:id/priority": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "优先级决定排队顺序，改它影响别人的工作量分配",
	},
	"POST /api/customer-sessions/:id/snooze": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "暂缓会把会话从别人的待办里藏掉一段时间",
	},
	"DELETE /api/customer-sessions/:id/snooze": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
	},
	"POST /api/customer-sessions/:id/apply-tag-rule": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
	},
	"POST /api/customer-sessions/:id/csat": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "评分要落到真实处理人头上，代客打分等于篡改别人的绩效",
	},
	"POST /api/customer-sessions/:id/csat/trigger": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "触发会给客户推消息，属于对客户动作",
	},
	// 黑名单这条走的是自增 id：controller 里把参数 ParseUint 后再查。
	"POST /api/customer-sessions/:id/blacklist": {
		Table: "customer_sessions", IDColumn: "id", Param: "id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "拉黑会让客户再也进不了会话",
	},
	"POST /api/inbox/unlock-human/:session_id": {
		Table: "customer_sessions", IDColumn: "session_id", Param: "session_id",
		OwnerColumn: "agent_id", AllowUnowned: true,
		Note: "解除人工接管会把会话还给机器人，动的是别人正在处理的会话",
	},
	// 客户档案（customer 域）按行寻址的那批：2026-11-15 审计 H-1 修复前，这批接口
	// 只挂 JWT，任何 data_scope=self 的坐席都能用客户主键 id 直读其他坐席名下客户的
	// 360 视图与消息全文——同一份数据换 session-id 寻址会被上面那批登记行拦住，
	// 换 customer-id 寻址即绕过。口径与 customer_sessions 一致：
	// owner_agent_id 是客户归属（uint system_users.id），未分配（NULL/0）按未分配池放行。
	// 注意 :id 语义是 customers 表主键（varchar(36)）——同一路径形状上 behaviors/tags
	// 系列的 :id 实际是 sessions.user_id 语义（见 controller 各 handler），它们走
	// service 层 ensureCustomerVisible 断言，不能登记成 customers.id（否则全片误 404）；
	// PUT /api/customer/:id 落点是 legacy user 表（无归属列），已收 AdminAuthMiddleware。
	"GET /api/customer/360/:id": {
		Table: "customers", IDColumn: "id", Param: "id",
		OwnerColumn: "owner_agent_id", AllowUnowned: true,
		Note: "客户 360° 全量视图（含消息历史），读到即是客户隐私全量",
	},
	"GET /api/customer/:id": {
		Table: "customers", IDColumn: "id", Param: "id",
		OwnerColumn: "owner_agent_id", AllowUnowned: true,
	},
	"GET /api/customer/:id/communications": {
		Table: "customers", IDColumn: "id", Param: "id",
		OwnerColumn: "owner_agent_id", AllowUnowned: true,
		Note: "客户沟通记录（消息史）",
	},
	// 收件箱按行寻址的那批（详情/消息/打标/置顶/静音/删除消息）已在 service 层过
	// InboxScope，不再重复登记；这里没有 inbox_conversations 的行不是"漏了"，
	// 是它们已有执行点。登记表与 live 路由的差集由
	// internal/router/data_scope_gate_test.go 单向核对：那条腿把客服域每一条按行寻址的口
	// 分成三类——守卫已登记、名册 dataScopeScopedElsewhere 里写了执行点、两者皆无（红）。
	// 新增这类口时不要动这里来消红，要么登记一行，要么把它的执行点写进那份名册。
}

// DataScopeFor 查一条路由的行级隔离登记。
//
// 用 gin 的 FullPath（带 :param 的注册形状）而不是真实请求路径做键：同一条注册口
// 的所有具体 ID 共享一份判据，键必须与注册形状一致，否则永远查不中。
func DataScopeFor(method, fullPath string) (DataScopeRow, bool) {
	row, ok := dataScopeRoutes[strings.ToUpper(method)+" "+fullPath]
	return row, ok
}

// DataScopeRegistrations 返回全部登记的 "METHOD /path" 键，供门与审计枚举。
func DataScopeRegistrations() []string {
	keys := make([]string, 0, len(dataScopeRoutes))
	for k := range dataScopeRoutes {
		keys = append(keys, k)
	}
	return keys
}

// DataScopeRowOf 按键取登记（门用它逐条核对表/列是否真的存在）。
func DataScopeRowOf(key string) (DataScopeRow, bool) {
	row, ok := dataScopeRoutes[key]
	return row, ok
}

// DataScopeRowByID 按 (表, 定位列, 归属列) 反查登记。
//
// 归属查询源只接受登记表里成对出现的组合：守卫的入参来自路由匹配，路由匹配一旦因为
// 形状漂移拿到意外的三元组，宁可报错也不要把任意列名拼进 SQL。
func DataScopeRowByID(table, idColumn, ownerColumn string) (DataScopeRow, bool) {
	for _, row := range dataScopeRoutes {
		if row.Table == table && row.IDColumn == idColumn && row.OwnerColumn == ownerColumn {
			return row, true
		}
	}
	return DataScopeRow{}, false
}

// DataScopeIdentifiersSafe 自证登记表里的标识符形状全部合法。
//
// 拼进 SQL 的只有这三列，值一律走占位符；这条判据把"常量写歪了"变成装配期就能发现的问题，
// 而不是等一个奇形怪状的 ID 打进来才发现。
func DataScopeIdentifiersSafe() (bad []string) {
	for key, row := range dataScopeRoutes {
		for _, ident := range []string{row.Table, row.IDColumn, row.OwnerColumn} {
			if !dataScopeIdentifierRe.MatchString(ident) {
				bad = append(bad, key+" -> "+ident)
			}
		}
	}
	return bad
}
