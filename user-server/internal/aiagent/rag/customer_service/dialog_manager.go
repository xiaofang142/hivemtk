package ragcustomerservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// InMemoryDialogManager 内存对话管理器实现
type InMemoryDialogManager struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
	config   *DialogManagerConfig
}

// DialogManagerConfig 对话管理器配置
type DialogManagerConfig struct {
	DefaultMaxHistoryLength int           `json:"default_max_history_length"`
	DefaultSessionTimeout   time.Duration `json:"default_session_timeout"`
	SessionCleanupInterval  time.Duration `json:"session_cleanup_interval"`
}

// NewInMemoryDialogManager 创建新的内存对话管理器
func NewInMemoryDialogManager(config *DialogManagerConfig) *InMemoryDialogManager {
	if config == nil {
		config = &DialogManagerConfig{}
	}
	if config.DefaultMaxHistoryLength <= 0 {
		config.DefaultMaxHistoryLength = 10
	}
	if config.DefaultSessionTimeout <= 0 {
		config.DefaultSessionTimeout = 30 * time.Minute
	}
	if config.SessionCleanupInterval <= 0 {
		config.SessionCleanupInterval = 5 * time.Minute
	}

	manager := &InMemoryDialogManager{
		sessions: make(map[string]*Session),
		config:   config,
	}

	go manager.startSessionCleanup()

	return manager
}

// CreateSession 创建会话
func (dm *InMemoryDialogManager) CreateSession(ctx context.Context, userID, platform, kbID string, config SessionConfig) (*Session, error) {
	if userID == "" {
		return nil, errors.New("userID cannot be empty")
	}

	if platform == "" {
		return nil, errors.New("platform cannot be empty")
	}

	if kbID == "" {
		return nil, errors.New("kbID cannot be empty")
	}

	sessionID := generateSessionID(userID, platform)

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		Platform:  platform,
		KBID:      kbID,
		Status:    SessionActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata: map[string]any{
			"last_activity": time.Now(),
		},
		Conversation: &Conversation{
			Messages: []Message{},
			Context:  Context{},
			Metadata: map[string]any{},
		},
		Config: config,
	}

	if session.Config.MaxHistoryLength == 0 {
		session.Config.MaxHistoryLength = dm.config.DefaultMaxHistoryLength
	}

	if session.Config.Timeout == 0 {
		session.Config.Timeout = int(dm.config.DefaultSessionTimeout.Seconds())
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	dm.sessions[sessionID] = session

	return session, nil
}

// GetSession 获取会话
func (dm *InMemoryDialogManager) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, errors.New("sessionID cannot be empty")
	}

	dm.mutex.RLock()
	session, exists := dm.sessions[sessionID]
	dm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	dm.updateLastActivity(sessionID)

	return session, nil
}

// AddMessage 添加消息到会话
func (dm *InMemoryDialogManager) AddMessage(ctx context.Context, sessionID string, message Message) error {
	if sessionID == "" {
		return errors.New("sessionID cannot be empty")
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	session, exists := dm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if session.Status != SessionActive {
		return fmt.Errorf("session %s is not active", sessionID)
	}

	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now()
	}

	session.Conversation.Messages = append(session.Conversation.Messages, message)

	if len(session.Conversation.Messages) > session.Config.MaxHistoryLength {
		session.Conversation.Messages = session.Conversation.Messages[len(session.Conversation.Messages)-session.Config.MaxHistoryLength:]
	}

	session.UpdatedAt = time.Now()
	session.Conversation.Metadata["last_message_time"] = message.Timestamp

	return nil
}

// GetConversationHistory 获取对话历史
func (dm *InMemoryDialogManager) GetConversationHistory(ctx context.Context, sessionID string, limit int) (*Conversation, error) {
	if sessionID == "" {
		return nil, errors.New("sessionID cannot be empty")
	}

	dm.mutex.RLock()
	session, exists := dm.sessions[sessionID]
	dm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	history := &Conversation{
		Messages: make([]Message, len(session.Conversation.Messages)),
		Context:  session.Conversation.Context,
		Metadata: session.Conversation.Metadata,
	}

	copy(history.Messages, session.Conversation.Messages)

	if limit > 0 && limit < len(history.Messages) {
		startIdx := len(history.Messages) - limit
		history.Messages = history.Messages[startIdx:]
	}

	dm.updateLastActivity(sessionID)

	return history, nil
}

// UpdateSessionMetadata 更新会话元数据
func (dm *InMemoryDialogManager) UpdateSessionMetadata(ctx context.Context, sessionID string, metadata map[string]any) error {
	if sessionID == "" {
		return errors.New("sessionID cannot be empty")
	}

	if len(metadata) == 0 {
		return errors.New("metadata cannot be empty")
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	session, exists := dm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}

	for key, value := range metadata {
		session.Metadata[key] = value
	}

	session.UpdatedAt = time.Now()

	return nil
}

// CloseSession 关闭会话
func (dm *InMemoryDialogManager) CloseSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.New("sessionID cannot be empty")
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	session, exists := dm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.Status = SessionClosed
	session.UpdatedAt = time.Now()
	session.Metadata["closed_at"] = time.Now()

	return nil
}

// CleanupExpiredSessions 清理会话
func (dm *InMemoryDialogManager) CleanupExpiredSessions(ctx context.Context) error {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	now := time.Now()
	var expiredSessions []string

	for sessionID, session := range dm.sessions {
		lastActivity := session.Metadata["last_activity"]
		if lastActivityTime, ok := lastActivity.(time.Time); ok {
			timeoutDuration := time.Duration(session.Config.Timeout) * time.Second
			if now.Sub(lastActivityTime) > timeoutDuration {
				expiredSessions = append(expiredSessions, sessionID)
			}
		}
	}

	for _, sessionID := range expiredSessions {
		delete(dm.sessions, sessionID)
	}

	return nil
}

// ListUserSessions 列出用户会话
func (dm *InMemoryDialogManager) ListUserSessions(ctx context.Context, userID, platform string, status SessionStatus) ([]Session, error) {
	if userID == "" {
		return nil, errors.New("userID cannot be empty")
	}

	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	var sessions []Session

	for _, session := range dm.sessions {
		if session.UserID == userID &&
			(platform == "" || session.Platform == platform) &&
			(status == "" || session.Status == status) {
			sessions = append(sessions, *session)
		}
	}

	return sessions, nil
}

func (dm *InMemoryDialogManager) startSessionCleanup() {
	ticker := time.NewTicker(dm.config.SessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		if err := dm.CleanupExpiredSessions(ctx); err != nil {
			logger.Warnf("[DialogManager] 清理过期会话失败: %v", err)
		}
	}
}

func (dm *InMemoryDialogManager) updateLastActivity(sessionID string) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if session, exists := dm.sessions[sessionID]; exists {
		session.Metadata["last_activity"] = time.Now()
		session.UpdatedAt = time.Now()
	}
}

// generateSessionID 后缀里的随机段不可省：本机实测 UnixNano 粒度粗于纳秒（同参连调 20000 次
// 只得 5136 个不同值），而 sessions 是 map，同 (user,platform) 背靠背两次建会话会撞进同一个 key，
// 先建的会话被静默覆盖。随机段同时消掉了"按时钟猜他人会话 ID"的面。
func generateSessionID(userID, platform string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%s_%d_%s", userID, platform, time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// ContextUnderstandingServiceImpl 上下文理解服务实现
type ContextUnderstandingServiceImpl struct {
	config *ContextUnderstandingConfig
}

// ContextUnderstandingConfig 上下文理解配置
type ContextUnderstandingConfig struct {
	IntentRecognitionThreshold float64 `json:"intent_recognition_threshold"`
	EntityExtractionEnabled    bool    `json:"entity_extraction_enabled"`
	SentimentAnalysisEnabled   bool    `json:"sentiment_analysis_enabled"`
	TopicDetectionEnabled      bool    `json:"topic_detection_enabled"`
}

// NewContextUnderstandingService 创建新的上下文理解服务
func NewContextUnderstandingService(config *ContextUnderstandingConfig) *ContextUnderstandingServiceImpl {
	if config == nil {
		config = &ContextUnderstandingConfig{
			IntentRecognitionThreshold: 0.6,
			EntityExtractionEnabled:    true,
			SentimentAnalysisEnabled:   true,
			TopicDetectionEnabled:      true,
		}
	}

	return &ContextUnderstandingServiceImpl{
		config: config,
	}
}

// AnalyzeIntent 分析用户意图
func (cus *ContextUnderstandingServiceImpl) AnalyzeIntent(ctx context.Context, message string, history []Message) (IntentAnalysis, error) {
	if message == "" {
		return IntentAnalysis{}, errors.New("message cannot be empty")
	}

	intent := IntentAnalysis{
		Confidence: 0.8,
	}

	lowerMsg := toLower(message)

	if containsAny(lowerMsg, []string{"你好", "您好", "hi", "hello"}) {
		intent.PrimaryIntent = "greeting"
		intent.Categories = []string{"social"}
	} else if containsAny(lowerMsg, []string{"产品", "商品", "购买", "买", "价格", "多少钱", "优惠"}) {
		intent.PrimaryIntent = "product_inquiry"
		intent.Categories = []string{"sales", "support"}
	} else if containsAny(lowerMsg, []string{"订单", "发货", "物流", "快递", "配送"}) {
		intent.PrimaryIntent = "order_inquiry"
		intent.Categories = []string{"support", "logistics"}
	} else if containsAny(lowerMsg, []string{"售后", "退货", "退款", "投诉", "问题"}) {
		intent.PrimaryIntent = "complaint_support"
		intent.Categories = []string{"support", "complaint"}
	} else if containsAny(lowerMsg, []string{"谢谢", "感谢", "好的", "可以", "满意"}) {
		intent.PrimaryIntent = "positive_feedback"
		intent.Categories = []string{"social", "feedback"}
	} else {
		intent.PrimaryIntent = "general_inquiry"
		intent.Categories = []string{"general"}
		intent.Confidence = 0.6
	}

	parameters := extractParameters(message)
	intent.Parameters = parameters

	return intent, nil
}

// ExtractEntities 提取实体
func (cus *ContextUnderstandingServiceImpl) ExtractEntities(ctx context.Context, message string) (map[string][]string, error) {
	if message == "" {
		return nil, errors.New("message cannot be empty")
	}

	entities := make(map[string][]string)

	if !cus.config.EntityExtractionEnabled {
		return entities, nil
	}

	lowerMsg := toLower(message)

	timeEntities := extractTimeEntities(lowerMsg)
	if len(timeEntities) > 0 {
		entities["time"] = timeEntities
	}

	productEntities := extractProductEntities(lowerMsg)
	if len(productEntities) > 0 {
		entities["product"] = productEntities
	}

	brandEntities := extractBrandEntities(lowerMsg)
	if len(brandEntities) > 0 {
		entities["brand"] = brandEntities
	}

	return entities, nil
}

// AnalyzeSentiment 情感分析
func (cus *ContextUnderstandingServiceImpl) AnalyzeSentiment(ctx context.Context, message string) (Sentiment, error) {
	if message == "" {
		return Sentiment{}, errors.New("message cannot be empty")
	}

	if !cus.config.SentimentAnalysisEnabled {
		return Sentiment{Score: 0.0, Label: "neutral"}, nil
	}

	score := calculateSentimentScore(message)
	label := getSentimentLabel(score)

	emotions := detectEmotions(message)

	return Sentiment{
		Score:    score,
		Label:    label,
		Emotions: emotions,
	}, nil
}

// UpdateContext 更新上下文
func (cus *ContextUnderstandingServiceImpl) UpdateContext(ctx context.Context, currentContext Context, newMessage Message, intent IntentAnalysis) (Context, error) {
	updatedContext := currentContext

	// 值拷贝共享 map 与切片底层数组：本函数往两者里写，调用方持有的原 Context 会被无赋值改写。
	if currentContext.Entities != nil {
		entities := make(map[string][]string, len(currentContext.Entities))
		for key, values := range currentContext.Entities {
			entities[key] = values
		}
		updatedContext.Entities = entities
	}
	if currentContext.PreviousTopics != nil {
		previousTopics := make([]string, len(currentContext.PreviousTopics), len(currentContext.PreviousTopics)+1)
		copy(previousTopics, currentContext.PreviousTopics)
		updatedContext.PreviousTopics = previousTopics
	}

	if cus.config.TopicDetectionEnabled {
		changed, newTopic, err := cus.DetectTopicChange(ctx, currentContext.Topic, newMessage)
		if err == nil && changed {
			updatedContext.Topic = newTopic
			updatedContext.PreviousTopics = append(updatedContext.PreviousTopics, currentContext.Topic)
		}
	}

	if intent.PrimaryIntent != "" {
		updatedContext.Intent = intent.PrimaryIntent
	}

	if len(intent.Parameters) > 0 {
		if updatedContext.Entities == nil {
			updatedContext.Entities = make(map[string][]string)
		}
		for param, value := range intent.Parameters {
			updatedContext.Entities[param] = []string{value}
		}
	}

	sentiment, err := cus.AnalyzeSentiment(ctx, newMessage.Content)
	if err == nil {
		updatedContext.Sentiment = sentiment
	}

	updatedContext.LastInteraction = time.Now()

	return updatedContext, nil
}

// DetectTopicChange 检测话题变更
func (cus *ContextUnderstandingServiceImpl) DetectTopicChange(ctx context.Context, currentTopic string, newMessage Message) (bool, string, error) {
	if !cus.config.TopicDetectionEnabled {
		return false, currentTopic, nil
	}

	intent, err := cus.AnalyzeIntent(ctx, newMessage.Content, []Message{})
	if err != nil {
		return false, currentTopic, err
	}

	isTopicChange := !isRelatedTopic(currentTopic, intent.PrimaryIntent)

	if isTopicChange {
		return true, intent.PrimaryIntent, nil
	}

	return false, currentTopic, nil
}

// GetUserPreferences 获取用户偏好
func (cus *ContextUnderstandingServiceImpl) GetUserPreferences(ctx context.Context, userID, platform string) (map[string]any, error) {
	return make(map[string]any), nil
}

// UpdateUserPreferences 更新用户偏好
func (cus *ContextUnderstandingServiceImpl) UpdateUserPreferences(ctx context.Context, userID, platform string, preferences map[string]any) error {
	return nil
}

func toLower(s string) string {
	result := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			result += string(r + ('a' - 'A'))
		} else {
			result += string(r)
		}
	}
	return result
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if contains(text, keyword) {
			return true
		}
	}
	return false
}

func contains(text, substr string) bool {
	return len(text) >= len(substr) && findSubstring(text, substr) != -1
}

func findSubstring(text, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(text) < len(substr) {
		return -1
	}

	for i := 0; i <= len(text)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if text[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func extractParameters(message string) map[string]string {
	parameters := make(map[string]string)

	lowerMsg := toLower(message)

	if idx := findSubstring(lowerMsg, "订单号"); idx != -1 {
		orderNum := extractOrderNumber(lowerMsg[idx:])
		if orderNum != "" {
			parameters["order_number"] = orderNum
		}
	}

	if idx := findSubstring(lowerMsg, "商品"); idx != -1 {
		productName := extractProductName(lowerMsg[idx:])
		if productName != "" {
			parameters["product_name"] = productName
		}
	}

	return parameters
}

func extractTimeEntities(message string) []string {
	var entities []string

	timePatterns := []string{"今天", "明天", "后天", "昨天", "前天", "本周", "下周", "本月", "下月"}

	for _, pattern := range timePatterns {
		if contains(message, pattern) {
			entities = append(entities, pattern)
		}
	}

	return entities
}

func extractProductEntities(message string) []string {
	var entities []string

	productKeywords := []string{"裙子", "衣服", "鞋子", "包包", "手机", "电脑", "书籍", "食品"}

	for _, keyword := range productKeywords {
		if contains(message, keyword) {
			entities = append(entities, keyword)
		}
	}

	return entities
}

func extractBrandEntities(message string) []string {
	var entities []string

	brandKeywords := []string{"苹果", "华为", "小米", "耐克", "阿迪达斯", "优衣库", "星巴克"}

	for _, keyword := range brandKeywords {
		if contains(message, keyword) {
			entities = append(entities, keyword)
		}
	}

	return entities
}

func calculateSentimentScore(message string) float64 {
	positiveWords := []string{"好", "棒", "不错", "满意", "喜欢", "推荐", "值得", "惊喜"}
	negativeWords := []string{"差", "烂", "不好", "失望", "讨厌", "糟糕", "坑", "贵"}
	negationPrefixes := []string{"不", "没", "无", "未", "非", "别"}

	positiveCount := 0
	negativeCount := 0

	lowerMsg := toLower(message)

	for _, word := range negativeWords {
		if contains(lowerMsg, toLower(word)) {
			negativeCount++
		}
	}

	// 正面词逐次判定"是否被否定前缀紧挨着"：整张负面词表里没有「不喜欢/不满意/不推荐」这些组合，
	// 只按子串命中会把「不好」里的「好」也计成正面，正负抵消成 0 ⇒ 所有「不+正面词」都判成中性。
	// 表内固定正面词（「不错」）按整词先命中且前面不是否定前缀，不会被这条规则误伤。
	for _, word := range positiveWords {
		w := toLower(word)
		positiveHit, negatedHit := false, false
		for start := 0; start+len(w) <= len(lowerMsg); start++ {
			if lowerMsg[start:start+len(w)] != w {
				continue
			}
			if hasNegationPrefix(lowerMsg, start, negationPrefixes) {
				negatedHit = true
				continue
			}
			positiveHit = true
			break
		}
		switch {
		case positiveHit:
			positiveCount++
		case negatedHit:
			negativeCount++
		}
	}

	total := positiveCount + negativeCount
	if total == 0 {
		return 0.0
	}

	return float64(positiveCount-negativeCount) / float64(total)
}

func hasNegationPrefix(msg string, start int, prefixes []string) bool {
	for _, p := range prefixes {
		if start >= len(p) && msg[start-len(p):start] == p {
			return true
		}
	}
	return false
}

func getSentimentLabel(score float64) string {
	if score > 0.1 {
		return "positive"
	} else if score < -0.1 {
		return "negative"
	}
	return "neutral"
}

func detectEmotions(message string) []Emotion {
	var emotions []Emotion

	lowerMsg := toLower(message)

	joyWords := []string{"开心", "快乐", "高兴", "愉快", "兴奋"}
	if hasAnyWords(lowerMsg, joyWords) {
		emotions = append(emotions, Emotion{Type: "joy", Score: 0.8})
	}

	angerWords := []string{"生气", "愤怒", "气愤", "恼火", "烦躁"}
	if hasAnyWords(lowerMsg, angerWords) {
		emotions = append(emotions, Emotion{Type: "anger", Score: 0.7})
	}

	sadnessWords := []string{"难过", "伤心", "悲伤", "沮丧", "失落"}
	if hasAnyWords(lowerMsg, sadnessWords) {
		emotions = append(emotions, Emotion{Type: "sadness", Score: 0.6})
	}

	fearWords := []string{"害怕", "恐惧", "担心", "忧虑", "紧张"}
	if hasAnyWords(lowerMsg, fearWords) {
		emotions = append(emotions, Emotion{Type: "fear", Score: 0.5})
	}

	return emotions
}

func hasAnyWords(text string, words []string) bool {
	for _, word := range words {
		if contains(text, toLower(word)) {
			return true
		}
	}
	return false
}

func isRelatedTopic(currentTopic, newIntent string) bool {
	if currentTopic == "" {
		return false
	}

	salesTopics := []string{"product_inquiry", "order_inquiry", "pricing", "sales"}
	supportTopics := []string{"complaint_support", "technical_support", "troubleshooting"}

	currentIsSales := containsAny(currentTopic, salesTopics)
	newIsSales := containsAny(newIntent, salesTopics)

	currentIsSupport := containsAny(currentTopic, supportTopics)
	newIsSupport := containsAny(newIntent, supportTopics)

	return (currentIsSales && newIsSales) || (currentIsSupport && newIsSupport)
}

func extractOrderNumber(text string) string {
	var run []byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			run = append(run, c)
			continue
		}
		if len(run) > 0 {
			break
		}
	}
	return string(run)
}

func extractProductName(text string) string {
	if contains(text, "裙子") {
		return "裙子"
	} else if contains(text, "手机") {
		return "手机"
	}
	return ""
}
