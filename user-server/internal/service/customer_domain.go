package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	_type "hivemtk-user/internal/pkg/utils/type"
	"hivemtk-user/internal/repository"
)

func customerTagGetRule(t *model.CustomerTag) map[string]any {
	if t.Rule == "" {
		return map[string]any{}
	}
	var rule map[string]any
	if err := json.Unmarshal([]byte(t.Rule), &rule); err != nil {
		return map[string]any{}
	}
	return rule
}

func customerTagSetRule(t *model.CustomerTag, rule map[string]any) error {
	if rule == nil {
		rule = map[string]any{}
	}
	jsonData, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	t.Rule = string(jsonData)
	return nil
}

// UserTagService 用户标签门面服务
type UserTagService struct {
	tagRepo repository.UserTagRepository
}

// NewUserTagService 创建用户标签门面服务
func NewUserTagService() *UserTagService {
	return &UserTagService{tagRepo: repository.NewUserTagRepository()}
}

// ErrUserTagInvalid 标签入参非法（空标签 / 超长 / 数量超限）。
var ErrUserTagInvalid = errors.New("标签非法")

// 标签约束与 user_tags.tag_name（varchar(50)）保持一致，避免把 DB 层的
// 「value too long for type character varying(50)」当成 500 抛给前端。
const (
	MaxUserTagRunes = 50
	MaxUserTags     = 100
)

// NormalizeUserTags 去空白、去空串、去重并校验长度与数量。
func NormalizeUserTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > MaxUserTagRunes {
			return nil, fmt.Errorf("%w：标签「%s…」超过 %d 个字符（当前 %d）", ErrUserTagInvalid, truncateRunes(t, 12), MaxUserTagRunes, utf8.RuneCountInString(t))
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) > MaxUserTags {
		return nil, fmt.Errorf("%w：一次最多设置 %d 个标签（当前 %d）", ErrUserTagInvalid, MaxUserTags, len(out))
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ReplaceUserTags 覆盖式更新用户标签，返回最终标签列表。
//
// 先校验再删旧数据：原实现「先全删再插入」，插入失败（例如标签超长触发
// varchar(50) 约束）时用户的标签已被清空，等于把一次参数错误放大成数据丢失。
func (s *UserTagService) ReplaceUserTags(ctx context.Context, userID string, tags []string) ([]string, error) {
	normalized, err := NormalizeUserTags(tags)
	if err != nil {
		return nil, err
	}
	if err := s.tagRepo.DeleteTagsByUser(ctx, userID); err != nil {
		return nil, err
	}
	if err := s.tagRepo.AddTags(ctx, userID, normalized); err != nil {
		return nil, err
	}
	return s.tagRepo.GetTagsByUser(ctx, userID)
}

// AddUserTag 添加单个用户标签，返回最终标签列表
func (s *UserTagService) AddUserTag(ctx context.Context, userID, tag string) ([]string, error) {
	if err := s.tagRepo.AddTag(ctx, userID, tag); err != nil {
		return nil, err
	}
	return s.tagRepo.GetTagsByUser(ctx, userID)
}

// RemoveUserTag 移除单个用户标签，返回最终标签列表
func (s *UserTagService) RemoveUserTag(ctx context.Context, userID, tag string) ([]string, error) {
	if err := s.tagRepo.RemoveTag(ctx, userID, tag); err != nil {
		return nil, err
	}
	return s.tagRepo.GetTagsByUser(ctx, userID)
}

// GetUserTags 获取用户标签列表
func (s *UserTagService) GetUserTags(ctx context.Context, userID string) ([]string, error) {
	tags, err := s.tagRepo.GetTagsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// UserProfileService 客户档案门面服务
type UserProfileService struct {
	userRepo repository.UserRepository
}

// NewUserProfileService 创建客户档案门面服务
func NewUserProfileService() *UserProfileService {
	return &UserProfileService{userRepo: repository.NewUserRepository()}
}

// UserUpdateInput 客户更新输入（字段均为可选，空值表示不更新）
type UserUpdateInput struct {
	RealName *string `json:"real_name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	Status   *int    `json:"status"`
}

// UpdateUserByID 按 ID 更新客户字段，返回更新后的客户基础信息
func (s *UserProfileService) UpdateUserByID(ctx context.Context, userID string, input UserUpdateInput) (*UserBasicView, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID == "" {
		return nil, errors.New("客户不存在")
	}
	updated := false
	if input.RealName != nil && *input.RealName != "" {
		user.RealName = *input.RealName
		updated = true
	}
	if input.Email != nil {
		user.Email = *input.Email
		updated = true
	}
	if input.Phone != nil {
		user.Phone = *input.Phone
		updated = true
	}
	if input.Status != nil {
		user.Status = _type.UserStatusType(*input.Status)
		updated = true
	}
	if !updated {
		return nil, errors.New("没有可更新的字段")
	}
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	return &UserBasicView{
		ID:         user.ID,
		Username:   user.Username,
		RealName:   user.RealName,
		Email:      user.Email,
		Phone:      user.Phone,
		Status:     user.Status,
		UpdateTime: user.UpdateTime,
	}, nil
}

// UserBasicView 客户基础信息视图（脱离 model 的 DTO）
type UserBasicView struct {
	ID         string               `json:"id"`
	Username   string               `json:"username"`
	RealName   string               `json:"real_name"`
	Email      string               `json:"email"`
	Phone      string               `json:"phone"`
	Status     _type.UserStatusType `json:"status"`
	UpdateTime int64                `json:"update_time"`
}

// TagRuleService 自动标签规则门面服务
type TagRuleService struct {
	tagRepo repository.CustomerTagRepository
}

// NewTagRuleService 创建自动标签规则门面服务
func NewTagRuleService() *TagRuleService {
	return &TagRuleService{tagRepo: repository.NewCustomerTagRepository()}
}

// TagRuleItem 标签规则响应项（脱离 model 的 DTO）
type TagRuleItem struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category string         `json:"category"`
	Source   string         `json:"source"`
	Rule     map[string]any `json:"rule"`
	Active   bool           `json:"active"`
	Priority int            `json:"priority"`
}

// ListTagRules 获取自动标签规则列表
func (s *TagRuleService) ListTagRules(ctx context.Context) ([]TagRuleItem, error) {
	tags, err := s.tagRepo.ListAutoTags(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]TagRuleItem, 0, len(tags))
	for i, t := range tags {
		rules = append(rules, TagRuleItem{
			ID:       t.ID,
			Name:     t.Name,
			Category: string(t.Category),
			Source:   string(t.Source),
			Rule:     customerTagGetRule(t),
			Active:   true,
			Priority: i + 1,
		})
	}
	return rules, nil
}

// SaveTagRuleInput 创建/更新标签规则输入
type SaveTagRuleInput struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category string         `json:"category"`
	Rule     map[string]any `json:"rule"`
	Active   *bool          `json:"active"`
}

// TagRuleSaveResult 标签规则保存结果
type TagRuleSaveResult struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category string         `json:"category"`
	Source   string         `json:"source"`
	Rule     map[string]any `json:"rule"`
	Active   bool           `json:"active"`
}

// SaveTagRule 创建或更新自动标签规则（ID 为空则新建）
func (s *TagRuleService) SaveTagRule(ctx context.Context, input SaveTagRuleInput) (*TagRuleSaveResult, error) {
	tag := &model.CustomerTag{
		Name:     input.Name,
		Category: model.TagCategory(input.Category),
		Source:   model.TagSourceAuto,
	}
	if err := customerTagSetRule(tag, input.Rule); err != nil {
		return nil, err
	}
	if input.ID != "" {
		tag.ID = input.ID
	}
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, err
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	return &TagRuleSaveResult{
		ID:       tag.ID,
		Name:     tag.Name,
		Category: string(tag.Category),
		Source:   string(tag.Source),
		Rule:     customerTagGetRule(tag),
		Active:   active,
	}, nil
}

// UpdateTagRule 更新指定自动标签规则
func (s *TagRuleService) UpdateTagRule(ctx context.Context, id string, input SaveTagRuleInput) (*TagRuleSaveResult, error) {
	existing, err := s.tagRepo.GetByID(ctx, id)
	if err != nil || existing == nil {
		return nil, errors.New("规则不存在")
	}
	if input.Name != "" {
		existing.Name = input.Name
	}
	if input.Category != "" {
		existing.Category = model.TagCategory(input.Category)
	}
	if input.Rule != nil {
		if err := customerTagSetRule(existing, input.Rule); err != nil {
			return nil, err
		}
	}
	if err := s.tagRepo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return &TagRuleSaveResult{
		ID:       existing.ID,
		Name:     existing.Name,
		Category: string(existing.Category),
		Source:   string(existing.Source),
		Rule:     customerTagGetRule(existing),
	}, nil
}

// DeleteTagRule 删除自动标签规则
func (s *TagRuleService) DeleteTagRule(ctx context.Context, id string) error {
	return s.tagRepo.Delete(ctx, id)
}

// TagStatsResult 标签统计结果
type TagStatsResult struct {
	Total      int           `json:"total"`
	ByCategory []TagStatItem `json:"by_category"`
	BySource   []TagStatItem `json:"by_source"`
}

// TagStatItem 标签统计项
type TagStatItem struct {
	Key   string `json:"category"`
	Count int    `json:"count"`
}

// GetTagStats 获取标签统计
func (s *TagRuleService) GetTagStats(ctx context.Context) (*TagStatsResult, error) {
	tags, err := s.tagRepo.ListByMerchant(ctx)
	if err != nil {
		return nil, err
	}
	byCategory := make(map[string]int)
	bySource := make(map[string]int)
	total := len(tags)
	for _, t := range tags {
		cat := string(t.Category)
		if cat == "" {
			cat = "uncategorized"
		}
		byCategory[cat]++
		bySource[string(t.Source)]++
	}
	categories := make([]TagStatItem, 0, len(byCategory))
	for k, v := range byCategory {
		categories = append(categories, TagStatItem{Key: k, Count: v})
	}
	sources := make([]TagStatItem, 0, len(bySource))
	for k, v := range bySource {
		sources = append(sources, TagStatItem{Key: k, Count: v})
	}
	return &TagStatsResult{
		Total:      total,
		ByCategory: categories,
		BySource:   sources,
	}, nil
}

// CustomerQueryService OneID 客户查询门面服务
type CustomerQueryService struct {
	custRepo repository.CustomerRepository
}

// NewCustomerQueryService 创建 OneID 客户查询门面服务
func NewCustomerQueryService() *CustomerQueryService {
	return &CustomerQueryService{custRepo: repository.NewCustomerRepository()}
}

// CustomerView 客户视图（脱离 model 的 DTO）
type CustomerView struct {
	ID            string `json:"id"`
	UnifiedID     string `json:"unified_id"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	WechatOpenID  string `json:"wechat_open_id"`
	DouyinOpenID  string `json:"douyin_open_id"`
	XiaohongshuID string `json:"xiaohongshu_id"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// GetCustomerByID 按 ID 获取客户视图
func (s *CustomerQueryService) GetCustomerByID(ctx context.Context, id string) (*CustomerView, error) {
	customer, err := s.custRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if customer == nil || customer.ID == "" {
		return nil, nil
	}
	return &CustomerView{
		ID:            customer.ID,
		UnifiedID:     customer.UnifiedID,
		Phone:         customer.Phone,
		Email:         customer.Email,
		WechatOpenID:  customer.WechatOpenID,
		DouyinOpenID:  customer.DouyinOpenID,
		XiaohongshuID: customer.XiaohongshuID,
		CreatedAt:     customer.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:     customer.UpdatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// ListCustomers 列出 OneID 客户
func (s *CustomerQueryService) ListCustomers(ctx context.Context, page, pageSize int, keyword string) ([]*CustomerView, int64) {
	list, total := ListOneIDCustomers(ctx, s.custRepo, page, pageSize, keyword)
	out := make([]*CustomerView, 0, len(list))
	for _, c := range list {
		out = append(out, &CustomerView{
			ID:            c.ID,
			UnifiedID:     c.UnifiedID,
			Phone:         c.Phone,
			Email:         c.Email,
			WechatOpenID:  c.WechatOpenID,
			DouyinOpenID:  c.DouyinOpenID,
			XiaohongshuID: c.XiaohongshuID,
			CreatedAt:     c.CreatedAt,
			UpdatedAt:     c.UpdatedAt,
		})
	}
	return out, total
}

// ListConflicts 列出身份冲突
func (s *CustomerQueryService) ListConflicts(ctx context.Context, page, pageSize int) ([]*IdentityConflict, int64) {
	return DetectIdentityConflicts(ctx, s.custRepo, page, pageSize)
}

// OneIDStatsView OneID 统计视图
type OneIDStatsView struct {
	Total         int64 `json:"total"`
	WithPhone     int64 `json:"with_phone"`
	WithEmail     int64 `json:"with_email"`
	WithWechat    int64 `json:"with_wechat"`
	WithDouyin    int64 `json:"with_douyin"`
	MultiIdentity int64 `json:"multi_identity"`
}

// OneIDStats OneID 体系统计：总数、关联各渠道数、多身份客户数
func (s *CustomerQueryService) OneIDStats(ctx context.Context) *OneIDStatsView {
	_, total := s.ListCustomers(ctx, 1, 1, "")
	withPhone, _ := s.custRepo.CountNotEmpty(ctx, "phone")
	withEmail, _ := s.custRepo.CountNotEmpty(ctx, "email")
	withWechat, _ := s.custRepo.CountNotEmpty(ctx, "wechat_open_id")
	withDouyin, _ := s.custRepo.CountNotEmpty(ctx, "douyin_open_id")
	multi, _ := s.custRepo.CountMultiIdentity(ctx)
	return &OneIDStatsView{
		Total:         total,
		WithPhone:     withPhone,
		WithEmail:     withEmail,
		WithWechat:    withWechat,
		WithDouyin:    withDouyin,
		MultiIdentity: multi,
	}
}
