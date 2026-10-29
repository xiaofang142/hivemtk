package cards

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// DomainPoolResolver 卡片域对域名池的只读依赖（拆包第三刀的破环接缝）。
//
// 为什么用接口而不是直接引根包 service.DomainPoolService：cards 包一旦反向
// import 根包就成环（根包 compat 要引 cards 给别名）。卡片只需要「按 ID 读一条
// 域名池」，故按最小接口注入；根包 DomainPoolService 天然满足本接口，无需改动。
type DomainPoolResolver interface {
	GetByID(ctx context.Context, id int) (*model.DomainPool, error)
}

// repoDomainPoolResolver 默认实现：直读 repository，与根包 DomainPoolService.GetByID
// 同一实现（根包那层也是纯转发，无业务逻辑，故不构成两套口径）。
type repoDomainPoolResolver struct {
	repo repository.DomainPoolRepository
}

func newRepoDomainPoolResolver(repo repository.DomainPoolRepository) DomainPoolResolver {
	return &repoDomainPoolResolver{repo: repo}
}

func (r *repoDomainPoolResolver) GetByID(ctx context.Context, id int) (*model.DomainPool, error) {
	return r.repo.GetByID(ctx, id)
}
