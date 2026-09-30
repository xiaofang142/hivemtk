package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/safeprompt"
)

// ErrSecurityRulesUnavailable 数据层不可用。
var ErrSecurityRulesUnavailable = errors.New("security rules store unavailable")

// SecurityRulesService 安全规则配置服务：敏感词过滤 / 输出护栏 / 数据脱敏。
type SecurityRulesService interface {
	Available() bool
	Get(ctx context.Context) (*model.SecurityRules, error)
	Put(ctx context.Context, rules *model.SecurityRules) (*model.SecurityRules, error)
}

type securityRulesService struct {
	repo repository.SecurityRulesRepository
}

// NewSecurityRulesService 构造安全规则配置服务。
func NewSecurityRulesService(db *gorm.DB) SecurityRulesService {
	kv := repository.NewSystemConfigKVRepositoryWithDB(db)
	return &securityRulesService{repo: repository.NewSecurityRulesRepository(kv)}
}

// Available 数据层是否可用。
func (s *securityRulesService) Available() bool {
	return s != nil && s.repo != nil && s.repo.Available()
}

// Get 读取配置；首次访问返回内置默认并落库。
func (s *securityRulesService) Get(ctx context.Context) (*model.SecurityRules, error) {
	if !s.Available() {
		return nil, ErrSecurityRulesUnavailable
	}
	rules, err := s.repo.Get(ctx)
	if err == nil {
		s.apply(rules)
		return rules, nil
	}
	if !errors.Is(err, repository.ErrSecurityRulesNotFound) {
		return nil, err
	}

	rules = defaultSecurityRules()
	if saveErr := s.repo.Upsert(ctx, rules); saveErr != nil {
		return nil, saveErr
	}
	s.apply(rules)
	return rules, nil
}

// Put 写入配置并热更新敏感词词典。
func (s *securityRulesService) Put(ctx context.Context, rules *model.SecurityRules) (*model.SecurityRules, error) {
	if !s.Available() {
		return nil, ErrSecurityRulesUnavailable
	}
	if rules == nil {
		return nil, ErrSecurityRulesUnavailable
	}
	if rules.SensitiveWords == nil {
		rules.SensitiveWords = map[string]int{}
	}
	rules.UpdatedAt = time.Now()
	if err := s.repo.Upsert(ctx, rules); err != nil {
		return nil, err
	}
	s.apply(rules)
	return rules, nil
}

// apply 把配置推给 safeprompt 运行时（词典热更新）。
func (s *securityRulesService) apply(rules *model.SecurityRules) {
	if rules == nil || len(rules.SensitiveWords) == 0 {
		return
	}
	safeprompt.RegisterForbiddenWords(rules.SensitiveWords)
}

// defaultSecurityRules 内置默认：空词典表示沿用 safeprompt 内置敏感词表，护栏与脱敏默认开启。
func defaultSecurityRules() *model.SecurityRules {
	return &model.SecurityRules{
		SensitiveWords:     map[string]int{},
		OutputGuardEnabled: true,
		PIIMaskEnabled:     true,
		UpdatedAt:          time.Now(),
	}
}
