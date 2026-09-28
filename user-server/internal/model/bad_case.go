// bad_case.go G-2 Bad Case 闭环模型（新规划任务清单 T-P8-03）。
//
// 审计侧的事实是"全仓 Bad Case 零命中"（新规划.md:271 / CS-31）：低质回答既没有被
// 记下来的地方，也就没有"谁看过、判成什么、修没修"这条链。本表补的就是这条链的地基。
//
// 与 human_tasks 同一取舍：这一行是**"有一件事需要人判"的索引 + 判完的结论**，
// 不复制会话与消息的正本，靠 (session_id, message_id) 指回去；但**答案原文要抄**，
// 因为打标的人要对着这句回答判断，而消息表允许被后续业务改写（撤回/编辑），
// 一条已经进了评测集的样本如果被上游改掉，评测分数就再也对不上任何一份字节。
//
// 建表登记走 internal/pkg/db 的 allModels() 与 migrate_test.go 的 mustCover，
// 不产出 v3_NN 迁移文件（同 T-P3-01/T-P3-02 的实测口径）。
package model

import (
	"strings"
	"time"
)

// BadCase 一条待判/已判的低质回答样本（表 bad_cases）。
//
// ID 不自增（形如 bc_<unixnano>_<seq>），与 human_tasks / approval_requests 同一取向：
// 导出成评测集后这个 id 会长期存在于仓库外的文件里，自增号在跨库搬迁与重放时对不上号。
type BadCase struct {
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// Source 这条是**怎么进来的**，值域见 BadCaseSources。
	// 它决定 dedup 键的形态，也决定"自动标记有没有在刷屏"这类运营问题怎么查。
	Source string `gorm:"type:varchar(32);index;not null" json:"source"`

	// Status 状态机位置，值域见 BadCaseStatuses，迁移表见 BadCaseTransitions。
	Status string `gorm:"type:varchar(16);index;not null" json:"status"`

	// ---- 指回事实源 ----

	SessionID string `gorm:"type:text;index" json:"session_id,omitempty"`
	MessageID string `gorm:"type:text" json:"message_id,omitempty"`
	// SignalID 关联的 confidence_signals.signal_id。可空：手动录入与老数据没有信号行。
	// 有值时它是"为什么被标记"的可查证据，而不是本表自说自话的一句理由。
	SignalID   string `gorm:"type:text" json:"signal_id,omitempty"`
	IntentType string `gorm:"type:varchar(64);index" json:"intent_type,omitempty"`

	// ---- 判案要看的现场（抄本，见文件头那段理由）----

	QueryText  string `gorm:"type:text" json:"query_text,omitempty"`
	AnswerText string `gorm:"type:text" json:"answer_text,omitempty"`

	// Confidence/Threshold 标记那一刻的聚合置信度与当时生效的阈值。
	// 两个都要存：只存前者的话，阈值一改（动态阈值本来就会随客户等级漂），
	// 事后就没法回答"这条到底算不算低质"，而打标队列里最贵的正是判据。
	Confidence float64 `gorm:"type:decimal(5,4);not null" json:"confidence"`
	Threshold  float64 `gorm:"type:decimal(5,4);not null" json:"threshold"`
	// RetrievedCount 这次回答实际检回几条知识片段。0 就是 AC③ 要的"零命中"。
	RetrievedCount int `gorm:"not null;default:0" json:"retrieved_count"`
	// MarkReason 自动标记时系统给的一句话（含否决规则名、降级档位等）。原样存，不归一。
	MarkReason string `gorm:"type:text" json:"mark_reason,omitempty"`

	// ---- 人工判完的结论 ----

	// Label 归因类目，值域见 BadCaseLabels；未打标时为空。
	Label string `gorm:"type:varchar(32);index" json:"label,omitempty"`
	// LabelNote 判定依据（"库里有这条但写的是旧政策"一类）。导出评测集时它是标准答案的注脚。
	LabelNote    string     `gorm:"type:text" json:"label_note,omitempty"`
	LabelerID    string     `gorm:"type:text" json:"labeler_id,omitempty"`
	LabeledAt    *time.Time `json:"labeled_at,omitempty"`
	EvalSetID    string     `gorm:"type:varchar(64);index" json:"eval_set_id,omitempty"`
	ExportedAt   *time.Time `json:"exported_at,omitempty"`
	FixLayer     string     `gorm:"type:varchar(16);index" json:"fix_layer,omitempty"`
	CancelReason string     `gorm:"type:text" json:"cancel_reason,omitempty"`

	// DedupKey 幂等键，全表唯一。自动来源写成 <source>|<session_id>|<message_id>，
	// 手动来源写成本行 id（手动录两条同样现场是允许的，那是人的判断不是系统的重复）。
	//
	// 为什么用唯一键而不是"先查再插"：标记发生在对话热路径上（异步 goroutine），
	// 同一轮被并发标记两次是常态（超时重试、消息重投），先查再插之间没有任何东西挡得住。
	DedupKey  string    `gorm:"type:text;uniqueIndex;not null" json:"dedup_key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名
func (BadCase) TableName() string { return "bad_cases" }

// 标记来源（Source 值域）。
const (
	// BadCaseSourceLowConfidence 置信度低于本轮生效阈值 —— AC① 的主来源，接 extractConfidence。
	BadCaseSourceLowConfidence = "low_confidence"
	// BadCaseSourceZeroHit 检回 0 条片段：答案再好也是无本之木。
	// 它单独成一类而不并进 low_confidence，是因为修法完全不同（补知识 vs 调阈值），
	// 而这两拨人不在一个组里。
	BadCaseSourceZeroHit = "zero_hit"
	// BadCaseSourceManual 人工在页面上补录（坐席觉得答得烂，但置信度没觉得）。
	BadCaseSourceManual = "manual"
)

// BadCaseSources 来源全集（列表过滤器与仓储校验共用这一份）。
var BadCaseSources = []string{
	BadCaseSourceLowConfidence,
	BadCaseSourceZeroHit,
	BadCaseSourceManual,
}

// 状态（Status 值域）。
const (
	BadCaseStatusPending   = "pending"
	BadCaseStatusLabeled   = "labeled"
	BadCaseStatusExported  = "exported"
	BadCaseStatusDismissed = "dismissed"
)

// BadCaseStatuses 状态全集。
var BadCaseStatuses = []string{
	BadCaseStatusPending,
	BadCaseStatusLabeled,
	BadCaseStatusExported,
	BadCaseStatusDismissed,
}

// BadCaseTransitions 状态机迁移表（唯一真源，服务层照它判，不写 if）。
//
// exported 与 dismissed 是终态：
//   - exported 再改动，仓库外那份评测集就和库里的结论不一致了 —— 要改就重新导一份新集；
//   - dismissed 是"这不算坏例"的结论，重开一条等于把同一现场判两次。
var BadCaseTransitions = map[string][]string{
	BadCaseStatusPending:   {BadCaseStatusLabeled, BadCaseStatusDismissed},
	BadCaseStatusLabeled:   {BadCaseStatusExported, BadCaseStatusDismissed},
	BadCaseStatusExported:  nil,
	BadCaseStatusDismissed: nil,
}

// CanBadCaseTransition 报告 status 能否迁到 to。未知状态一律判否。
func CanBadCaseTransition(status, to string) bool {
	for _, allow := range BadCaseTransitions[status] {
		if allow == to {
			return true
		}
	}
	return false
}

// 归因类目（Label 值域）。每一个都指向一个明确的修复责任层，见 BadCaseLabelFixLayer。
const (
	BadCaseLabelKBMissing       = "kb_missing"
	BadCaseLabelKBStale         = "kb_stale"
	BadCaseLabelRetrieveMiss    = "retrieve_miss"
	BadCaseLabelRetrieveWrong   = "retrieve_wrong"
	BadCaseLabelGenerationWrong = "generation_wrong"
	BadCaseLabelGenerationStyle = "generation_style"
	BadCaseLabelIntentMisjudge  = "intent_misjudge"
)

// BadCaseLabels 类目全集。
var BadCaseLabels = []string{
	BadCaseLabelKBMissing,
	BadCaseLabelKBStale,
	BadCaseLabelRetrieveMiss,
	BadCaseLabelRetrieveWrong,
	BadCaseLabelGenerationWrong,
	BadCaseLabelGenerationStyle,
	BadCaseLabelIntentMisjudge,
}

// 修复责任层（FixLayer 值域）。归因到这里，P8 的"闭环"才有落点：
// 一份只有类目的列表要再人工读一遍才知道该找谁，而率值统计要的正是"每层各多少"。
const (
	BadCaseFixLayerKnowledge  = "knowledge"
	BadCaseFixLayerRetrieval  = "retrieval"
	BadCaseFixLayerGeneration = "generation"
	BadCaseFixLayerIntent     = "intent"
)

// BadCaseFixLayers 责任层全集。
var BadCaseFixLayers = []string{
	BadCaseFixLayerKnowledge,
	BadCaseFixLayerRetrieval,
	BadCaseFixLayerGeneration,
	BadCaseFixLayerIntent,
}

// BadCaseLabelFixLayer 类目 → 责任层。表写成数据而不是 switch：
// "七个类目各自落在四层里的哪一层"本身就是本卡的验收对象（见 model 层的表驱动用例）。
var BadCaseLabelFixLayer = map[string]string{
	BadCaseLabelKBMissing:       BadCaseFixLayerKnowledge,
	BadCaseLabelKBStale:         BadCaseFixLayerKnowledge,
	BadCaseLabelRetrieveMiss:    BadCaseFixLayerRetrieval,
	BadCaseLabelRetrieveWrong:   BadCaseFixLayerRetrieval,
	BadCaseLabelGenerationWrong: BadCaseFixLayerGeneration,
	BadCaseLabelGenerationStyle: BadCaseFixLayerGeneration,
	BadCaseLabelIntentMisjudge:  BadCaseFixLayerIntent,
}

// FixLayerOfLabel 返回类目对应的责任层；未知类目返回空串（调用方须自行拒绝，
// 不许把未知读成"没有层"，那等于悄悄丢一行归因）。
func FixLayerOfLabel(label string) string { return BadCaseLabelFixLayer[label] }

// IsKnownBadCaseSource / IsKnownBadCaseStatus / IsKnownBadCaseLabel 值域判定。
func IsKnownBadCaseSource(v string) bool { return badCaseIn(BadCaseSources, v) }
func IsKnownBadCaseStatus(v string) bool { return badCaseIn(BadCaseStatuses, v) }
func IsKnownBadCaseLabel(v string) bool  { return badCaseIn(BadCaseLabels, v) }

func badCaseIn(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// BadCaseDedupKey 算出自动标记的幂等键。
//
// message_id 为空时返回 ok=false：热路径上拿不到消息 id 就意味着这条无法指回现场，
// 让它进唯一键会把"同一条键"的后续真坏例全部静默吃掉（唯一键冲突被当成"已存在"忽略）。
func BadCaseDedupKey(source, sessionID, messageID string) (string, bool) {
	if source != BadCaseSourceManual && strings.TrimSpace(messageID) == "" {
		return "", false
	}
	return source + "|" + sessionID + "|" + messageID, true
}
