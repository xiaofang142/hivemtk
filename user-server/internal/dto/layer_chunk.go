// Package dto 提供数据传输对象 (Layer 决策 / FAQ 匹配 / 流式输出)
//
// 设计依据: AI 智能体性能优化 (/ /)
//
// 三个核心 DTO:
//   - LayerDecision       双层架构路由决策 (Layer1 / Layer2)
//   - FAQMatchResult      FAQ 匹配结果 (含 score + 命中后元数据)
//   - StreamChunk         WebSocket 流式输出 (start/delta/final/error 4 类)
package dto

// Layer 取值
const (
	Layer1 = "layer1"
	Layer2 = "layer2"
)

// LayerDecisionReason 取值
const (
	ReasonFAQHit            = "faq_hit"
	ReasonSOPHit            = "sop_hit"
	ReasonFallback          = "fallback"
	ReasonLayer1Disabled    = "layer1_disabled"
	ReasonLowConfidenceSkip = "low_confidence_skip"
)

// LayerDecision 双层架构路由决策
type LayerDecision struct {
	Layer      string  `json:"layer"`
	SkipLLM    bool    `json:"skip_llm"`
	Reply      string  `json:"reply"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	FAQID      uint    `json:"faq_id"`
	SOPID      uint    `json:"sop_id"`
	Intent     string  `json:"intent"`
	WallMs     int     `json:"wall_ms"`
	Metadata   string  `json:"metadata"`
}

// FAQMatchResult FAQ 匹配结果
type FAQMatchResult struct {
	Entry     *FAQEntry `json:"entry,omitempty"`
	Score     float64   `json:"score"`
	Rank      int       `json:"rank"`
	HitCount  int64     `json:"hit_count"`
	MatchType string    `json:"match_type"`
}

// FAQEntry FAQ 简短 DTO (避免直接暴露 model)
type FAQEntry struct {
	ID         uint     `json:"id"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer"`
	Keywords   []string `json:"keywords"`
	Category   string   `json:"category"`
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	HitCount   int64    `json:"hit_count"`
	Enabled    *bool    `json:"enabled,omitempty"`
	AgentID    *uint    `json:"agent_id,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
}

// SOPTemplate SOP 模板 DTO (: 前端 SOP 模板管理页面)
type SOPTemplate struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	Intent     string  `json:"intent"`
	Stage      string  `json:"stage"`
	Template   string  `json:"template"`
	Vars       string  `json:"vars"`
	Priority   int     `json:"priority"`
	Confidence float64 `json:"confidence"`
	HitCount   int64   `json:"hit_count"`
	Enabled    *bool   `json:"enabled,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
	UpdatedAt  string  `json:"updated_at,omitempty"`
}

// StreamChunkType 取值
const (
	ChunkTypeStart = "start"
	ChunkTypeDelta = "delta"
	ChunkTypeFinal = "final"
	ChunkTypeError = "error"
)

// StreamChunk WebSocket 流式输出
//
// 协议:
//   - {"type":"start", "trace_id":"xxx", "intent":"greeting"} -> 流开始
//   - {"type":"delta", "text":"你好"} -> 增量 chunk
//   - {"type":"final", "text":"完整回复", "steps":[...], "wall_ms":1234} -> 结束
//   - {"type":"error", "msg":"llm_timeout"} -> 错误
type StreamChunk struct {
	Type     string         `json:"type"`
	TraceID  string         `json:"trace_id"`
	Text     string         `json:"text,omitempty"`
	Intent   string         `json:"intent,omitempty"`
	Step     string         `json:"step,omitempty"`
	Steps    []SalesStepLog `json:"steps,omitempty"`
	WallMs   int            `json:"wall_ms,omitempty"`
	Layer    string         `json:"layer,omitempty"`
	Model    string         `json:"model,omitempty"`
	Tokens   int            `json:"tokens,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata string         `json:"metadata,omitempty"`
}
