package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type DialogueMemoryService struct {
	repo       repository.DialogueMemoryRepository
	ms         *MemorySystem
	dispatcher *llm.Dispatcher

	mu     sync.Mutex
	sumBuf map[string][]string
	sumDup map[string]int
}

const (
	shortTermWindow    = 10
	shortTermMsgMaxLen = 1500

	summaryBufferMax     = 12
	summaryDupTrigger    = 2
	summaryJaccardThresh = 0.6
)

// NewDialogueMemoryService 创建对话记忆服务
// db 参数保留以兼容调用方签名（router/sales_engine_factory），内部转换为 DialogueMemoryRepository。
// db 为 nil 时 repo 也为 nil，方法内通过 s.repo == nil 防御。
func NewDialogueMemoryService(db *gorm.DB, dispatcher *llm.Dispatcher) *DialogueMemoryService {
	svc := &DialogueMemoryService{
		dispatcher: dispatcher,
		sumBuf:     map[string][]string{},
		sumDup:     map[string]int{},
	}
	if db != nil {
		svc.repo = repository.NewDialogueMemoryRepositoryWithDB(db)

		svc.ms = &MemorySystem{
			memoryRepo:   repository.NewMemoryRepositoryWithDB(db),
			embeddingSvc: llm.NewEmbeddingService(),
		}
	}
	return svc
}

// GetOrCreateMemory 获取或创建记忆
func (s *DialogueMemoryService) GetOrCreateMemory(ctx context.Context, sessionID, customerID string) (*model.DialogueMemory, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("db is nil")
	}
	mem, err := s.repo.GetDialogueMemoryBySession(ctx, sessionID)
	if err == nil {
		return mem, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	mem = &model.DialogueMemory{
		SessionID:    sessionID,
		CustomerID:   customerID,
		KeyFacts:     model.JSONMap{},
		Objections:   model.JSONArray{},
		IntentTrail:  model.JSONArray{},
		SOPHistory:   model.JSONArray{},
		LastActiveAt: time.Now(),
		MessageCount: 0,
	}
	if err := s.repo.CreateDialogueMemory(ctx, mem); err != nil {
		return nil, err
	}
	return mem, nil
}

// AppendMessage 追加消息并更新记忆
func (s *DialogueMemoryService) AppendMessage(ctx context.Context, sessionID, customerID string, msg dto.Message) error {
	mem, err := s.GetOrCreateMemory(ctx, sessionID, customerID)
	if err != nil {
		return err
	}
	trail := []string{}
	if mem.IntentTrail != nil {
		_ = json.Unmarshal(mustJSON(mem.IntentTrail), &trail)
	}

	mem.MessageCount++
	mem.LastActiveAt = time.Now()
	if msg.Role == "ai" || msg.Role == "agent" {
		mem.LastAction = truncate(msg.Content, 100)
	}

	if s.ms != nil && msg.Content != "" {
		_ = s.ms.L1Append(ctx, sessionID, customerID, msg.Role, msg.Content)
	}

	if s.dispatcher != nil && msg.Content != "" {
		s.offerSummary(ctx, mem, customerID, msg.Content)
	}

	return s.repo.SaveDialogueMemory(ctx, mem)
}

// GetShortTermMemory 获取短期记忆（从 message_hub 取最近 N 条）
func (s *DialogueMemoryService) GetShortTermMemory(ctx context.Context, sessionID string) ([]dto.Message, error) {
	if s.repo == nil {
		return nil, nil
	}
	records, err := s.repo.ListMessageHubByConversation(ctx, sessionID, shortTermWindow)
	if err != nil {
		return nil, err
	}
	msgs := make([]dto.Message, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		role := "user"
		if r.IsAIReply || r.Direction == "outbound" {
			role = "ai"
		}
		msgs = append(msgs, dto.Message{
			Role:      role,
			Content:   truncate(r.Content, shortTermMsgMaxLen),
			Timestamp: r.SentAt,
		})
	}
	return msgs, nil
}

// GetLongTermMemory 获取长期记忆
func (s *DialogueMemoryService) GetLongTermMemory(ctx context.Context, sessionID string) (*model.DialogueMemory, error) {
	return s.GetOrCreateMemory(ctx, sessionID, "")
}

// ListByCustomerID 根据 customerID 获取对话记忆列表
func (s *DialogueMemoryService) ListByCustomerID(ctx context.Context, customerID string, limit int) ([]*model.DialogueMemory, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	if limit <= 0 {
		limit = 10
	}
	return s.repo.ListDialogueMemoriesByCustomer(ctx, customerID, limit)
}

func (s *DialogueMemoryService) UpdateKeyFacts(ctx context.Context, sessionID string, facts map[string]string) error {
	mem, err := s.GetOrCreateMemory(ctx, sessionID, "")
	if err != nil {
		return err
	}
	existing := map[string]string{}
	if mem.KeyFacts != nil {
		_ = json.Unmarshal(mustJSON(mem.KeyFacts), &existing)
	}
	for k, v := range facts {
		existing[k] = v
	}
	data, _ := json.Marshal(existing)
	keyFacts := make(model.JSONMap)
	for k, v := range existing {
		keyFacts[k] = v
	}
	mem.KeyFacts = keyFacts
	if name, ok := facts["name"]; ok && name != "" {
		mem.CustomerName = name
	}
	if phone, ok := facts["phone"]; ok && phone != "" {
		mem.CustomerPhone = phone
	}
	if wechat, ok := facts["wechat"]; ok && wechat != "" {
		mem.CustomerWechat = wechat
	}
	if budget, ok := facts["budget"]; ok && budget != "" {
		mem.Budget = budget
	}
	if demand, ok := facts["demand"]; ok && demand != "" {
		mem.Demand = demand
	}
	_ = data
	if err := s.repo.SaveDialogueMemory(ctx, mem); err != nil {
		return err
	}
	s.syncKeyFactsToMemorySystem(ctx, mem.CustomerID, facts)
	return nil
}
func (s *DialogueMemoryService) RecordObjection(ctx context.Context, sessionID, objectionType, content string) error {
	mem, err := s.GetOrCreateMemory(ctx, sessionID, "")
	if err != nil {
		return err
	}
	objs := []map[string]any{}
	if mem.Objections != nil {
		_ = json.Unmarshal(mustJSON(mem.Objections), &objs)
	}
	objs = append(objs, map[string]any{
		"type":    objectionType,
		"content": content,
		"time":    time.Now(),
	})
	mem.Objections = model.JSONArray(toIfaceSlice(objs))
	if err := s.repo.SaveDialogueMemory(ctx, mem); err != nil {
		return err
	}
	s.syncObjectionToMemorySystem(ctx, mem.CustomerID, objectionType, content)
	return nil
}

// UpdatePurchaseIntent 更新购买意向
func (s *DialogueMemoryService) UpdatePurchaseIntent(ctx context.Context, sessionID, level string) error {
	if level != "high" && level != "medium" && level != "low" {
		level = "low"
	}
	mem, err := s.GetOrCreateMemory(ctx, sessionID, "")
	if err != nil {
		return err
	}
	mem.PurchaseIntent = level
	if err := s.repo.SaveDialogueMemory(ctx, mem); err != nil {
		return err
	}
	s.syncPurchaseIntentToMemorySystem(ctx, mem.CustomerID, level)
	return nil
}

// RecordIntent 记录意图轨迹
func (s *DialogueMemoryService) RecordIntent(ctx context.Context, sessionID, intentType string) error {
	mem, err := s.GetOrCreateMemory(ctx, sessionID, "")
	if err != nil {
		return err
	}
	trail := []string{}
	if mem.IntentTrail != nil {
		_ = json.Unmarshal(mustJSON(mem.IntentTrail), &trail)
	}
	trail = append(trail, intentType)
	if len(trail) > 30 {
		trail = trail[len(trail)-30:]
	}
	mem.IntentTrail = model.JSONArray(toIfaceSliceFromStrings(trail))
	return s.repo.SaveDialogueMemory(ctx, mem)
}

// RecordSOP 记录 SOP 经历
func (s *DialogueMemoryService) RecordSOP(ctx context.Context, sessionID, sopName string) error {
	mem, err := s.GetOrCreateMemory(ctx, sessionID, "")
	if err != nil {
		return err
	}
	hist := []string{}
	if mem.SOPHistory != nil {
		_ = json.Unmarshal(mustJSON(mem.SOPHistory), &hist)
	}
	hist = append(hist, sopName)
	mem.SOPHistory = model.JSONArray(toIfaceSliceFromStrings(hist))
	return s.repo.SaveDialogueMemory(ctx, mem)
}

// BuildContext 构建对话上下文（短期+长期+事实）
func (s *DialogueMemoryService) BuildContext(ctx context.Context, sessionID, customerID string) (string, error) {
	short, _ := s.GetShortTermMemory(ctx, sessionID)
	long, _ := s.GetLongTermMemory(ctx, sessionID)
	var sb strings.Builder
	sb.WriteString("【客户长期记忆】\n")
	if long != nil {
		if long.CustomerName != "" {
			sb.WriteString(fmt.Sprintf("客户姓名: %s\n", long.CustomerName))
		}
		if long.Budget != "" {
			sb.WriteString(fmt.Sprintf("预算: %s\n", long.Budget))
		}
		if long.Demand != "" {
			sb.WriteString(fmt.Sprintf("需求: %s\n", long.Demand))
		}
		if len(long.Objections) > 0 {
			sb.WriteString(fmt.Sprintf("历史异议: %v\n", long.Objections))
		}
		if long.PurchaseIntent != "" {
			sb.WriteString(fmt.Sprintf("购买意向: %s\n", long.PurchaseIntent))
		}
		if long.Summary != "" {
			sb.WriteString(fmt.Sprintf("历史摘要: %s\n", long.Summary))
		}
	}
	sb.WriteString("\n【最近对话】\n")
	for _, m := range short {
		role := "客户"
		if m.Role == "ai" || m.Role == "agent" {
			role = "我"
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}
	return sb.String(), nil
}

func (s *DialogueMemoryService) offerSummary(ctx context.Context, mem *model.DialogueMemory, customerID, content string) bool {
	s.mu.Lock()
	buf := s.sumBuf[mem.SessionID]
	dups := s.sumDup[mem.SessionID]
	for _, b := range buf {
		if keywordJaccard(content, b) >= summaryJaccardThresh {
			dups++
		}
	}
	buf = append(buf, content)
	trigger := len(buf) >= summaryBufferMax || dups >= summaryDupTrigger
	var batch []string
	if trigger {
		batch = buf
		delete(s.sumBuf, mem.SessionID)
		delete(s.sumDup, mem.SessionID)
	} else {
		s.sumBuf[mem.SessionID] = buf
		s.sumDup[mem.SessionID] = dups
	}
	s.mu.Unlock()

	if !trigger {
		return false
	}
	s.updateLongTermSummary(ctx, mem, customerID, batch)
	return true
}

func isCJK(r rune) bool { return r >= 0x2E80 }

func tokenizeKeywords(s string) map[string]struct{} {
	tokens := map[string]struct{}{}
	runes := []rune(strings.ToLower(s))
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		run := runes[start:end]
		hasCJK := false
		for _, r := range run {
			if isCJK(r) {
				hasCJK = true
				break
			}
		}
		if hasCJK {
			if len(run) == 1 {
				tokens[string(run)] = struct{}{}
			}
			for i := 0; i+2 <= len(run); i++ {
				tokens[string(run[i:i+2])] = struct{}{}
			}
		} else {
			tokens[string(run)] = struct{}{}
		}
		start = -1
	}
	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(runes))
	return tokens
}

func keywordJaccard(a, b string) float64 {
	ta, tb := tokenizeKeywords(a), tokenizeKeywords(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func (s *DialogueMemoryService) updateLongTermSummary(ctx context.Context, mem *model.DialogueMemory, customerID string, batch []string) {
	if s.dispatcher == nil || len(batch) == 0 {
		return
	}

	lines := make([]string, 0, len(batch))
	for i, c := range batch {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, truncate(c, shortTermMsgMaxLen)))
	}
	prompt := fmt.Sprintf(`你是销冠对话分析师。请根据以下信息，生成对话的长期摘要和关键事实。

【已有摘要】: %s
【已有事实】: %v
【历史异议】: %v
【意图轨迹】: %v
【近期消息】:
%s

请输出 JSON 格式:
{
  "summary": "50-150 字的对话摘要",
  "key_facts": {"key": "value"},
  "next_action_suggestion": "建议下一步动作"
}`, mem.Summary, mem.KeyFacts, mem.Objections, mem.IntentTrail, strings.Join(lines, "\n"))

	req := llm.DispatchRequest{
		Scenario:    llm.ScenarioLongSummary,
		Prompt:      prompt,
		JSONMode:    true,
		MaxTokens:   500,
		Temperature: 0.3,
	}
	result, err := s.dispatcher.Dispatch(ctx, req)
	if err != nil {
		logger.Warnf("[DialogueMemory] 长期摘要调用失败 session=%s err=%v，重试 1 次", mem.SessionID, err)
		result, err = s.dispatcher.Dispatch(ctx, req)
		if err != nil {
			logger.Errorf("[DialogueMemory] 长期摘要重试仍失败 session=%s err=%v", mem.SessionID, err)
			return
		}
	}
	var parsed struct {
		Summary              string            `json:"summary"`
		KeyFacts             map[string]string `json:"key_facts"`
		NextActionSuggestion string            `json:"next_action_suggestion"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		logger.Errorf("[DialogueMemory] 长期摘要解析失败 session=%s err=%v", mem.SessionID, err)
		return
	}
	mem.Summary = parsed.Summary
	if len(parsed.KeyFacts) > 0 {
		mem.KeyFacts = model.JSONMap(stringMapToIface(parsed.KeyFacts))
	}
	mem.NextActionSuggestion = parsed.NextActionSuggestion

	s.routeSummaryToMemorySystem(ctx, customerID, parsed.Summary, parsed.KeyFacts)
}

func (s *DialogueMemoryService) routeSummaryToMemorySystem(ctx context.Context, customerID, summary string, keyFacts map[string]string) {
	if s.ms == nil || customerID == "" || summary == "" {
		return
	}
	if _, err := s.ms.Remember(ctx, customerID, model.LongTermMemoryFact, summary, 8); err != nil {
		logger.Warnf("[DialogueMemory] 摘要写入 MemorySystem.Remember 失败，降级 L2SaveSummary customer=%s err=%v", customerID, err)
		_ = s.ms.L2SaveSummary(ctx, customerID, summary)
	}
	for k, v := range keyFacts {
		if k == "" || v == "" {
			continue
		}
		if _, err := s.ms.Remember(ctx, customerID, model.LongTermMemoryFact, k+"="+v, 7); err != nil {
			_ = s.ms.L2SaveFact(ctx, customerID, k, v, 7)
		}
	}
}

func (s *DialogueMemoryService) syncKeyFactsToMemorySystem(ctx context.Context, customerID string, facts map[string]string) {
	if s.ms == nil || customerID == "" || len(facts) == 0 {
		return
	}
	for k, v := range facts {
		if k == "" || v == "" {
			continue
		}
		if _, err := s.ms.Remember(ctx, customerID, model.LongTermMemoryPreference, k+"="+v, 7); err != nil {
			logger.Warnf("[DialogueMemory] syncKeyFacts Remember 失败，降级 L2SaveFact customer=%s key=%s err=%v", customerID, k, err)
			_ = s.ms.L2SaveFact(ctx, customerID, k, v, 7)
		}
	}
}

func (s *DialogueMemoryService) syncObjectionToMemorySystem(ctx context.Context, customerID, objectionType, content string) {
	if s.ms == nil || customerID == "" || objectionType == "" {
		return
	}
	meta, _ := json.Marshal(map[string]any{"type": objectionType})
	_ = s.ms.L4Record(ctx, customerID, "objection", content, objectionType, 6,
		map[string]any{"objection_meta": string(meta)})
}

func (s *DialogueMemoryService) syncPurchaseIntentToMemorySystem(ctx context.Context, customerID, level string) {
	if s.ms == nil || customerID == "" {
		return
	}
	_ = s.ms.L4Record(ctx, customerID, "intent", "购买意向="+level, level, 8, nil)
}

var (
	dialogueMemoryOnce sync.Once
	dialogueMemory     *DialogueMemoryService
)

func GetDialogueMemory() *DialogueMemoryService {
	return dialogueMemory
}

func InitDialogueMemory(db *gorm.DB, dispatcher *llm.Dispatcher) *DialogueMemoryService {
	dialogueMemoryOnce.Do(func() {
		dialogueMemory = NewDialogueMemoryService(db, dispatcher)
	})
	return dialogueMemory
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func toIfaceSlice(m []map[string]any) []any {
	out := make([]any, len(m))
	for i, v := range m {
		out[i] = v
	}
	return out
}

func toIfaceSliceFromStrings(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func stringMapToIface(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
