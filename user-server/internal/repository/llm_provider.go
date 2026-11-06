// llm_provider.go llm_providers 表的按租户读取仓储（AV-04a）。
//
// 职责边界：platform_llm_usage.go 管的是平台垫资用量镜像与平台下发 provider
// 行的 upsert（写路径）；本文件只放「路由/列表按租户反查 provider 名单」的
// 读路径查询。二者不合并——合并会让平台同步的写依赖与租户名单的读依赖
// 共用同一个结构体，任何一侧变化都会波及另一侧调用方。
//
// 下沉缘由：service/llm_routing.go 此前在 tenantProviderNames 里直接
// pkgdb.GetDB() 查库（G-ARCH 2.2 违规），查询下沉到这里并经构造注入。
package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// LLMProviderRepository llm_providers 表仓储。
type LLMProviderRepository struct {
	db *gorm.DB
}

// NewLLMProviderRepository 默认构造（全局句柄，仓库既有惯例）。
func NewLLMProviderRepository() *LLMProviderRepository {
	return &LLMProviderRepository{db: GetDB()}
}

// NewLLMProviderRepositoryWithDB 用测试/装配注入的连接构造。
func NewLLMProviderRepositoryWithDB(db *gorm.DB) *LLMProviderRepository {
	return &LLMProviderRepository{db: db}
}

// ListProviderNamesByTenant 返回指定租户名下全部 provider 的 Name 列表。
//
// 契约（AV-04a 冻结，与下沉前 service 内直查逐项一致）：
//   - 输入：tenantID 为 ctx 已解析的当前租户 ID。单租户模式/缺租户上下文
//     由调用方（service.tenantProviderNames）短路，不会进入本方法。
//   - 输出：仅 name 列（不含 api_key 等敏感列），库返回序——调用方聚合
//     成集合后再按 Name 排序出口，行序不是本方法契约的一部分。
//   - 错误：读库失败原样上抛；调用方按 fail-closed（空名单）处理，
//     绝不把空错误当「全平台名单」。
func (r *LLMProviderRepository) ListProviderNamesByTenant(ctx context.Context, tenantID uint) ([]string, error) {
	if r.db == nil {
		return nil, ErrDBNotReady
	}
	var names []string
	if err := r.db.WithContext(ctx).
		Model(&model.LLMProvider{}).
		Where("tenant_id = ?", tenantID).
		Pluck("name", &names).Error; err != nil {
		return nil, err
	}
	return names, nil
}
