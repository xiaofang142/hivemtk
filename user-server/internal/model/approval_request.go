// approval_request.go 异步审批检查点模型（T-P3-01 / N-4，C2 裁定的唯一实体）
//
// C2 的裁定是"只建一套"：A 文档的"同步二次确认"是本表在 auto-approve 快速路径下的
// 退化形式（入队即 approved，调用方原地拿结果），B 文档的"异步审批检查点"是同一条
// 记录处于 pending 的形状。两者共用一行、共用一套状态机，因此这里刻意**没有**
// "同步/异步"这一列 —— 区分只由 status + decided_by 表达（见 ApprovalDecidedByPolicy）。
//
// 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑 GORM AutoMigrate，
// 启动期版本化迁移固定 v1.0.0→v1.0.0 是空跑，同 T-P2-01/T-P2-04/T-P2-05/T-P2-06 四次的实测）。
package model

import "time"

// ApprovalRequest 审批请求行（表 approval_requests）
//
// 列宽取向：取值集合可枚举的列（status/subject_type/policy_key）用定宽，
// 由上游决定的标识（subject_id/decided_by/resume_token）一律 text ——
// 定宽列配不受约束的输入 = 写入硬失败，而**一次写入失败对审批门意味着外发被放行还是
// 被拒**取决于降级方向，这条边界不该由列宽来决定（T-P1-08 在 tool_call_audits 上踩过：
// trace_id 定宽 64 被上游透传的长值撑爆，CreateInBatches 整批回滚）。
type ApprovalRequest struct {
	// ID 业务主键（service 生成，形如 apr_<unixnano>_<seq>）。
	// 用它在列表/审计/日志里指代一条审批；**不**拿它当恢复凭证（见 ResumeToken）。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// SubjectType/SubjectID 被审对象的类型与主键，二元组定位"审的是哪一件事"。
	// 例：(quote, q_123) / (reach_plan, rp_9) / (order_command, oc_77)。
	// 刻意不建外键：被审对象分散在各业务表、且 P4–P7 还会新增类型，
	// 一条指向不定表的外键只能建成"指向某一张表"的假约束。
	SubjectType string `gorm:"type:varchar(32);uniqueIndex:uq_approval_request_open,priority:1,where:status = 'pending'" json:"subject_type"`
	SubjectID   string `gorm:"type:text;uniqueIndex:uq_approval_request_open,priority:2,where:status = 'pending'" json:"subject_id"`

	// PolicyKey 命中的策略（谁要求审的）。它**必须**进幂等键：
	// 同一个对象常常同时要过两道策略（例：报价既要过 quote.send、又要过高折扣档），
	// 只按 subject 去重会让第一道审批顺手把第二道也批了 —— 那是闸门被自己绕过。
	PolicyKey string `gorm:"type:varchar(64);uniqueIndex:uq_approval_request_open,priority:3,where:status = 'pending'" json:"policy_key"`

	Status string `gorm:"type:varchar(16);index" json:"status"`

	// ResumeToken 挂起点恢复凭证：pending 时非空，流程把它写进自己的 checkpoint，
	// 裁决后来凭它续跑（T-P3-02）。与 ID 分开的理由是权限，不是方便：
	// ID 会出现在列表、日志、审计里，凡看得见 ID 的地方都能拿去裁决；
	// 而"能恢复这条流程"是一种能力，必须不可枚举（crypto/rand 32 字节）。
	//
	// 索引是**部分**唯一（resume_token <> ''）：auto-approve 的记录永远不被恢复，
	// 凭证列留空；若唯一索引不带谓词，第二条 auto-approve 记录就会撞在空串上。
	ResumeToken string `gorm:"type:text;uniqueIndex:uq_approval_request_token,priority:1,where:resume_token <> ''" json:"resume_token,omitempty"`

	// ExpiresAt 裁决截止时刻，**只对 pending 有意义**：到期由清扫翻成 expired。
	// 指针而非零值时间：已裁决的行留 NULL，"这条不再被任何清扫触碰"就是可读的事实，
	// 不用先猜一个约定（"零值 = 不过期"还是"零值 = 早就过期了"）。
	ExpiresAt *time.Time `gorm:"index" json:"expires_at,omitempty"`

	// DecidedBy 裁决者：人工 = 操作者 ID；策略自动放行 = ApprovalDecidedByPolicy；
	// TTL 到期 = ApprovalDecidedByTTL。三者都是本包常量，判据见 ApprovalDecidedAutomatically。
	DecidedBy string `gorm:"type:text" json:"decided_by,omitempty"`
	// DecidedAt 裁决落定时刻；NULL = 仍在等人（不用零值时间冒充"没裁决"）。
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecisionNote string     `gorm:"type:text" json:"decision_note,omitempty"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (ApprovalRequest) TableName() string { return "approval_requests" }

// 审批状态值域（四态，与卡面逐字一致）。
//
// 刻意不引 PG ENUM：值域改动不该需要 DROP TYPE（同 sales_events / order_drafts 的口径）。
const (
	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved"
	ApprovalStatusRejected = "rejected"
	ApprovalStatusExpired  = "expired"
)

// ApprovalStatuses 全部合法状态（表驱动测试与越界判据用）。
var ApprovalStatuses = []string{
	ApprovalStatusPending,
	ApprovalStatusApproved,
	ApprovalStatusRejected,
	ApprovalStatusExpired,
}

// ApprovalDecidedStatuses 终态集合：落进这里的行不再被任何裁决或清扫改写。
//
// 与订单草稿的差别要说清：草稿的 confirmed 之所以不算终态是它还会被后续业务改写，
// 而**审批记录的裁决就是它的全部生命周期** —— 批准之后流程往哪跑、跑到哪一步，
// 由 checkpoint 侧负责（C2 明确"游标复用 agent_checkpoints"），不在本表再记一份。
// 若将来给本表加 resumed/fulfilled 之类的状态，等于把同一个事实劈成两处各说各话，
// 于是会出现"审批说已批准、流程根本没人续跑"这种永远查不出来的分歧。
var ApprovalDecidedStatuses = []string{
	ApprovalStatusApproved,
	ApprovalStatusRejected,
	ApprovalStatusExpired,
}

// approvalLegalTransitions 状态机的**唯一**合法跃迁表。
//
// 三条刻意的取舍：
//  1. 三个终态各自都是空集 —— 改判（把 rejected 翻成 approved）在这张表上无路可走。
//     要重审就重新 Submit 一条新记录：被拒的对象修正后**可以**再申请，
//     但必须留下两条记录说明"第一次被拒、第二次才批"，而不是原地覆盖掉第一次的裁决。
//     这是审批作为证据链与草稿作为业务数据的根本差别（草稿可以改状态，裁决不能改历史）。
//  2. 没有 pending→pending：重复提交由幂等路径返回原记录，不走跃迁。
//  3. expired 不可复活：超时就是超时，人工事后补批要新建一条（decided_by 里留得下"事后"）。
var approvalLegalTransitions = map[string]map[string]bool{
	ApprovalStatusPending: {
		ApprovalStatusApproved: true,
		ApprovalStatusRejected: true,
		ApprovalStatusExpired:  true,
	},
	ApprovalStatusApproved: {},
	ApprovalStatusRejected: {},
	ApprovalStatusExpired:  {},
}

// ApprovalTransitionAllowed 判定一次状态跃迁是否合法。
//
// 未知状态一律 false（含两端任一为空串）：判定表里没有路 = 拒，而不是"默认放行"。
// 审批门的判据出错时方向必须是"拦下来"，否则一个拼错的常量名就能把一条已批准记录
// 再批一次、或把已拒的翻成已批。
func ApprovalTransitionAllowed(from, to string) bool {
	targets, ok := approvalLegalTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// ApprovalTransitionTargets 返回 from 可去的状态集合（副本，防调用方 sort 改掉全进程词表）。
// 未知状态返回空集而非 nil：端点/报告直接渲染时不必再判 nil。
func ApprovalTransitionTargets(from string) []string {
	targets := approvalLegalTransitions[from]
	out := make([]string, 0, len(targets))
	for _, s := range ApprovalStatuses { // 按 ApprovalStatuses 的顺序输出，集合遍历不定序会让快照不可比
		if targets[s] {
			out = append(out, s)
		}
	}
	return out
}

// 裁决来源（decided_by 的三个取值）。写成常量而不是散落的字面量：
// 判据「这次批准是人给的还是策略给的」要靠这个字段回答，散字符串迟早漂成
// "policy:auto"/"auto_policy"/"系统" 三个版本，届时统计里的自动放行率就没人敢信。
const (
	// ApprovalDecidedByPolicy auto-approve 快速路径（策略当场放行，见 C2 同步退化态）。
	ApprovalDecidedByPolicy = "policy:auto"
	// ApprovalDecidedByTTL pending 超期未裁决，由清扫落终态。**不是**人工拒绝，
	// 所以单独一个值：把超时混进"人拒了"会让拒绝率虚高、且看不到"审批没人处理"这个真问题。
	ApprovalDecidedByTTL = "system:ttl"
)

// ApprovalDecidedAutomatically 报告这条记录是否**不是**人做的裁决。
// 精确等值比较（不是 HasPrefix）：前缀匹配会把人工账号 "policy:alice" 读成策略放行。
func ApprovalDecidedAutomatically(decidedBy string) bool {
	return decidedBy == ApprovalDecidedByPolicy || decidedBy == ApprovalDecidedByTTL
}
