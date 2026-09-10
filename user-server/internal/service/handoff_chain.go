package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// HandoffRule 升级规则定义
// 规则存储在 system_config_kv 中，key = "handoff_rules"，value 是 JSON 数组
type HandoffRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Order       int    `json:"order"`
	Condition   string `json:"condition"`
	TargetRole  string `json:"target_role"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

type HandoffChainService struct {
	repo *repository.HandoffChainRepository
}

// NewHandoffChainService 创建转派链服务
func NewHandoffChainService() *HandoffChainService {
	return &HandoffChainService{repo: repository.NewHandoffChainRepository(db.GetDB())}
}

// NewHandoffChainServiceWithDB 注入 DB（测试用）
func (s *HandoffChainService) WithDB(d *gorm.DB) *HandoffChainService {
	s.repo = repository.NewHandoffChainRepository(d)
	return s
}

// LoadRules 从 system_config_kv 加载规则（经 repository 收口）
func (s *HandoffChainService) LoadRules(ctx context.Context) ([]HandoffRule, error) {
	if s.repo == nil {
		return defaultHandoffRules(), nil
	}

	raw, err := s.repo.GetHandoffRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("load handoff_rules: %w", err)
	}
	if raw == "" {
		return defaultHandoffRules(), nil
	}
	var rules []HandoffRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		logger.Warnf("[HandoffChain] 解析 handoff_rules JSON 失败，使用默认规则: %v", err)
		return defaultHandoffRules(), nil
	}

	out := make([]HandoffRule, 0, len(rules))
	for _, r := range rules {
		if r.Enabled {
			out = append(out, r)
		}
	}

	sortHandoffRules(out)
	return out, nil
}

// SaveRules 保存规则到 system_config_kv
func (s *HandoffChainService) SaveRules(ctx context.Context, rules []HandoffRule) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	if s.repo == nil {
		return fmt.Errorf("db 未初始化")
	}
	_, err = s.repo.SaveHandoffRules(ctx, string(data))
	return err
}

// CheckRules 对指定会话执行规则链检查
//
// 返回触发的规则列表（已执行）+ 建议动作
func (s *HandoffChainService) CheckRules(ctx context.Context, sessionID string) ([]string, error) {
	rules, err := s.LoadRules(ctx)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}
	if s.repo == nil {
		return nil, nil
	}

	sessRow, err := s.repo.GetHandoffSessionSnapshot(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询会话: %w", err)
	}
	sess := struct {
		ID              string     `gorm:"column:id"`
		Status          string     `gorm:"column:status"`
		CreatedAt       time.Time  `gorm:"column:created_at"`
		ResolvedAt      *time.Time `gorm:"column:updated_at"`
		CsatScore       *int       `gorm:"column:rating"`
		AssignedAgentID *uint      `gorm:"column:agent_id"`
	}{
		ID:              sessRow.ID,
		Status:          sessRow.Status,
		CreatedAt:       sessRow.CreatedAt,
		ResolvedAt:      sessRow.ResolvedAt,
		CsatScore:       sessRow.CsatScore,
		AssignedAgentID: sessRow.AssignedAgentID,
	}

	now := time.Now()
	var triggered []string
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if s.matchRule(r, sess, now) {
			if err := s.applyRule(ctx, r, sessionID); err != nil {
				logger.Warnf("[HandoffChain] 规则 %s 应用失败: %v", r.ID, err)
				continue
			}
			triggered = append(triggered, r.ID)
			logger.Infof("[HandoffChain] 规则触发 rule=%s session=%s action=%s target=%s",
				r.ID, sessionID, r.Action, r.TargetRole)
		}
	}
	return triggered, nil
}

// RunCron 扫描全量未解决会话并执行规则链
// 供 cron 定期调用（建议每 5 分钟）
func (s *HandoffChainService) RunCron(ctx context.Context, limit int) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 200
	}

	rows, err := s.repo.ListUnresolvedSessionIDs(ctx, limit)
	if err != nil {
		return 0, err
	}
	triggeredTotal := 0
	for _, rowID := range rows {
		tr, err := s.CheckRules(ctx, rowID)
		if err != nil {
			logger.Warnf("[HandoffChain] CheckRules session=%s 失败: %v", rowID, err)
			continue
		}
		triggeredTotal += len(tr)
	}
	logger.Infof("[HandoffChain] cron 扫描完成 sessions=%d triggered=%d", len(rows), triggeredTotal)
	return triggeredTotal, nil
}

func (s *HandoffChainService) matchRule(rule HandoffRule, sess struct {
	ID              string     `gorm:"column:id"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ResolvedAt      *time.Time `gorm:"column:updated_at"`
	CsatScore       *int       `gorm:"column:rating"`
	AssignedAgentID *uint      `gorm:"column:agent_id"`
}, now time.Time) bool {

	if sess.Status == "resolved" || sess.Status == "closed" {
		return false
	}
	switch rule.ID {
	case "escalate_24h":

		if sess.ResolvedAt != nil {
			return false
		}
		return now.Sub(sess.CreatedAt) > 24*time.Hour
	case "transfer_low_csat":

		if sess.CsatScore == nil || *sess.CsatScore > 2 {
			return false
		}
		return sess.ResolvedAt == nil
	default:

		return evalSimpleCondition(rule.Condition, sess, now)
	}
}

func (s *HandoffChainService) applyRule(ctx context.Context, rule HandoffRule, sessionID string) error {

	record := model.HandoffDecisionRecord{
		DecisionID:   rule.ID + "_" + sessionID,
		SessionID:    sessionID,
		Reason:       rule.Action + "_by_rule:" + rule.ID,
		ReasonDetail: rule.Description,
		IntentType:   rule.TargetRole,
	}
	_ = s.repo.CreateHandoffDecisionRecord(ctx, &record)

	if rule.Action == "escalate" {
		_ = s.repo.EscalateSession(ctx, sessionID)
	}
	return nil
}

func evalSimpleCondition(cond string, sess interface{}, now time.Time) bool {

	switch {
	case handoffContains(cond, "24h"):

		return false
	}
	return false
}

func handoffContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && (len(sub) == 0 || handoffSearchSubstring(s, sub)))
}

func handoffSearchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func defaultHandoffRules() []HandoffRule {
	return []HandoffRule{
		{
			ID:          "escalate_24h",
			Name:        "24 小时未解决升级到主管",
			Enabled:     true,
			Order:       1,
			Condition:   "unresolved > 24h",
			TargetRole:  "supervisor",
			Action:      "escalate",
			Description: "会话创建超过 24 小时仍未解决，自动升级到 supervisor 处理",
		},
		{
			ID:          "transfer_low_csat",
			Name:        "低分+未解决转派专家",
			Enabled:     true,
			Order:       2,
			Condition:   "csat <= 2 && unresolved",
			TargetRole:  "specialist",
			Action:      "transfer",
			Description: "客户 CSAT 评分 ≤ 2 但会话仍未解决，自动转派到 specialist 跟进",
		},
	}
}

func sortHandoffRules(rules []HandoffRule) {
	for i := 1; i < len(rules); i++ {
		for j := i; j > 0 && rules[j-1].Order > rules[j].Order; j-- {
			rules[j-1], rules[j] = rules[j], rules[j-1]
		}
	}
}
