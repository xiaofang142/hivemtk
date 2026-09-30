package repository

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/model"
)

// ErrSecurityRulesNotFound 尚未写入过配置。
var ErrSecurityRulesNotFound = errors.New("security rules not found")

// SecurityRulesRepository 安全规则配置仓储接口。
type SecurityRulesRepository interface {
	Available() bool
	Get(ctx context.Context) (*model.SecurityRules, error)
	Upsert(ctx context.Context, rules *model.SecurityRules) error
}

type securityRulesRepo struct {
	kv SystemConfigKVRepository
}

// NewSecurityRulesRepository 构造安全规则仓储（复用 system_config_kv）。
func NewSecurityRulesRepository(kv SystemConfigKVRepository) SecurityRulesRepository {
	if kv == nil {
		return nil
	}
	return &securityRulesRepo{kv: kv}
}

// Available 配置存储是否可用。
func (r *securityRulesRepo) Available() bool {
	return r != nil && r.kv != nil
}

// Get 读取安全规则配置。
func (r *securityRulesRepo) Get(ctx context.Context) (*model.SecurityRules, error) {
	if !r.Available() {
		return nil, ErrSecurityRulesNotFound
	}
	raw, err := r.kv.Get(ctx, model.SecurityRulesConfigKey)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, ErrSecurityRulesNotFound
	}
	var rules model.SecurityRules
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// Upsert 写入安全规则配置。
func (r *securityRulesRepo) Upsert(ctx context.Context, rules *model.SecurityRules) error {
	if !r.Available() || rules == nil {
		return ErrSecurityRulesNotFound
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	_, err = r.kv.Upsert(ctx, model.SecurityRulesConfigKey, string(raw))
	return err
}
