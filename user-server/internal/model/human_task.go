// human_task.go 统一待办模型（新规划任务清单 T-P3-03 / N-9）。
//
// C3 的裁定是「统一数据模型 + 分离视图」：A 文档的人工 = 接收**会话所有权**的客服坐席，
// B 文档的人工 = 接收**流程卡点**的销售/审批人，两边都要"有一件事在等人做"，但
// —— 状态机不同（会话可抢占、可释放；审批不可抢占，只能裁决）、指标不同
// （响应时长 vs 卡点通过率）。合并视图会把"等报价审批 3 天"算进坐席响应时长，
// 污染 CS-32；分开建模则必然出现两套待办表与两处未读数。
//
// 所以本表是**一张表三类待办**：共用一套状态机（humanTaskActionTargets 是唯一真源），
// 三类之间的差异用两张小表表达 —— 哪个动作允许哪类（humanTaskActionKinds）、
// 哪类用哪个 SLA 列（HumanTaskSLAField）。差异写成数据而不是写成 if，
// 是为了"三类共用一套状态机"这句话本身可测（见 model 层的表驱动用例）。
//
// 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑 GORM AutoMigrate，
// 启动期版本化迁移固定 v1.0.0→v1.0.0 是空跑，同 T-P3-01/T-P3-02 的实测）。
package model

import (
	"strings"
	"time"
)

// HumanTask 人工待办行（表 human_tasks）。
//
// 这一行的职责边界要说清楚：它是**"有一件事在等人"这条事实的索引**，不是业务对象本身。
// 会话还是那个会话（customer_sessions）、审批还是那条审批（approval_requests）、
// 催收升级还是那张单（P7 的表）—— 本表只存"谁该处理、处理到哪一步、什么时候算慢"，
// 靠 (subject_type, subject_id) 指回去，**不复制**业务字段。
// 复制一份的理由与不复制的理由是同一个：待办中心要能一行读出标题；
// 但把金额、状态、客户名抄进来，就会有两份会各自漂移的副本（T-P3-02 在 TTL 上
// 已经为这个形状付过一次代价：两处各写一份到期时刻，先骗到的是流程）。
type HumanTask struct {
	// ID 业务主键（service 生成，形如 ht_<unixnano>_<seq>），列表/审计/日志里指代一条待办。
	// 与 approval_requests.id 同一取向：不自增，免得跨库搬迁与导出时对不上号。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// Kind 待办类型，值域见下面三个常量（三类之外一律拒）。
	Kind string `gorm:"type:varchar(32);index;not null" json:"kind"`

	// Status 状态机位置，值域见 HumanTaskStatuses。
	Status string `gorm:"type:varchar(16);index;not null" json:"status"`

	// SubjectType/SubjectID 这件事**关于**哪条业务记录，二元组定位。
	// 例：(customer_session, sess_8f2…) / (approval_request, apr_…) / (collection_case, cc_…)。
	// 与 approval_requests 同理刻意不建外键：指向不定表的外键只能建成假约束。
	//
	// 这两列同时是"同一件事不许投递两次"的键（部分唯一索引 uq_human_task_open，
	// 谓词 = 仍未处理）。开放态而不是 pending 单值进谓词是有讲究的：坐席已认领的待办
	// 对系统而言仍然"没处理完"，此时第二次转人工若再建一行，就会出现两行同时指着一个
	// 会话、各被认领一次（AC② 的"不重复投递"就是这么破的）。
	SubjectType string `gorm:"type:varchar(32);uniqueIndex:uq_human_task_open,priority:1,where:status <> 'done' AND status <> 'cancelled'" json:"subject_type"`
	SubjectID   string `gorm:"type:text;uniqueIndex:uq_human_task_open,priority:2,where:status <> 'done' AND status <> 'cancelled'" json:"subject_id"`

	// Title 给人看的那一行字（待办中心列表的标题列）。text 且允许空：
	// 它是展示串不是标识，截断一条中文标题比存不下它好处理得多。
	Title string `gorm:"type:text" json:"title,omitempty"`
	// Reason 为什么要人工（转人工原因 / 触发策略 / 升级缘由）。原样存上游给的文本，
	// 不做归一：归一是幂等键的事，Reason 只负责"事后看得懂"。
	Reason string `gorm:"type:text" json:"reason,omitempty"`
	// PayloadRef 处理时要去看的那个位置的只读引用（例如前端路由片段）。
	// 存引用不存内容，理由与上面"不复制业务字段"一致。
	PayloadRef string `gorm:"type:text" json:"payload_ref,omitempty"`

	// OneID 关联到归一后的客户身份（C4 表里"跨域关联 = oneid"那一行）。
	// 它是本表**唯一**一个跨三类都要带的业务标识：待办中心要按客户聚合待办，
	// 而三类各自的表里 one_id 的列名与类型都不一样。可空 = 归一还没跑出来。
	OneID string `gorm:"type:varchar(100);index" json:"one_id,omitempty"`

	// AssigneeUserID 当前在处理这件事的人。claimed 由认领写入、done 由完成者写入。
	// text 而非定宽：写入方是登录态里的 user id / 工号，形态由上游决定。
	AssigneeUserID string `gorm:"type:text" json:"assignee_user_id,omitempty"`

	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	// CancelReason 为什么撤销（"会话已由他人回复" / "审批改由策略放行"）。
	// 与 approval_requests.decision_note 同一取向：撤销是有原因的动作，无原因不可撤。
	CancelReason string `gorm:"type:text" json:"cancel_reason,omitempty"`

	// —— SLA 列：三类各一列、互斥（AC①「SLA 字段独立」的落点）。
	//
	// 为什么不是一个 sla_due_at 通吃：坐席档以**分钟**计（首响），审批档以**小时/天**计
	// （卡点裁决），催收档以**天**计（升级）。一列两义 = 同一个数字在两处单位不同，
	// 正是 C3 说的污染。分成三列之后，"这条待办的 SLA 是哪一档"由 kind 唯一决定，
	// 填错列在写入口就被拒（见 HumanTaskSLAUnsupportedFields 与 service 的守卫）。
	//
	// 三列都是指针 + 可空：未填 = 这一档不适用，而不是"截止于零值时刻"（早就逾期）
	// 或"永不过期"（无截止）—— 后两种读法都得先猜一个约定，而 SLA 判逾期是直接进指标的。

	// SlaFirstResponseAt 仅 conversation_handoff：首响截止时刻。
	SlaFirstResponseAt *time.Time `gorm:"index" json:"sla_first_response_at,omitempty"`
	// SlaDecideAt 仅 approval：裁决截止时刻。**由调用方从 approval_requests.expires_at
	// 抄来**（同一事实源），本表不自带 TTL 配置 —— 两处各写一份 TTL，先漂移的是指标。
	SlaDecideAt *time.Time `gorm:"index" json:"sla_decide_at,omitempty"`
	// SlaEscalateAt 仅 collection_escalation：升级处理截止时刻（P7 的催收卡给值）。
	SlaEscalateAt *time.Time `gorm:"index" json:"sla_escalate_at,omitempty"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (HumanTask) TableName() string { return "human_tasks" }

// 待办类型值域（三类，与 C3 裁定逐字一致）。
//
// 刻意不引 PG ENUM：加一类不该需要 DROP TYPE（同 approval_requests.status 的口径）。
const (
	HumanTaskKindConversationHandoff  = "conversation_handoff"
	HumanTaskKindApproval             = "approval"
	HumanTaskKindCollectionEscalation = "collection_escalation"
)

// HumanTaskKinds 全部合法类型（表驱动测试、API 入参校验、越界判据用）。
// 顺序即待办中心默认展示顺序：会话（分钟级，最急）→ 审批 → 催收升级。
var HumanTaskKinds = []string{
	HumanTaskKindConversationHandoff,
	HumanTaskKindApproval,
	HumanTaskKindCollectionEscalation,
}

// 状态值域（四态）。
//
// 刻意**没有** expired：逾期不是终态。到期未处理仍是一条要人做的待办，
// 把它翻成终态会让它从"待处理"列表里消失，而底下那件事（会话没人接、审批没人裁）
// 一件都没少 —— 逾期的正确表达是 sla_* 列上的时刻已过去（可算可读的读数），
// 不是一个状态。审批那条 TTL 到期是 approval_requests 自己的事（status=expired），
// 由 T-P3-02 的清扫落，本表不重复落一遍。
const (
	HumanTaskStatusPending   = "pending"
	HumanTaskStatusClaimed   = "claimed"
	HumanTaskStatusDone      = "done"
	HumanTaskStatusCancelled = "cancelled"
)

// HumanTaskStatuses 全部合法状态。
var HumanTaskStatuses = []string{
	HumanTaskStatusPending,
	HumanTaskStatusClaimed,
	HumanTaskStatusDone,
	HumanTaskStatusCancelled,
}

// HumanTaskOpenStatuses 开放态（仍占着幂等键、仍出现在待办列表里、仍进未读聚合）。
var HumanTaskOpenStatuses = []string{
	HumanTaskStatusPending,
	HumanTaskStatusClaimed,
}

// HumanTaskTerminalStatuses 终态：落进这里的事已经不需要人再做了。
//
// 它与开放态是**同一次划分的两侧**（用例逐状态比对两者对同一集合的判定），
// 之所以两侧都要写出来，是因为下面那条 SQL 谓词只能用这一侧表达。
var HumanTaskTerminalStatuses = []string{
	HumanTaskStatusDone,
	HumanTaskStatusCancelled,
}

// HumanTaskOpenPredicateSQL 部分唯一索引 uq_human_task_open 的 WHERE 谓词。
//
// 写成「不是终态」而不是「IN 那两个开放值」，有两个理由，第二个是硬的：
//  1. 方向。将来加第五个状态时，IN 写法会让新状态的行**不受约束**（于是同一件事可以
//     重复投递而不报错），而终态写法让它默认受约束 —— 未分类的事偏向"仍占坑、仍看得见"，
//     这与待办池的用途一致（宁可多看见一条，不可静默少一条）。
//  2. 可行性。GORM 按逗号切 tag，`where:status IN ('pending','claimed')` 会被劈成
//     `where:status IN ('pending'` + 一个无意义的片段，AutoMigrate 直接 42601
//     syntax error（本卡实测：建表阶段就炸，12 条仓储用例全红在同一句上）。
//     含逗号的谓词进不了 tag，与写法优劣无关。
func HumanTaskOpenPredicateSQL() string {
	clauses := make([]string, 0, len(HumanTaskTerminalStatuses))
	for _, s := range HumanTaskTerminalStatuses {
		clauses = append(clauses, "status <> '"+s+"'")
	}
	return strings.Join(clauses, " AND ")
}

// HumanTaskIsOpen 报告这条待办是否仍未处理完。未知状态（含空串）判 false：
// Go 侧的判定偏向"不算一条待办"，与索引侧偏向"仍受约束"是两回事 ——
// 前者出现在读数与列表里（未分类的状态不该混进指标），后者管的是写入约束（少拦比多拦坏）。
func HumanTaskIsOpen(status string) bool {
	for _, s := range HumanTaskOpenStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// 动作值域（对外的四个动词）。用"动作"而不是"目标状态"作接口面：
// 调用方想表达的是"我要认领它"，而不是"我要把它变成 claimed"——
// 后者会把状态机的形状泄漏给每一个调用点，加一个状态要改一片调用方。
const (
	HumanTaskActionClaim    = "claim"
	HumanTaskActionRelease  = "release"
	HumanTaskActionComplete = "complete"
	HumanTaskActionCancel   = "cancel"
)

// HumanTaskActions 全部动作（表驱动测试与"每个动作都被测到"的用例用）。
var HumanTaskActions = []string{
	HumanTaskActionClaim,
	HumanTaskActionRelease,
	HumanTaskActionComplete,
	HumanTaskActionCancel,
}

// humanTaskActionTargets 共用状态机的**唯一**合法跃迁表：from --action--> to。
//
// 三条刻意的取舍：
//  1. 两个终态（done/cancelled）出边为空 —— 处理完的待办不许原地复活。
//     同一件事再次需要人工，是一条**新**待办（新的 ID、新的 created_at），
//     这样"这个会话被转过两次人工"才看得见；复用旧行等于把次数抹成一。
//  2. claimed→pending（release）存在而 pending→pending 不存在：释放是"退回池子"，
//     不是"再认领一次"。认领同一件事两次的正确回答由仓储的 CAS 给（只有一个赢家）。
//  3. 没有 claim→claim、也没有 done→cancel：撤销只针对还开着的待办。
var humanTaskActionTargets = map[string]map[string]string{
	HumanTaskStatusPending: {
		HumanTaskActionClaim:    HumanTaskStatusClaimed,
		HumanTaskActionComplete: HumanTaskStatusDone,
		HumanTaskActionCancel:   HumanTaskStatusCancelled,
	},
	HumanTaskStatusClaimed: {
		HumanTaskActionRelease:  HumanTaskStatusPending,
		HumanTaskActionComplete: HumanTaskStatusDone,
		HumanTaskActionCancel:   HumanTaskStatusCancelled,
	},
	HumanTaskStatusDone:      {},
	HumanTaskStatusCancelled: {},
}

// humanTaskActionKinds 哪个动作允许用在哪类待办上（C3 那句"状态机不同"的落点）。
//
// claim / release 只对 conversation_handoff 开：会话所有权可以抢、可以退，
// 而**审批不可抢占只能裁决**（C3 原文），把"认领"开放给审批类待办会造出第二种批法 ——
// "我认领了所以只有我能批"，而 approval_requests 那边根本不认这件事。
// 催收升级同理：它是一张单据，处理它的人由裁决动作留下，不靠抢。
//
// complete / cancel 三类都开：都表示"这件事不用再等人了"。
var humanTaskActionKinds = map[string]map[string]bool{
	HumanTaskActionClaim: {
		HumanTaskKindConversationHandoff: true,
	},
	HumanTaskActionRelease: {
		HumanTaskKindConversationHandoff: true,
	},
	HumanTaskActionComplete: {
		HumanTaskKindConversationHandoff:  true,
		HumanTaskKindApproval:             true,
		HumanTaskKindCollectionEscalation: true,
	},
	HumanTaskActionCancel: {
		HumanTaskKindConversationHandoff:  true,
		HumanTaskKindApproval:             true,
		HumanTaskKindCollectionEscalation: true,
	},
}

// HumanTaskSLAColumns 三档 SLA 列的库列名，顺序固定（同一次输入的两次判读必须给出同一份清单）。
var HumanTaskSLAColumns = []string{"sla_first_response_at", "sla_decide_at", "sla_escalate_at"}

// humanTaskSLAFilledAt 列名 → "这条待办的这一列填了没有"。
// 写成表而不是在三处各写一个 if：加第四档时只有一张表要动，
// 漏了表漏了 switch 都会撞用例（见 model 测试的逐类断言）。
var humanTaskSLAFilledAt = map[string]func(*HumanTask) bool{
	"sla_first_response_at": func(t *HumanTask) bool { return t.SlaFirstResponseAt != nil },
	"sla_decide_at":         func(t *HumanTask) bool { return t.SlaDecideAt != nil },
	"sla_escalate_at":       func(t *HumanTask) bool { return t.SlaEscalateAt != nil },
}

// HumanTaskTransitionAllowed 判定 (kind, from) 上执行 action 是否合法，合法时给出目标态。
//
// 两道判据按"差异是数据"的顺序串起来：先问这类待办准不准做这个动作（humanTaskActionKinds），
// 再问这台状态机在此刻有没有这条路（humanTaskActionTargets）。
// 未知 kind / 未知 from / 未知 action 一律拒（to 空、ok false）：判定表里没有这条路
// 就是"不许"，而不是"默认放行"（与审批跃迁同一方向）—— 一个拼错的常量名若被读成放行，
// 就能把已经处理完的待办原地再处理一次，而"我处理过几件"是这张表要回答的问题。
func HumanTaskTransitionAllowed(kind, from, action string) (to string, ok bool) {
	if !humanTaskActionKinds[action][kind] {
		return "", false
	}
	targets, exists := humanTaskActionTargets[from]
	if !exists {
		return "", false
	}
	to, ok = targets[action]
	if !ok {
		return "", false
	}
	return to, true
}

// HumanTaskSLAField 返回该 kind 唯一可填的 SLA 列的库列名；未知 kind 返回空串。
// 空串在下面的判里读成"哪一列都不属于它"，于是任何已填列都是越界 —— 未知类别
// 不该有一条能填上的 SLA。
func HumanTaskSLAField(kind string) string {
	switch kind {
	case HumanTaskKindConversationHandoff:
		return "sla_first_response_at"
	case HumanTaskKindApproval:
		return "sla_decide_at"
	case HumanTaskKindCollectionEscalation:
		return "sla_escalate_at"
	}
	return ""
}

// HumanTaskSLAUnsupportedFields 列出这条待办上**不属于自己 kind** 的已填 SLA 列。
// 返回空切片（不是 nil）= 合规：调用方与端点直接看长度，不必每次判 nil。
//
// 写成包级函数而不是 `*HumanTask` 的方法：五层架构门禁止 model 携带业务方法
// （只许 TableName 与 GORM Hook，见 scripts/check-architecture.sh 第 4 项）——
// 这条判据本身就是"哪一列属于哪个 kind"的业务规则，摆进 model 会让门对下一个
// 往 model 上加方法的人失效。
func HumanTaskSLAUnsupportedFields(t *HumanTask) []string {
	own := HumanTaskSLAField(t.Kind)
	out := make([]string, 0, len(HumanTaskSLAColumns))
	for _, col := range HumanTaskSLAColumns {
		filled := humanTaskSLAFilledAt[col]
		if col != own && filled != nil && filled(t) {
			out = append(out, col)
		}
	}
	return out
}

// HumanTaskKindKnown 报告 kind 是否在三个已知值里（入参校验用；未知 kind 判 false 而不是 panic）。
func HumanTaskKindKnown(kind string) bool {
	for _, k := range HumanTaskKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// HumanTaskStatusKnown 报告 status 是否在四个已知值里。
func HumanTaskStatusKnown(status string) bool {
	for _, s := range HumanTaskStatuses {
		if s == status {
			return true
		}
	}
	return false
}
