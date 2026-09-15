package model

import "time"

// HumanizeEvaluatorType 评估器类型
type HumanizeEvaluatorType string

const (
	HumanizeEvaluatorRule   HumanizeEvaluatorType = "rule"
	HumanizeEvaluatorLLM    HumanizeEvaluatorType = "llm"
	HumanizeEvaluatorHybrid HumanizeEvaluatorType = "hybrid"
)

// HumanizeSampleStrategy 采样策略
type HumanizeSampleStrategy string

const (
	HumanizeSampleFull           HumanizeSampleStrategy = "full"
	HumanizeSampleBoundary       HumanizeSampleStrategy = "boundary"
	HumanizeSampleSampled        HumanizeSampleStrategy = "sampled"
	HumanizeSampleSampledMonitor HumanizeSampleStrategy = "sampled_monitor"
)

// HumanizeScore 拟人度评估主表（对应 humanize_scores 表）
//
// 每次评估（无论 Rule 还是 LLM）写一条记录；
// 重生成循环中每次评估也写一条记录，用 attempt_count 区分。
type HumanizeScore struct {
	ID                 uint64                 `gorm:"primaryKey;autoIncrement" json:"id"`
	ScoreID            string                 `gorm:"column:score_id;uniqueIndex;size:64;not null" json:"score_id"`
	SessionID          string                 `gorm:"column:session_id;size:128;not null;index:idx_humanize_session,priority:1" json:"session_id"`
	CustomerID         string                 `gorm:"column:customer_id;size:64;not null" json:"customer_id"`
	MessageID          string                 `gorm:"column:message_id;size:128" json:"message_id"`
	Persona            string                 `gorm:"column:persona;size:128;index:idx_humanize_persona,priority:1" json:"persona"`
	Industry           string                 `gorm:"column:industry;size:64;index:idx_humanize_persona,priority:2" json:"industry"`
	Platform           string                 `gorm:"column:platform;size:32" json:"platform"`
	Intent             string                 `gorm:"column:intent;size:32;index:idx_humanize_persona,priority:3" json:"intent"`
	CustomerMessage    string                 `gorm:"column:customer_message;type:text" json:"customer_message"`
	AIReply            string                 `gorm:"column:ai_reply;type:text;not null" json:"ai_reply"`
	FinalReply         string                 `gorm:"column:final_reply;type:text" json:"final_reply"`
	EvaluatorType      HumanizeEvaluatorType  `gorm:"column:evaluator_type;size:16;not null;default:'rule'" json:"evaluator_type"`
	SampleStrategy     HumanizeSampleStrategy `gorm:"column:sample_strategy;size:24;not null;default:'full'" json:"sample_strategy"`
	Naturalness        float64                `gorm:"column:naturalness;type:decimal(4,3);not null" json:"naturalness"`
	Conciseness        float64                `gorm:"column:conciseness;type:decimal(4,3);not null" json:"conciseness"`
	Empathy            float64                `gorm:"column:empathy;type:decimal(4,3);not null" json:"empathy"`
	Professionalism    float64                `gorm:"column:professionalism;type:decimal(4,3);not null" json:"professionalism"`
	Persuasiveness     float64                `gorm:"column:persuasiveness;type:decimal(4,3);not null" json:"persuasiveness"`
	TotalScore         float64                `gorm:"column:total_score;type:decimal(4,3);not null;index:idx_humanize_score" json:"total_score"`
	Threshold          float64                `gorm:"column:threshold;type:decimal(4,3);not null;default:0.850" json:"threshold"`
	DistanceToChampion float64                `gorm:"column:distance_to_champion;type:decimal(5,4);default:0" json:"distance_to_champion"`
	Passed             bool                   `gorm:"column:passed;not null;default:false" json:"passed"`
	AttemptCount       int                    `gorm:"column:attempt_count;not null;default:1" json:"attempt_count"`
	LLMModel           string                 `gorm:"column:llm_model;size:64" json:"llm_model"`
	LLMLatencyMs       int                    `gorm:"column:llm_latency_ms;default:0" json:"llm_latency_ms"`
	ReasonJSON         string                 `gorm:"column:reason_json;type:jsonb;default:'{}'" json:"reason_json"`
	CreatedAt          time.Time              `gorm:"column:created_at;not null;default:now();index:idx_humanize_session,priority:2" json:"created_at"`
}

// TableName 表名
func (HumanizeScore) TableName() string { return "humanize_scores" }

// HumanizeDimensionRecord 维度得分明细（对应 humanize_dimensions 表）
//
// 与 HumanizeScore 一对多（一次评估 5 条维度记录）
type HumanizeDimensionRecord struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ScoreID   string    `gorm:"column:score_id;size:64;not null;index:idx_hd_score,priority:1" json:"score_id"`
	Dimension string    `gorm:"column:dimension;size:32;not null;index:idx_hd_score,priority:2" json:"dimension"`
	Score     float64   `gorm:"column:score;type:decimal(4,3);not null" json:"score"`
	Weight    float64   `gorm:"column:weight;type:decimal(4,3);not null" json:"weight"`
	Reason    string    `gorm:"column:reason;type:text" json:"reason"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now();index:idx_hd_score,priority:3" json:"created_at"`
}

// TableName 表名
func (HumanizeDimensionRecord) TableName() string { return "humanize_dimensions" }

// ChampionBaseline 销冠基线（对应 champion_baselines 表）
//
// persona+industry+intent 三元组唯一确定一条启用基线（version 递增）
type ChampionBaseline struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Persona         string    `gorm:"column:persona;size:128;not null;index:idx_champion_pii,priority:1" json:"persona"`
	Industry        string    `gorm:"column:industry;size:64;not null;index:idx_champion_pii,priority:2" json:"industry"`
	Intent          string    `gorm:"column:intent;size:32;not null;index:idx_champion_pii,priority:3" json:"intent"`
	Naturalness     float64   `gorm:"column:naturalness;type:decimal(4,3);not null" json:"naturalness"`
	Conciseness     float64   `gorm:"column:conciseness;type:decimal(4,3);not null" json:"conciseness"`
	Empathy         float64   `gorm:"column:empathy;type:decimal(4,3);not null" json:"empathy"`
	Professionalism float64   `gorm:"column:professionalism;type:decimal(4,3);not null" json:"professionalism"`
	Persuasiveness  float64   `gorm:"column:persuasiveness;type:decimal(4,3);not null" json:"persuasiveness"`
	SampleCount     int       `gorm:"column:sample_count;not null" json:"sample_count"`
	SampleStddev    float64   `gorm:"column:sample_stddev;type:decimal(4,3)" json:"sample_stddev"`
	PeriodStart     time.Time `gorm:"column:period_start" json:"period_start"`
	PeriodEnd       time.Time `gorm:"column:period_end" json:"period_end"`
	Version         int       `gorm:"column:version;not null;default:1;index:idx_champion_pii,priority:4" json:"version"`
	Enabled         bool      `gorm:"column:enabled;not null;default:true;index:idx_champion_pii,priority:5" json:"enabled"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;default:now()" json:"created_at"`
}

// TableName 表名
func (ChampionBaseline) TableName() string { return "champion_baselines" }

// ChampionPhrase 销冠短语（对应 champion_phrases 表）
//
// TF-IDF 提取的销冠高频短语，用于加权欧氏距离补充信号
type ChampionPhrase struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	BaselineID uint64    `gorm:"column:baseline_id;not null;index:idx_cp_baseline,priority:1" json:"baseline_id"`
	Phrase     string    `gorm:"column:phrase;size:64;not null" json:"phrase"`
	TFIDFScore float64   `gorm:"column:tfidf_score;type:decimal(8,5);not null" json:"tfidf_score"`
	TF         int       `gorm:"column:tf;not null" json:"tf"`
	DF         int       `gorm:"column:df;not null" json:"df"`
	PhraseType string    `gorm:"column:phrase_type;size:16;not null;default:'general'" json:"phrase_type"`
	Rank       int       `gorm:"column:rank;not null;index:idx_cp_baseline,priority:2" json:"rank"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;default:now()" json:"created_at"`
}

// TableName 表名
func (ChampionPhrase) TableName() string { return "champion_phrases" }

// ABTestStat A/B 测试统计结果（对应 ab_test_stats 表）
//
// 与 ABTest 表不同：
//   - ABTest 表：A/B 实验配置（流量分配、目标、状态）
//   - ABTestStat 表：实验统计结果汇总（每组一条记录，含 U/p/d/CI/winner）
type ABTestStat struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ExperimentID    string    `gorm:"column:experiment_id;size:64;not null;index:idx_abstat_exp,priority:1" json:"experiment_id"`
	GroupName       string    `gorm:"column:group_name;size:16;not null;index:idx_abstat_exp,priority:2" json:"group_name"`
	SampleSize      int       `gorm:"column:sample_size;not null" json:"sample_size"`
	MeanScore       float64   `gorm:"column:mean_score;type:decimal(8,4)" json:"mean_score"`
	MedianScore     float64   `gorm:"column:median_score;type:decimal(8,4)" json:"median_score"`
	StddevScore     float64   `gorm:"column:stddev_score;type:decimal(8,4)" json:"stddev_score"`
	MannWhitneyU    int64     `gorm:"column:mann_whitney_u;default:0" json:"mann_whitney_u"`
	MannWhitneyP    float64   `gorm:"column:mann_whitney_p;type:decimal(8,4);default:0" json:"mann_whitney_p"`
	CohensD         float64   `gorm:"column:cohens_d;type:decimal(8,4);default:0" json:"cohens_d"`
	BootstrapCILow  float64   `gorm:"column:bootstrap_ci_low;type:decimal(5,4);default:0" json:"bootstrap_ci_low"`
	BootstrapCIHigh float64   `gorm:"column:bootstrap_ci_high;type:decimal(5,4);default:0" json:"bootstrap_ci_high"`
	Significant     bool      `gorm:"column:significant;default:false" json:"significant"`
	EffectSizeLabel string    `gorm:"column:effect_size_label;size:16;default:'negligible'" json:"effect_size_label"`
	Winner          string    `gorm:"column:winner;size:16;default:'inconclusive'" json:"winner"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;default:now();index:idx_abstat_exp,priority:3" json:"created_at"`
}

// TableName 表名
func (ABTestStat) TableName() string { return "ab_test_stats" }

// LowQualitySampleType 扩展常量（新增类型）
//
// 复用 low_quality_samples 表的 sample_type 字段，仅追加枚举值，不修改表结构
const (
	LowQualitySampleNaturalnessLow    LowQualitySampleType = "naturalness_low"
	LowQualitySamplePersuasivenessLow LowQualitySampleType = "persuasiveness_low"
	LowQualitySampleChampionDistance  LowQualitySampleType = "champion_distance"
	LowQualitySampleABTestLoser       LowQualitySampleType = "ab_test_loser"
)
