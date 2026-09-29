// kb_release.go 知识库变更流程的三张表（新规划 T-P9-02 / G-5，CS-74 政策变更同步）。
//
// 一句话口径：**变更要生效，必须先过审批、再过一次"发布"**。审批答"这条改动准不准"
// （唯一事实源仍是 approval_requests，本文件不复制它的结论），发布答"这批改动今天
// 上不上线"，而上线与否只由 kb_releases.effective_version 这一个指针决定 ——
// 回滚因此是指针回拨，一个字节都不碰 knowledge_chunks（AC②）。
//
// 为什么"落库"挂在发布而不是挂在裁决上（二次审核时否掉的另一版设计）：
//   - 裁决即落库要配一套"审批完成 → 叫醒落库方"的推送（ApprovalRequestService 的
//     notifier 只有一个槽，已被 SOP 恢复桥占用，见 approval_runtime_wiring.go:198），
//     而一条 KB 变更晚十分钟落库没有任何代价 —— 没有人在等它；
//   - 更要命的是那版会让"回滚后再发布"把上次回滚掉的内容静默放回来：草稿桶连续写入、
//     生效指针只往前推，于是跨过一次被回滚的版本就复活了那段语料，且不留任何痕迹。
//   - 落库放进发布这一个事务里，失败就是"这次发布没发生"，连 apply_failed 这个态都不必存在。
//
// 建表登记走 internal/pkg/db 的 allModels() 与 migrate_test.go 的 mustCover，
// 不产出 v3_NN_*.go（同 T-P2-05/T-P8-03 的实测口径：生产建表只跑 AutoMigrate）。
// 但 knowledge_chunks 的**版本列默认值**与闸门片段在 internal/pkg/kbrelease，
// 本文件只声明形状 —— 两侧同改，别只改一边。
package model

import "time"

// KBChangeRequest 一条知识库变更请求（表 kb_change_requests）
//
// 它描述的是"有人想把某库改成什么样"，**不是**内容本身：内容在 apply 之前只活在本行的
// content 列里，一行都不进 knowledge_chunks —— 这就是 AC①（未审批变更不进线上检索）的
// 全部实现，不依赖任何读路径的过滤。
type KBChangeRequest struct {
	// ID 业务主键（service 生成，形如 kbc_<unixnano>_<seq>，同 approval_requests 的取向）。
	// 审批侧用它当 subject_id，所以它必须出现在列表/审计/日志里，也因此不需要额外的
	// 恢复凭证（approval_requests.ResumeToken 那种 json:"-" 的列在本表没有对应需求）。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// ProductID 生效归属：线上 RAG 召回按 product_id 取数（不是 knowledge_bases.id，
	// 见 kb_canary.go 文件头记的实测：生产检索链根本不看 knowledge_bases 行）。
	// 所以版本闸门、待发布桶、发布/回滚全部挂在 product_id 上。
	ProductID string `gorm:"type:varchar(64);index;not null;default:''" json:"product_id"`

	DocumentID uint64 `gorm:"index;not null;default:0" json:"document_id"`

	// Op 取值见 KBChangeOp* 三常量。add 只填 content；revise 要 content + 被替换的
	// target_chunk_id；retire 只要 target_chunk_id。这组约束由 service 判，不交给列宽。
	Op string `gorm:"type:varchar(16);not null" json:"op"`

	// TargetChunkID 被改/被撤的那一条 chunk。revise 与 retire 必填，add 必为 0。
	// 它是**逻辑外键**，刻意不建 FK：knowledge_chunks 会被重建流程物理删
	// （incremental_indexer.go:207 的 DeleteByDocumentID），一条 FK 只会让"引用过已删
	// 分段"的变更行变成整行删不掉的砖，而变更历史是 AC③ 要的，不能因为目标没了就消失。
	TargetChunkID uint64 `gorm:"not null;default:0" json:"target_chunk_id"`

	Content string `gorm:"type:text" json:"content"`

	// Reason 变更理由。AC③ 的载体之一：谁提的、为什么改，必须和裁决记录一起留得下来。
	Reason string `gorm:"type:text;not null" json:"reason"`

	Status string `gorm:"type:varchar(16);index;not null;default:'pending'" json:"status"`

	// ApprovalID 关联 approval_requests.id。**提交时先入队审批、再插本行**：
	// 反过来会留下"一条 pending 变更、没有对应审批"的孤儿，而它是 AC① 的反例
	// （没有审批记录可查，发布时只能按"没批"处理，但那行看上去像待办）。
	ApprovalID string `gorm:"type:text;index;not null;default:''" json:"approval_id"`

	RequestedBy string `gorm:"type:text;not null" json:"requested_by"`

	// ReleaseVersion 本条在第几版生效（apply 时写入，与 chunk 行的 kb_version 同值）。
	// 它是"回滚挡住了哪几条变更"这一判据的落点（见 KBRelease.RecalledVersion）。
	ReleaseVersion int `gorm:"not null;default:0" json:"release_version"`

	// AppliedChunkID add/revise 落库后新插入那条 chunk 的 id（retire 恒 0）。
	// 只作指认用：内容已经按版本进语料了，不需要它也能算可见性。
	AppliedChunkID uint64 `gorm:"not null;default:0" json:"applied_chunk_id"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名
func (KBChangeRequest) TableName() string { return "kb_change_requests" }

// 变更动作值域（三态，与卡面"新增/修订/下线"一致）。
const (
	KBChangeOpAdd    = "add"
	KBChangeOpRevise = "revise"
	KBChangeOpRetire = "retire"
)

// KBChangeOps 全部合法动作（表驱动测试与越界判据用）。
var KBChangeOps = []string{KBChangeOpAdd, KBChangeOpRevise, KBChangeOpRetire}

// 变更状态值域。
//
// 刻意**不设 rejected / expired**："批没批、有没有过期"的唯一事实源是
// approval_requests.status（同 C2"只建一套"的裁定）。在这里再记一份 rejected，
// 就会出现"审批说已批准、变更说已拒"这种永远查不出来的分歧；本表只记
// "有没有落进语料"和"发起人撤没撤"这两件 approval 表管不着的事。
const (
	KBChangeStatusPending   = "pending"
	KBChangeStatusApplied   = "applied"
	KBChangeStatusWithdrawn = "withdrawn"
)

// KBChangeStatuses 全部合法状态。
var KBChangeStatuses = []string{
	KBChangeStatusPending,
	KBChangeStatusApplied,
	KBChangeStatusWithdrawn,
}

// kbChangeLegalTransitions 变更行的唯一合法跃迁表。
//
// 三条取舍：
//  1. applied 是终态。回滚**不**把 applied 翻回 pending —— 那条改动确实生效过一次，
//     这是历史事实；"当前是否在服"由 kb_releases 的指针回答，不由这一行回答。
//     两个事实分放两处，才不会出现"状态说生效、语料里查无此文"的分歧。
//  2. withdrawn 只能由 pending 去：已经发布出去的内容撤不掉，只能提一条新的 retire
//     再走一次审批 —— 想撤回已上线的东西不需要一条特权路径，需要一次新的批准。
//  3. 没有 pending→applied 之外的落库路径：apply 只发生在发布事务内。
var kbChangeLegalTransitions = map[string]map[string]bool{
	KBChangeStatusPending: {
		KBChangeStatusApplied:   true,
		KBChangeStatusWithdrawn: true,
	},
	KBChangeStatusApplied:   {},
	KBChangeStatusWithdrawn: {},
}

// KBChangeTransitionAllowed 判 from→to 是否是合法跃迁（唯一判据，读写两侧都问它）。
func KBChangeTransitionAllowed(from, to string) bool {
	return kbChangeLegalTransitions[from][to]
}

// KBRelease 一个库的发布指针（表 kb_releases，一行一库）
//
// 四个版本号各自只答一个问题，合起来就是整套发布/回滚语义：
//   - allocated_version 是**高水位**（只增不减）：任何新桶都从它往后取一格，
//     于是"版本号"与"时间顺序"永远同向，回滚不会让某个号被复用；
//   - draft_version 是待发布桶（0 = 当前没有在攒的改动）：导入链路与已批准的变更
//     都落进这一格，一次发布整体上线；
//   - effective_version 是在服快照号（0 = 还没发布过任何一版）：**读路径唯一读的字段**；
//   - previous_version 只答"回滚回到哪一格"，因此回滚天然只有一步（见 RecalledVersion）。
type KBRelease struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// ProductID 一库一行。唯一索引是 EnsureDraftStamp 那条 ON CONFLICT 的依赖，
	// 摘掉它分配并发语义就变了（见 kbrelease.AllocateDraftSQL）。
	ProductID string `gorm:"type:varchar(64);uniqueIndex;not null;default:''" json:"product_id"`

	// Governed 本库是否进发布制。**这道锁与全局旗子是两件事**：
	// 旗子决定"闸门这段 SQL 加不加"，本列决定"哪个库被管"。
	// 之所以不能拿 effective_version > 0 当"已治理"：一个刚启用发布制、还一条没发的库
	// 必须立刻把新导入的内容挡在待发布桶里，而它此刻 effective 就是 0。
	Governed bool `gorm:"not null;default:false" json:"governed"`

	EffectiveVersion int `gorm:"not null;default:0" json:"effective_version"`
	PreviousVersion  int `gorm:"not null;default:0" json:"previous_version"`
	DraftVersion     int `gorm:"not null;default:0" json:"draft_version"`
	AllocatedVersion int `gorm:"not null;default:0" json:"allocated_version"`

	// RecalledVersion 被回滚掉的那个版本号（0 = 没有）。它是**发布禁令**：
	// 只要它 > effective_version，下一次发布就会被拒 —— 否则指针跨过这一格时，
	// 上次被回滚掉的那段语料会随"kb_version <= 新 effective"重新可见，
	// 而没有任何一次动作批准过它回来。这一条就是二次审核否掉旧设计的那个洞。
	// 解除只有两条路：Restore（回滚的逆操作，把号放回去）或撤掉那一版的变更。
	RecalledVersion int    `gorm:"not null;default:0" json:"recalled_version"`
	ChangedBy       string `gorm:"type:text" json:"changed_by"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (KBRelease) TableName() string { return "kb_releases" }

// KBChangeAuditLog 变更与发布的留痕（表 kb_change_audit_logs，AC③）
//
// 形状照 ConfigParamAuditLog（model/config_param.go:38）：被改对象的定位键 + 改前改后
// 两个文本 + 动作 + 操作者 + 时间。不新建第二套审计形状的理由很实在——运营查"谁动的"
// 时学的是同一张表的读法，而列语义一致才能复用既有的查询与页面。
//
// 它与 kb_change_requests 的分工：变更行只有一份**当前**状态（它的 UpdatedAt 会被
// 每次改写覆盖），而审计行是只增的事件流。裁决者、发布时间、回滚是谁按的，都只在
// 这里查得到 —— 也就是说"删掉这张表"等于把 AC③ 一起删掉。
type KBChangeAuditLog struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// SubjectKey 被改对象：`change:<id>` 或 `release:<product_id>`。
	// 用一个键而不是两列，是为了让"某条变更的全部动作"和"某个库的全部发布动作"
	// 走同一条索引查（下面那个 index 就是为它留的）。
	SubjectKey string `gorm:"type:varchar(128);index;not null" json:"subject_key"`

	OldValue string `gorm:"type:text" json:"old_value"`
	NewValue string `gorm:"type:text" json:"new_value"`

	// Action 取值见 KBAudit* 常量。
	Action string `gorm:"type:varchar(24);not null" json:"action"`

	// Actor 操作者标识。这里是 text 而非 uint：变更可以来自管理端登录用户，也可以
	// 来自系统通路（导入打戳记的是 system:import），拿外键列去装一个非人主体，
	// 只会得到一个永远对不上的引用。
	Actor     string    `gorm:"type:text;not null" json:"actor"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (KBChangeAuditLog) TableName() string { return "kb_change_audit_logs" }

// 审计动作值域（一次变更请求的一生 + 一次发布指针的移动）。
const (
	KBAuditSubmitted = "submitted" // 变更入队（同时创建了审批请求）
	KBAuditWithdrawn = "withdrawn" // 发起人撤回
	KBAuditPublished = "published" // 一批变更落库 + 生效指针前移
	KBAuditRollback  = "rollback"  // 生效指针回拨（不碰语料）
	KBAuditRestored  = "restored"  // 回滚的逆操作
	KBAuditGoverned  = "governed"  // 启用/停用发布制（governed 翻转）
)

// KBChangeAuditActions 全部合法动作。
var KBChangeAuditActions = []string{
	KBAuditSubmitted,
	KBAuditWithdrawn,
	KBAuditPublished,
	KBAuditRollback,
	KBAuditRestored,
	KBAuditGoverned,
}

// IsValidKBChangeOp 动作是否合法（service 与表驱动测试共用一份判据，避免两处各写一遍 switch）。
func IsValidKBChangeOp(op string) bool {
	for _, v := range KBChangeOps {
		if v == op {
			return true
		}
	}
	return false
}

// IsValidKBChangeStatus 状态是否合法。
func IsValidKBChangeStatus(s string) bool {
	for _, v := range KBChangeStatuses {
		if v == s {
			return true
		}
	}
	return false
}
