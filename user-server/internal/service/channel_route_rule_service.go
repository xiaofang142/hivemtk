package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// channel_route_rule_service.go 路由规则管理（specs/024 T305）。
//
// 校验集中在这里，Controller 只做绑定与状态码：
// 规则写错最典型的形态是「目标词拼错」或「Match 不是合法 JSON」——两者在引擎侧
// 都会被跳过，表现为「配了规则但没生效」，而不会报错。管理面必须在写入时就拒掉。

var (
	// ErrChannelRouteRuleInvalid 规则内容不合法（400）。
	ErrChannelRouteRuleInvalid = errors.New("channel route rule invalid")
	// ErrChannelRouteRuleNotFound 规则不存在或不属于本租户（404）。
	ErrChannelRouteRuleNotFound = errors.New("channel route rule not found")
)

type ChannelRouteRuleService struct {
	repo *repository.ChannelRouteRuleRepository
}

func NewChannelRouteRuleService(repo *repository.ChannelRouteRuleRepository) *ChannelRouteRuleService {
	return &ChannelRouteRuleService{repo: repo}
}

// List 某租户全部规则（按优先级升序）。
func (s *ChannelRouteRuleService) List(ctx context.Context, tenantID string) ([]model.ChannelRouteRule, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	return s.repo.ListByTenant(ctx, tenantID)
}

// Create 新建规则（ID 必须为 0，防止「POST 带 id」变成越权更新）。
func (s *ChannelRouteRuleService) Create(ctx context.Context, tenantID string, rule *model.ChannelRouteRule) error {
	if rule == nil {
		return fmt.Errorf("%w: 规则体为空", ErrChannelRouteRuleInvalid)
	}
	rule.ID = 0
	if err := validateChannelRouteRule(rule); err != nil {
		return err
	}
	return s.repo.Upsert(ctx, tenantID, rule)
}

// Update 更新规则（必须带 id；跨租户 → 404）。
func (s *ChannelRouteRuleService) Update(ctx context.Context, tenantID string, rule *model.ChannelRouteRule) error {
	if rule == nil || rule.ID == 0 {
		return fmt.Errorf("%w: 缺少规则 id", ErrChannelRouteRuleInvalid)
	}
	if err := validateChannelRouteRule(rule); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, tenantID, rule); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrChannelRouteRuleNotFound
		}
		return err
	}
	return nil
}

// Delete 删除规则；未命中（不存在/非本租户）返回 ErrChannelRouteRuleNotFound。
func (s *ChannelRouteRuleService) Delete(ctx context.Context, tenantID string, id uint) error {
	ok, err := s.repo.Delete(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChannelRouteRuleNotFound
	}
	return nil
}

// validateChannelRouteRule 写入期校验（引擎侧的跳过逻辑是兜底，不是许可）。
func validateChannelRouteRule(rule *model.ChannelRouteRule) error {
	rule.Channel = strings.TrimSpace(rule.Channel)
	rule.AccountID = strings.TrimSpace(rule.AccountID)
	rule.EventType = strings.TrimSpace(rule.EventType)
	rule.TargetRef = strings.TrimSpace(rule.TargetRef)
	rule.Note = strings.TrimSpace(rule.Note)
	rule.Match = strings.TrimSpace(rule.Match)

	if rule.Target != "" && !model.IsKnownChannelRouteTarget(rule.Target) {
		return fmt.Errorf("%w: 未知目标 %q（可选 discard/pipeline/agent/plugin）", ErrChannelRouteRuleInvalid, rule.Target)
	}
	// 事件类型：允许空（=*）、通配、或词表内类型。拼错的字面量必须拒掉，
	// 否则「规则永远不命中」会以「配置没生效」的形式报上来，查很久。
	if et := rule.EventType; et != "" && et != "*" && !strings.HasSuffix(et, ".*") {
		base := et
		if !strings.Contains(base, ".") {
			// 家族写法（message / group / bot）放行：引擎按前缀处理。
			if !strings.HasPrefix(base, "message") && !strings.HasPrefix(base, "group") && !strings.HasPrefix(base, "bot") && !strings.HasPrefix(base, "interaction") {
				return fmt.Errorf("%w: 未知事件类型 %q", ErrChannelRouteRuleInvalid, et)
			}
		} else if !model.IsKnownEventType(et) {
			return fmt.Errorf("%w: 未知事件类型 %q", ErrChannelRouteRuleInvalid, et)
		}
	}
	if rule.Match != "" && rule.Match != "{}" {
		var probe map[string]any
		if err := json.Unmarshal([]byte(rule.Match), &probe); err != nil {
			return fmt.Errorf("%w: Match 不是合法 JSON 对象", ErrChannelRouteRuleInvalid)
		}
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if rule.Priority < 0 {
		return fmt.Errorf("%w: priority 不能为负", ErrChannelRouteRuleInvalid)
	}
	if rule.Target == "" {
		rule.Target = model.ChannelRouteTargetPipeline
	}
	if rule.Target == model.ChannelRouteTargetPlugin && rule.TargetRef == "" {
		return fmt.Errorf("%w: plugin 目标必须写 target_ref", ErrChannelRouteRuleInvalid)
	}
	if len(rule.Note) > 255 {
		rule.Note = rule.Note[:255]
	}
	return nil
}
