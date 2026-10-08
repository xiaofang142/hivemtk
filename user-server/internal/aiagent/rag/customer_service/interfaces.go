package ragcustomerservice

import (
	"context"
	"time"
)

// Session 会话结构
type Session struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	Platform     string         `json:"platform"`
	KBID         string         `json:"kb_id"`
	Status       SessionStatus  `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Metadata     map[string]any `json:"metadata"`
	Conversation *Conversation  `json:"conversation"`
	Config       SessionConfig  `json:"config"`
}

// SessionStatus 会话状态
type SessionStatus string

const (
	SessionActive  SessionStatus = "active"
	SessionPaused  SessionStatus = "paused"
	SessionClosed  SessionStatus = "closed"
	SessionExpired SessionStatus = "expired"
)

// Message 消息结构
type Message struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	Role       MessageRole    `json:"role"`
	Content    string         `json:"content"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata"`
	References []string       `json:"references"`
}

// MessageRole 消息角色
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
)

// Response 回复结构
type Response struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	Content        string         `json:"content"`
	Intent         string         `json:"intent"`
	Confidence     float64        `json:"confidence"`
	References     []Reference    `json:"references"`
	Metadata       map[string]any `json:"metadata"`
	Timestamp      time.Time      `json:"timestamp"`
	ProcessingTime time.Duration  `json:"processing_time"`
	Source         string         `json:"source"`
}

// Reference 引用信息
type Reference struct {
	DocumentID string  `json:"document_id"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	Source     string  `json:"source"`
	ChunkIndex int     `json:"chunk_index"`
}

// Feedback 用户反馈
type Feedback struct {
	SessionID  string         `json:"session_id"`
	ResponseID string         `json:"response_id"`
	Rating     int            `json:"rating"`
	IsHelpful  bool           `json:"is_helpful"`
	Comment    string         `json:"comment"`
	Metadata   map[string]any `json:"metadata"`
}

// SessionConfig 会话配置
type SessionConfig struct {
	MaxHistoryLength int     `json:"max_history_length"`
	Timeout          int     `json:"timeout"`
	Temperature      float64 `json:"temperature"`
	MaxTokens        int     `json:"max_tokens"`
	SystemPrompt     string  `json:"system_prompt"`
	EnableContextual bool    `json:"enable_contextual"`
	EnableLearning   bool    `json:"enable_learning"`
	EnableFallback   bool    `json:"enable_fallback"`
}

// Conversation 对话结构
type Conversation struct {
	Messages []Message      `json:"messages"`
	Context  Context        `json:"context"`
	Metadata map[string]any `json:"metadata"`
}

// Context 对话上下文
type Context struct {
	Topic           string              `json:"topic"`
	Intent          string              `json:"intent"`
	Entities        map[string][]string `json:"entities"`
	Sentiment       Sentiment           `json:"sentiment"`
	PreviousTopics  []string            `json:"previous_topics"`
	UserPreferences map[string]any      `json:"user_preferences"`
	SessionContext  map[string]any      `json:"session_context"`
	LastInteraction time.Time           `json:"last_interaction"`
}

// IntentAnalysis 意图分析结果
type IntentAnalysis struct {
	PrimaryIntent    string            `json:"primary_intent"`
	SecondaryIntents []string          `json:"secondary_intents"`
	Confidence       float64           `json:"confidence"`
	Parameters       map[string]string `json:"parameters"`
	Categories       []string          `json:"categories"`
}

// Sentiment 情感分析
type Sentiment struct {
	Score    float64   `json:"score"`
	Label    string    `json:"label"`
	Emotions []Emotion `json:"emotions"`
}

// Emotion 情感
type Emotion struct {
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

// SessionMetrics 会话指标
type SessionMetrics struct {
	SessionID        string    `json:"session_id"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	MessageCount     int       `json:"message_count"`
	AvgResponseTime  float64   `json:"avg_response_time"`
	ResolutionRate   float64   `json:"resolution_rate"`
	UserSatisfaction float64   `json:"user_satisfaction"`
	FeedbackCount    int       `json:"feedback_count"`
}

// RagCustomerService RAG客服服务接口
type RagCustomerService interface {
	ProcessMessage(ctx context.Context, session Session, message Message) (Response, error)

	CreateSession(ctx context.Context, userID, platform, kbID string, config SessionConfig) (*Session, error)

	GetSession(ctx context.Context, sessionID string) (*Session, error)

	UpdateSession(ctx context.Context, sessionID string, updates map[string]any) error

	EndSession(ctx context.Context, sessionID string) error

	GetSessionHistory(ctx context.Context, sessionID string, limit int) ([]Message, error)

	ProcessBatchMessages(ctx context.Context, session Session, messages []Message) ([]Response, error)

	UpdateKnowledge(ctx context.Context, kbID string, feedback Feedback) error

	GetSessionMetrics(ctx context.Context, sessionID string) (*SessionMetrics, error)

	ListSessions(ctx context.Context, userID, platform string, status SessionStatus) ([]Session, error)

	GetUserContext(ctx context.Context, userID, platform string) (*Context, error)
}

// DialogManagerInterface 对话管理器接口
type DialogManagerInterface interface {
	CreateSession(ctx context.Context, userID, platform, kbID string, config SessionConfig) (*Session, error)

	GetSession(ctx context.Context, sessionID string) (*Session, error)

	AddMessage(ctx context.Context, sessionID string, message Message) error

	GetConversationHistory(ctx context.Context, sessionID string, limit int) (*Conversation, error)

	UpdateSessionMetadata(ctx context.Context, sessionID string, metadata map[string]any) error

	CloseSession(ctx context.Context, sessionID string) error

	CleanupExpiredSessions(ctx context.Context) error

	ListUserSessions(ctx context.Context, userID, platform string, status SessionStatus) ([]Session, error)
}

// ContextUnderstandingInterface 上下文理解器接口
type ContextUnderstandingInterface interface {
	AnalyzeIntent(ctx context.Context, message string, history []Message) (IntentAnalysis, error)

	ExtractEntities(ctx context.Context, message string) (map[string][]string, error)

	AnalyzeSentiment(ctx context.Context, message string) (Sentiment, error)

	UpdateContext(ctx context.Context, currentContext Context, newMessage Message, intent IntentAnalysis) (Context, error)

	DetectTopicChange(ctx context.Context, currentTopic string, newMessage Message) (bool, string, error)

	GetUserPreferences(ctx context.Context, userID, platform string) (map[string]any, error)

	UpdateUserPreferences(ctx context.Context, userID, platform string, preferences map[string]any) error
}

// ResponseGeneratorInterface 回复生成器接口
type ResponseGeneratorInterface interface {
	GenerateResponse(ctx context.Context, request ResponseGenerationRequest) (string, error)
	GenerateStructuredResponse(ctx context.Context, request ResponseGenerationRequest, schema any) (any, error)
	BuildContextString(results []any, context Context) string
	BuildResponsePrompt(query, contextStr string, session Session, context Context) string
}

// ResponseGenerationRequest 回复生成请求
type ResponseGenerationRequest struct {
	Query         string        `json:"query"`
	Context       Context       `json:"context"`
	SearchResults []any         `json:"search_results"`
	Session       Session       `json:"session"`
	Config        SessionConfig `json:"config"`
	LLMConfig     any           `json:"llm_config"`
}

// QualityAssessmentInterface 质量评估接口
type QualityAssessmentInterface interface {
	EvaluateResponse(ctx context.Context, responseContent, query string, searchResults []any) (float64, error)
	EvaluateRelevance(ctx context.Context, response, query string) (float64, error)
	EvaluateAccuracy(ctx context.Context, response string, referenceSources []string) (float64, error)
	EvaluateCoherence(ctx context.Context, response string) (float64, error)
}

// FeedbackLearningInterface 反馈学习接口
type FeedbackLearningInterface interface {
	ProcessFeedback(ctx context.Context, feedback Feedback) error
	LearnFromFeedback(ctx context.Context, feedback Feedback) error
	GetLearningInsights(ctx context.Context, sessionID string) ([]LearningInsight, error)
	UpdateKnowledgeBase(ctx context.Context, kbID string, insights []LearningInsight) error
}

// LearningInsight 学习洞察
type LearningInsight struct {
	Type           string    `json:"type"`
	Description    string    `json:"description"`
	Confidence     float64   `json:"confidence"`
	Recommendation string    `json:"recommendation"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
}
