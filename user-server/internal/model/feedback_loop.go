package model

import (
	"gorm.io/gorm"
	"time"
)

// FeedbackEvent 反馈事件（append-only 流水）
//
// 三类信号统一入库：
//   - explicit  显式反馈（点赞/点踩/评分/投诉/转人工原因/人工修订）
//   - implicit  隐式反馈（转化率/回复率/会话时长/转人工）
//   - champion  销冠标记（is_champion/script_adopt/5 维度评分）
type FeedbackEvent struct {
	ID                uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID           string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"event_id"`
	SessionID         string         `gorm:"type:varchar(120);index;not null" json:"session_id"`
	CustomerID        string         `gorm:"type:varchar(64);index;not null" json:"customer_id"`
	SOPID             uint           `gorm:"index" json:"sop_id"`
	ExecutionID       uint           `json:"execution_id"`
	Variant           string         `gorm:"type:varchar(50);index" json:"variant"`
	PromptCandidateID uint           `gorm:"index" json:"prompt_candidate_id"`
	EventType         string         `gorm:"type:varchar(30);index;not null" json:"event_type"`
	SignalKey         string         `gorm:"type:varchar(50);index;not null" json:"signal_key"`
	SignalValue       JSONMap        `gorm:"type:jsonb;not null" json:"signal_value"`
	Weight            float64        `gorm:"type:decimal(4,2);not null;default:0" json:"weight"`
	Reward            float64        `gorm:"type:decimal(6,3);not null;default:0" json:"reward"`
	AIReply           string         `gorm:"type:text" json:"ai_reply"`
	CustomerMsg       string         `gorm:"type:text" json:"customer_msg"`
	Metadata          JSONMap        `gorm:"type:jsonb" json:"metadata"`
	CreatedBy         uint           `json:"created_by"`
	CreatedAt         time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 表名
func (FeedbackEvent) TableName() string { return "feedback_events" }

// FeedbackSignalKey 反馈信号类型常量（与 service/feedback_loop SignalKey 字符串值一致）
const (
	FeedbackSignalLike         = "like"
	FeedbackSignalDislike      = "dislike"
	FeedbackSignalRating       = "rating"
	FeedbackSignalComplaint    = "complaint"
	FeedbackSignalConversion   = "conversion"
	FeedbackSignalReplyRate    = "reply_rate"
	FeedbackSignalDuration     = "duration"
	FeedbackSignalTransfer     = "transfer"
	FeedbackSignalChampionMark = "champion_mark"
	FeedbackSignalScriptAdopt  = "script_adopt"
)

// FeedbackEventType 反馈事件类型常量
const (
	FeedbackEventTypeExplicit = "explicit"
	FeedbackEventTypeImplicit = "implicit"
	FeedbackEventTypeChampion = "champion"
)

// FeedbackSignal 反馈信号聚合（按 session_id 唯一）
//
// 一个 session 多次反馈事件累加为单一 reward 值，供 Bandit / 画像分析消费
type FeedbackSignal struct {
	ID                uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID         string     `gorm:"type:varchar(120);uniqueIndex;not null" json:"session_id"`
	CustomerID        string     `gorm:"type:varchar(64);not null" json:"customer_id"`
	SOPID             uint       `gorm:"index" json:"sop_id"`
	Variant           string     `gorm:"type:varchar(50);index" json:"variant"`
	PromptCandidateID uint       `gorm:"index" json:"prompt_candidate_id"`
	AggregatedReward  float64    `gorm:"type:decimal(8,3);not null;default:0" json:"aggregated_reward"`
	SignalCount       int        `gorm:"not null;default:0" json:"signal_count"`
	SignalBreakdown   JSONMap    `gorm:"type:jsonb;not null" json:"signal_breakdown"`
	Outcome           string     `gorm:"type:varchar(20);not null;default:'pending'" json:"outcome"`
	IsChampion        bool       `gorm:"not null;default:false" json:"is_champion"`
	SessionStartedAt  *time.Time `json:"session_started_at"`
	SessionEndedAt    *time.Time `json:"session_ended_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (FeedbackSignal) TableName() string { return "feedback_signals" }

// FeedbackSignalOutcome 反馈信号结果常量
const (
	FeedbackSignalOutcomeSuccess = "success"
	FeedbackSignalOutcomeFail    = "fail"
	FeedbackSignalOutcomePending = "pending"
)

// ChampionDialogue 销冠对话片段
//
// 来源：ChampionDialogueAnalyzer 从 feedback_signals 筛选高价值对话聚类后入库
// 用途：1) 相似场景话术检索（pgvector 余弦相似度） 2) 销冠画像快照生成
type ChampionDialogue struct {
	ID                  uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	DialogueFingerprint string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"dialogue_fingerprint"`
	SessionID           string     `gorm:"type:varchar(120);not null" json:"session_id"`
	CustomerID          string     `gorm:"type:varchar(64);not null" json:"customer_id"`
	StaffID             uint       `gorm:"index" json:"staff_id"`
	StaffName           string     `gorm:"type:varchar(100)" json:"staff_name"`
	Scenario            string     `gorm:"type:varchar(50);not null;index" json:"scenario"`
	JourneyStage        string     `gorm:"type:varchar(30)" json:"journey_stage"`
	CustomerMsg         string     `gorm:"type:text;not null" json:"customer_msg"`
	ChampionReply       string     `gorm:"type:text;not null" json:"champion_reply"`
	ContextMsgs         JSONMap    `gorm:"type:jsonb" json:"context_msgs"`
	Embedding           string     `gorm:"type:vector(1024);not null" json:"-"`
	ClusterID           uint       `gorm:"index" json:"cluster_id"`
	Reward              float64    `gorm:"type:decimal(6,3);not null;default:0" json:"reward"`
	ConversionAchieved  bool       `gorm:"not null;default:false" json:"conversion_achieved"`
	ExtractedScripts    JSONMap    `gorm:"type:jsonb" json:"extracted_scripts"`
	ExtractedAt         *time.Time `json:"extracted_at"`
	CreatedAt           time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName 表名
func (ChampionDialogue) TableName() string { return "champion_dialogues" }

// ChampionScenario 销冠对话场景常量
const (
	ChampionScenarioObjection  = "objection"
	ChampionScenarioClosing    = "closing"
	ChampionScenarioFollowup   = "followup"
	ChampionScenarioNurture    = "nurture"
	ChampionScenarioRepurchase = "repurchase"
	ChampionScenarioGeneral    = "general"
)

// ChampionJourneyStage 客户旅程阶段常量
const (
	ChampionStageLead     = "lead"
	ChampionStageContact  = "contact"
	ChampionStageConsider = "consider"
	ChampionStageDecide   = "decide"
	ChampionStageRetain   = "retain"
)

// PromptCandidate Prompt 候选版本
//
// 来源：PromptIterator 基于负反馈样本让 LLM 生成改进版本
// 用途：1) A/B 测试 variant 池 2) SalesEngine.generateCandidate 加载 system/user prompt
type PromptCandidate struct {
	ID                 uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SOPNodeID          string     `gorm:"type:varchar(50);index" json:"sop_node_id"`
	SOPID              uint       `gorm:"index" json:"sop_id"`
	Scenario           string     `gorm:"type:varchar(50);not null;index" json:"scenario"`
	Version            string     `gorm:"type:varchar(20);not null" json:"version"`
	Title              string     `gorm:"type:varchar(100);not null" json:"title"`
	SystemPrompt       string     `gorm:"type:text;not null" json:"system_prompt"`
	UserPromptTemplate string     `gorm:"type:text;not null" json:"user_prompt_template"`
	Variables          JSONMap    `gorm:"type:jsonb" json:"variables"`
	ParentID           uint       `gorm:"index" json:"parent_id"`
	ImprovementNotes   string     `gorm:"type:text" json:"improvement_notes"`
	Status             string     `gorm:"type:varchar(20);not null;default:'draft';index" json:"status"`
	Alpha              float64    `gorm:"type:decimal(8,2);not null;default:2" json:"alpha"`
	Beta               float64    `gorm:"type:decimal(8,2);not null;default:2" json:"beta"`
	SampleCount        int        `gorm:"not null;default:0" json:"sample_count"`
	SuccessCount       int        `gorm:"not null;default:0" json:"success_count"`
	AvgReward          float64    `gorm:"type:decimal(6,3);not null;default:0" json:"avg_reward"`
	PromotedAt         *time.Time `json:"promoted_at"`
	RetiredAt          *time.Time `json:"retired_at"`
	RetiredReason      string     `gorm:"type:varchar(100)" json:"retired_reason"`
	ReviewedBy         uint       `json:"reviewed_by"`
	ReviewedAt         *time.Time `json:"reviewed_at"`
	GeneratedBy        string     `gorm:"type:varchar(20);not null;default:'auto'" json:"generated_by"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (PromptCandidate) TableName() string { return "prompt_candidates" }

// PromptCandidateStatus Prompt 候选状态常量
const (
	PromptCandidateStatusDraft    = "draft"
	PromptCandidateStatusPending  = "pending"
	PromptCandidateStatusApproved = "approved"
	PromptCandidateStatusActive   = "active"
	PromptCandidateStatusPromoted = "promoted"
	PromptCandidateStatusRetired  = "retired"
)

// BanditArm Multi-Armed Bandit 臂状态
//
// 每个 arm 关联一个 variant（prompt_candidate / sop_variant / script）
// 维护 Beta(alpha, beta) 后验，由 BanditAllocator 增量更新
type BanditArm struct {
	ID                uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExperimentID      string     `gorm:"type:varchar(64);index;not null" json:"experiment_id"`
	ExperimentType    string     `gorm:"type:varchar(20);not null" json:"experiment_type"`
	ArmKey            string     `gorm:"type:varchar(100);not null;uniqueIndex:idx_bandit_exp_arm" json:"arm_key"`
	SOPID             uint       `gorm:"index" json:"sop_id"`
	Variant           string     `gorm:"type:varchar(50)" json:"variant"`
	PromptCandidateID uint       `json:"prompt_candidate_id"`
	Alpha             float64    `gorm:"type:decimal(10,2);not null;default:1" json:"alpha"`
	Beta              float64    `gorm:"type:decimal(10,2);not null;default:1" json:"beta"`
	TotalTrials       int64      `gorm:"not null;default:0" json:"total_trials"`
	SuccessTrials     int64      `gorm:"not null;default:0" json:"success_trials"`
	SumReward         float64    `gorm:"type:decimal(12,3);not null;default:0" json:"sum_reward"`
	AvgReward         float64    `gorm:"type:decimal(8,4);not null;default:0" json:"avg_reward"`
	MinTrafficPct     float64    `gorm:"type:decimal(5,2);not null;default:10.00" json:"min_traffic_pct"`
	MaxTrafficPct     float64    `gorm:"type:decimal(5,2);not null;default:60.00" json:"max_traffic_pct"`
	CurrentTrafficPct float64    `gorm:"type:decimal(5,2);not null;default:0" json:"current_traffic_pct"`
	Status            string     `gorm:"type:varchar(20);not null;default:'exploring';index" json:"status"`
	PromotedAt        *time.Time `json:"promoted_at"`
	RetiredAt         *time.Time `json:"retired_at"`
	PosteriorBestProb float64    `gorm:"type:decimal(5,4)" json:"posterior_best_prob"`
	LastSampledAt     *time.Time `json:"last_sampled_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (BanditArm) TableName() string { return "bandit_arms" }

// BanditArmStatus Bandit 臂状态常量
const (
	BanditArmStatusExploring  = "exploring"
	BanditArmStatusExploiting = "exploiting"
	BanditArmStatusPromoted   = "promoted"
	BanditArmStatusRetired    = "retired"
)

// BanditExperimentType Bandit 实验类型常量
const (
	BanditExperimentTypePrompt     = "prompt"
	BanditExperimentTypeSOPVariant = "sop_variant"
	BanditExperimentTypeScript     = "script"
)

// PromptABTest Prompt A/B 测试配置与结果
//
// 一次 A/B 测试包含多个 arm（variant），由 BanditAllocator 进行流量分配
type PromptABTest struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExperimentID   string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"experiment_id"`
	ExperimentType string     `gorm:"type:varchar(20);not null" json:"experiment_type"`
	SOPID          uint       `gorm:"index" json:"sop_id"`
	SOPNodeID      string     `gorm:"type:varchar(50)" json:"sop_node_id"`
	Name           string     `gorm:"type:varchar(100);not null" json:"name"`
	Description    string     `gorm:"type:text" json:"description"`
	ArmKeys        JSONArray  `gorm:"type:jsonb;not null" json:"arm_keys"`
	Config         JSONMap    `gorm:"type:jsonb;not null" json:"config"`
	Status         string     `gorm:"type:varchar(20);not null;default:'running';index" json:"status"`
	StartedAt      *time.Time `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
	WinnerArmKey   string     `gorm:"type:varchar(100)" json:"winner_arm_key"`
	AutoPromote    bool       `gorm:"not null;default:true" json:"auto_promote"`
	CreatedBy      uint       `json:"created_by"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 表名
func (PromptABTest) TableName() string { return "prompt_ab_tests" }

// PromptABTestStatus A/B 测试状态常量
const (
	PromptABTestStatusDraft      = "draft"
	PromptABTestStatusRunning    = "running"
	PromptABTestStatusPaused     = "paused"
	PromptABTestStatusCompleted  = "completed"
	PromptABTestStatusRolledBack = "rolled_back"
)
