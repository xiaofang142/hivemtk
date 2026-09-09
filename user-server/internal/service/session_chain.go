package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// SessionChainService 会话生命周期链服务
type SessionChainService struct {
	repo *repository.SessionChainRepository
}

// NewSessionChainService 构造
func NewSessionChainService(gdb *gorm.DB) *SessionChainService {
	return &SessionChainService{repo: repository.NewSessionChainRepository(gdb)}
}

// NewSessionChainServiceFromGlobal 便捷构造
func NewSessionChainServiceFromGlobal() *SessionChainService {
	return NewSessionChainService(db.GetDB())
}

// TriggerCSATOnClose 会话关闭时自动下发 CSAT（csat_survey_listener 语义）。
// 由 UpdateSessionStatus 在 resolved/closed 迁移点调用；fire-and-forget 不阻塞。
func (s *SessionChainService) TriggerCSATOnClose(session *model.CustomerSession) {
	if session == nil || session.SessionID == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), utils.RagMetricsTimeout)
		defer cancel()
		csat := NewCSATService()
		if _, err := csat.Trigger(ctx, session.SessionID, "auto"); err != nil {

			return
		}
	}()
}

// AutoResolveConfigKey KV 键
const AutoResolveConfigKey = "session.auto_resolve"

// AutoResolveConfig SLA 配置
type AutoResolveConfig struct {
	Enabled       bool   `json:"enabled"`
	Hours         int    `json:"hours"`
	OnlyClosedOff bool   `json:"only_open_like"`
	AddTag        string `json:"add_tag"`
}

// DefaultAutoResolveConfig 默认关闭
func DefaultAutoResolveConfig() AutoResolveConfig {
	return AutoResolveConfig{Enabled: false, Hours: 72, OnlyClosedOff: true, AddTag: "auto_resolved"}
}

// GetAutoResolveConfig 读配置
func (s *SessionChainService) GetAutoResolveConfig(ctx context.Context) AutoResolveConfig {
	cfg := DefaultAutoResolveConfig()
	raw, err := repository.NewSystemConfigKVRepository().Get(ctx, AutoResolveConfigKey)
	if err != nil || raw == "" {
		return cfg
	}
	var p AutoResolveConfig
	if json.Unmarshal([]byte(raw), &p) == nil {
		if p.Hours <= 0 {
			p.Hours = cfg.Hours
		}
		return p
	}
	return cfg
}

// SaveAutoResolveConfig 存配置
func (s *SessionChainService) SaveAutoResolveConfig(ctx context.Context, cfg *AutoResolveConfig) error {
	if cfg.Hours <= 0 || cfg.Hours > 24*90 {
		return fmt.Errorf("hours 必须在 1-2160")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = repository.NewSystemConfigKVRepository().Upsert(ctx, AutoResolveConfigKey, string(raw))
	return err
}

// RunAutoResolve SLA 扫描（cron 入口）：无活动超时会话 → 打标 + 关闭
func (s *SessionChainService) RunAutoResolve(ctx context.Context) (int, error) {
	cfg := s.GetAutoResolveConfig(ctx)
	if !cfg.Enabled {
		return 0, nil
	}
	threshold := time.Now().Add(-time.Duration(cfg.Hours) * time.Hour)
	sessions, err := s.repo.ListStaleSessions(ctx, threshold)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, sess := range sessions {

		if cfg.AddTag != "" {
			var tags []string
			_ = json.Unmarshal([]byte(sess.Tags), &tags)
			has := false
			for _, t := range tags {
				if t == cfg.AddTag {
					has = true
					break
				}
			}
			if !has {
				tags = append(tags, cfg.AddTag)
				if merged, err := json.Marshal(tags); err == nil {
					_ = s.repo.UpdateSessionTags(ctx, sess.ID, string(merged))
				}
			}
		}
		if err := s.repo.CloseSessionByPK(ctx, sess.ID); err == nil {
			closed++
		}
	}
	return closed, nil
}

// ReopenOnInboundMessage 访客消息落库后调用：resolved/closed 会话自动回 waiting（toggle_status 语义）。
// 返回 true=发生了 reopen。
func (s *SessionChainService) ReopenOnInboundMessage(ctx context.Context, sessionID string) (bool, error) {
	return s.repo.ReopenSessionOnInbound(ctx, sessionID)
}

// GetSession 根据 session_id 获取会话（供 controller 层复用，避免直接 db.GetDB）
func (s *SessionChainService) GetSession(ctx context.Context, sessionID string) (*model.CustomerSession, error) {
	repo := repository.NewCustomerSessionRepositoryWithDB(db.GetDB())
	return repo.GetBySessionID(ctx, sessionID)
}

// 支持的事件
const (
	RuleEventConversationCreated = "conversation_created"
	RuleEventMessageInbound      = "message_inbound"
	RuleEventSessionResolved     = "session_resolved"
)

// 支持的动作
const (
	RuleActAddTag      = "add_tag"
	RuleActSetPriority = "set_priority"
	RuleActAssign      = "assign"
	RuleActClose       = "close"
	RuleActSendMessage = "send_message"
	RuleActAddNote     = "add_note"
	RuleActWebhook     = "webhook"
)

// RuleCondition 条件
type RuleCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// RuleAction 动作
type RuleAction struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// RuleEngineService 规则引擎
type RuleEngineService struct {
	repo   *repository.AutomationRuleRepository
	csPlus *CustomerServicePlusService
	now    func() time.Time
}

// NewRuleEngineService 构造
func NewRuleEngineService(gdb *gorm.DB) *RuleEngineService {
	return &RuleEngineService{
		repo:   repository.NewAutomationRuleRepository(gdb),
		csPlus: NewCustomerServicePlusServiceFromGlobal(),
		now:    time.Now,
	}
}

// NewRuleEngineServiceFromGlobal 便捷构造
func NewRuleEngineServiceFromGlobal() *RuleEngineService { return NewRuleEngineService(db.GetDB()) }

// Create 创建规则
func (s *RuleEngineService) Create(ctx context.Context, r *model.AutomationRule) (*model.AutomationRule, error) {
	if r.Event != RuleEventConversationCreated && r.Event != RuleEventMessageInbound && r.Event != RuleEventSessionResolved {
		return nil, fmt.Errorf("不支持的事件: %s", r.Event)
	}
	var conds []RuleCondition
	if err := json.Unmarshal([]byte(r.Conditions), &conds); err != nil {
		return nil, fmt.Errorf("conditions JSON 非法")
	}
	for _, c := range conds {
		switch c.Op {
		case "eq", "contains", "gt", "lt":
		default:
			return nil, fmt.Errorf("不支持的条件操作符: %s", c.Op)
		}
	}
	var acts []RuleAction
	if err := json.Unmarshal([]byte(r.Actions), &acts); err != nil || len(acts) == 0 {
		return nil, fmt.Errorf("actions JSON 非法或为空")
	}
	for _, a := range acts {
		switch a.Type {
		case RuleActAddTag, RuleActSetPriority, RuleActAssign, RuleActClose, RuleActSendMessage, RuleActAddNote, RuleActWebhook:
		default:
			return nil, fmt.Errorf("不支持的动作: %s", a.Type)
		}
	}
	if err := s.repo.CreateRule(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// List 规则列表
func (s *RuleEngineService) List(ctx context.Context, event string) ([]*model.AutomationRule, error) {
	return s.repo.ListRules(ctx, event)
}

// Delete 删除
func (s *RuleEngineService) Delete(ctx context.Context, id uint) error {
	return s.repo.DeleteRule(ctx, id)
}

// Toggle 启停
func (s *RuleEngineService) Toggle(ctx context.Context, id uint, enabled bool) error {
	return s.repo.ToggleRule(ctx, id, enabled)
}

// DispatchSessionEvent 会话事件入口（session 落库/迁移点调用）
func (s *RuleEngineService) DispatchSessionEvent(ctx context.Context, event, sessionID string, session *model.CustomerSession) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		c, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
		defer cancel()
		s.DispatchWithText(c, event, sessionID, "", session)
	}()
}

// Dispatch 规则匹配执行（inboundText: message_inbound 事件的消息内容，供 content 条件匹配）
func (s *RuleEngineService) Dispatch(ctx context.Context, event, sessionID string, session *model.CustomerSession) {
	s.DispatchWithText(ctx, event, sessionID, "", session)
}

// DispatchWithText 完整入口（带消息内容）
func (s *RuleEngineService) DispatchWithText(ctx context.Context, event, sessionID string, inboundText string, session *model.CustomerSession) {
	rules, err := s.repo.ListEnabledRulesByEvent(ctx, event)
	if err != nil {
		return
	}
	for _, rule := range rules {
		if session == nil || !s.matchConditions(rule, session, inboundText) {
			continue
		}
		if rule.DelayMinutes > 0 {
			pending := &model.RulePendingExecution{
				RuleID: rule.ID, SessionID: sessionID,
				ExecuteAt: s.now().Add(time.Duration(rule.DelayMinutes) * time.Minute),
			}
			_ = s.repo.CreatePendingRuleExecution(ctx, pending)
			continue
		}
		s.executeRule(ctx, rule, sessionID, session)
	}
}

func (s *RuleEngineService) matchConditions(rule *model.AutomationRule, sess *model.CustomerSession, inboundText string) bool {
	var conds []RuleCondition
	if json.Unmarshal([]byte(rule.Conditions), &conds) != nil {
		return true
	}
	for _, c := range conds {
		var actual string
		switch c.Field {
		case "platform":
			actual = string(sess.Platform)
		case "status":
			actual = string(sess.Status)
		case "one_id":
			actual = sess.OneID
		case "account_id":
			actual = sess.AccountID
		case "priority":
			actual = fmt.Sprintf("%d", sess.Priority)
		case "tags":
			actual = sess.Tags
		case "content":
			actual = inboundText
		default:
			return false
		}
		switch c.Op {
		case "eq":
			if actual != c.Value {
				return false
			}
		case "contains":
			if !strings.Contains(actual, c.Value) {
				return false
			}
		case "gt":
			if numActual, err1 := strconv.Atoi(actual); err1 == nil {
				if numExpected, err2 := strconv.Atoi(c.Value); err2 == nil {
					if !(numActual > numExpected) {
						return false
					}
					break
				}
			}
			if !(actual > c.Value) {
				return false
			}
		case "lt":
			if numActual, err1 := strconv.Atoi(actual); err1 == nil {
				if numExpected, err2 := strconv.Atoi(c.Value); err2 == nil {
					if !(numActual < numExpected) {
						return false
					}
					break
				}
			}
			if !(actual < c.Value) {
				return false
			}
		case "ge":
			if numActual, err1 := strconv.Atoi(actual); err1 == nil {
				if numExpected, err2 := strconv.Atoi(c.Value); err2 == nil {
					if !(numActual >= numExpected) {
						return false
					}
					break
				}
			}
			if !(actual >= c.Value) {
				return false
			}
		case "le":
			if numActual, err1 := strconv.Atoi(actual); err1 == nil {
				if numExpected, err2 := strconv.Atoi(c.Value); err2 == nil {
					if !(numActual <= numExpected) {
						return false
					}
					break
				}
			}
			if !(actual <= c.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s *RuleEngineService) executeRule(ctx context.Context, rule *model.AutomationRule, sessionID string, sess *model.CustomerSession) {
	var acts []RuleAction
	if json.Unmarshal([]byte(rule.Actions), &acts) != nil {
		return
	}
	for _, a := range acts {
		var err error
		switch a.Type {
		case RuleActAddTag:
			var tags []string
			_ = json.Unmarshal([]byte(sess.Tags), &tags)
			has := false
			for _, t := range tags {
				if t == a.Value {
					has = true
					break
				}
			}
			if !has {
				tags = append(tags, a.Value)
				if merged, jm := json.Marshal(tags); jm == nil {
					err = s.repo.UpdateSessionFieldsBySessionID(ctx, sessionID, "tags", string(merged))
				}
			}
		case RuleActSetPriority:
			lvl := 0
			fmt.Sscanf(a.Value, "%d", &lvl)
			err = s.repo.UpdateSessionFieldsBySessionID(ctx, sessionID, "priority", lvl)
		case RuleActAssign:
			err = s.repo.UpdateSessionFieldsBySessionID(ctx, sessionID, "agent_id", a.Value)
		case RuleActClose:
			err = s.repo.UpdateSessionFieldsBySessionID(ctx, sessionID, "status", model.SessionStatusClosed)
		case RuleActSendMessage:
			now := time.Now()
			rec := &model.MessageHub{
				Platform:       string(sess.Platform),
				MsgID:          fmt.Sprintf("rule_%s_%s_%d", rule.Name, sessionID, now.UnixNano()),
				AccountID:      sess.AccountID,
				Direction:      "outbound",
				Status:         "pending",
				MsgType:        "text",
				SenderID:       "system",
				SenderName:     "自动化",
				Content:        a.Value,
				ConversationID: sessionID,
				TraceID:        "rule",
				SentAt:         now,
			}
			err = s.repo.CreateRuleOutboundMessage(ctx, rec)
		case RuleActAddNote:
			m := NewSessionMessageRepository()
			err = m.Create(ctx, &model.SessionMessage{
				SessionID: sessionID, Content: fmt.Sprintf("[自动化:%s] %s", rule.Name, a.Value),
				SenderType: "staff", SenderName: "自动化", IsInternal: true,
			})
		case RuleActWebhook:
			PublishWebhookEvent(ctx, "rule.triggered", map[string]any{
				"rule": rule.Name, "session_id": sessionID, "action": a.Type, "value": a.Value,
			})
		}
		if err != nil {
			slog.Warn("[RuleEngine] 动作执行失败", "rule", rule.Name, "action", a.Type, "err", err)
			return
		}
	}
	_ = s.repo.IncrementRuleRunCount(ctx, rule)
}

// ProcessPendingRules 延迟规则复核（cron 入口）
func (s *RuleEngineService) ProcessPendingRules(ctx context.Context) (int, error) {
	pendings, err := s.repo.ListDuePendingExecutions(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	done := 0
	for _, p := range pendings {
		rule, err := s.repo.GetRuleByID(ctx, p.RuleID)
		if err != nil || !rule.Enabled {
			_ = s.repo.UpdatePendingExecutionStatus(ctx, p.ID, "failed")
			continue
		}
		sess, err := s.repo.GetSessionBySessionID(ctx, p.SessionID)
		if err != nil {
			_ = s.repo.UpdatePendingExecutionStatus(ctx, p.ID, "failed")
			continue
		}
		if !s.matchConditions(rule, sess, "") {
			_ = s.repo.UpdatePendingExecutionStatus(ctx, p.ID, "done")
			done++
			continue
		}
		s.executeRule(ctx, rule, p.SessionID, sess)
		_ = s.repo.UpdatePendingExecutionStatus(ctx, p.ID, "done")
		done++
	}
	return done, nil
}

// SessionMessageRepoAlias 别名（executeRule AddNote 用）
func NewSessionMessageRepository() *repository.SessionMessageRepository {
	return repository.NewSessionMessageRepository()
}
