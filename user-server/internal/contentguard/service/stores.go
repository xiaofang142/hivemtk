// stores.go 业务层依赖的仓储接口（依赖倒置点）。
//
// 为什么服务层只认接口：内容审查的核心裁决逻辑（策略/阈值/升级）必须能在
// **不连数据库**的前提下被测试——它决定"谁被冻结多久"，这类断言不该依赖
// 一个有状态的测试库（本仓的测试库还要求先 export POSTGRES_TEST_* 才能连）。
// 仓储实现放 contentguard/repository（GORM），测试用内存假实现。
package service

import (
	"context"
	"time"

	"hivemtk-user/internal/contentguard/model"
)

// WordQuery 词条列表查询。
type WordQuery = model.WordQuery

// RecordQuery 审查记录列表查询。
type RecordQuery = model.RecordQuery

// SanctionQuery 处置列表查询。
type SanctionQuery = model.SanctionQuery

// WordStore 词库仓储。
type WordStore interface {
	// ListEnabled 取当前租户全部启用词条（构建匹配快照用，不分页）。
	ListEnabled(ctx context.Context) ([]model.GuardWord, error)
	List(ctx context.Context, q WordQuery) ([]model.GuardWord, int64, error)
	Get(ctx context.Context, id uint) (*model.GuardWord, error)
	Upsert(ctx context.Context, w *model.GuardWord) (*model.GuardWord, error)
	BulkUpsert(ctx context.Context, words []model.GuardWord) (int, error)
	Delete(ctx context.Context, id uint) error
}

// PolicyStore 策略仓储。
type PolicyStore interface {
	Get(ctx context.Context, subjectType string) (*model.GuardPolicy, error)
	List(ctx context.Context) ([]model.GuardPolicy, error)
	Save(ctx context.Context, p *model.GuardPolicy) (*model.GuardPolicy, error)
}

// TaskStore 审查任务队列仓储。
type TaskStore interface {
	// Enqueue 入队；已存在同 TaskKey 时返回 false（幂等）。
	Enqueue(ctx context.Context, t *model.GuardTask) (bool, error)
	// ClaimDue 认领一批到期任务并置为 running（多副本安全：FOR UPDATE SKIP LOCKED）。
	ClaimDue(ctx context.Context, limit int, now time.Time) ([]model.GuardTask, error)
	MarkDone(ctx context.Context, id uint64) error
	// MarkFailed 记录失败。retryAt 非零 = 可重试（任务回到 pending 并顺延到 retryAt）；
	// 零值 = 放弃（置为 failed 死信，等人工排查）。两者必须区分：把死信留在 pending
	// 里会让"积压数"永远清不掉，而把可重试的置成 failed 会让一次抖动永久丢一条审查。
	MarkFailed(ctx context.Context, id uint64, errMsg string, retryAt time.Time) error
	CountPending(ctx context.Context) (int64, error)
}

// RecordStore 审查结论仓储。
type RecordStore interface {
	Create(ctx context.Context, r *model.GuardRecord) error
	Get(ctx context.Context, id uint64) (*model.GuardRecord, error)
	List(ctx context.Context, q RecordQuery) ([]model.GuardRecord, int64, error)
	// CountBlocksSince 统计窗口内该主体被拦截的次数（升级判据）。
	CountBlocksSince(ctx context.Context, subjectType, subjectID string, since time.Time) (int, error)
	UpdateManual(ctx context.Context, id uint64, status string, operatorID uint, at time.Time) error
	SetSanctionID(ctx context.Context, id, sanctionID uint64) error
}

// SanctionStore 处置仓储。
type SanctionStore interface {
	// FindActive 取该主体当前生效的处置（无则返回 nil, nil）。
	FindActive(ctx context.Context, subjectType, subjectID string, now time.Time) (*model.GuardSanction, error)
	Get(ctx context.Context, id uint64) (*model.GuardSanction, error)
	Create(ctx context.Context, s *model.GuardSanction) error
	Update(ctx context.Context, s *model.GuardSanction) error
	List(ctx context.Context, q SanctionQuery) ([]model.GuardSanction, int64, error)
}
