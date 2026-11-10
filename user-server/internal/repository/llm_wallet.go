package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

// LLMWalletRepository LLM 余额的本地镜像仓储。
// 一期只做「镜像展示」不做本地扣减：扣减权威在平台侧（代理转发时实时扣），
// 镜像的作用是平台不可达时仍能给用户一个可解释的余额数字。
type LLMWalletRepository interface {
	ApplyMirror(ctx context.Context, inputRemaining, outputRemaining int64) error
	// GetMirror 读本租户的镜像行（PLT-01 刀3）。没有镜像（从未在线查过余额）
	// 返回 (nil, nil)：调用侧据此区分「离线但有上次快照」与「从没有过数据」，
	// 两者都不是错误。
	GetMirror(ctx context.Context) (*model.LLMWallet, error)
	ListTxns(ctx context.Context, reason string, page, pageSize int) ([]*model.LLMWalletTxn, int64, error)
}

type llmWalletRepo struct{ db *gorm.DB }

func NewLLMWalletRepository() LLMWalletRepository { return &llmWalletRepo{db: db.GetDB()} }

func NewLLMWalletRepositoryWithDB(gdb *gorm.DB) LLMWalletRepository {
	return &llmWalletRepo{db: gdb}
}

// ApplyMirror 把平台余额落到本地单行镜像。
// 是 upsert（ON CONFLICT (tenant_id, scope) 只更新余额与同步时间）不是整行覆盖：
// 镜像行其他列由别的链路维护，整行写会把它们抹成零值。
func (r *llmWalletRepo) ApplyMirror(ctx context.Context, inputRemaining, outputRemaining int64) error {
	if r == nil || r.db == nil {
		return errors.New("[llm-wallet] 仓储未初始化（DB 未就绪）")
	}
	row := &model.LLMWallet{
		InputTokensRemaining:  inputRemaining,
		OutputTokensRemaining: outputRemaining,
		SyncedAt:              time.Now(),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		// tenant_id 在前：租户索引策略的产物（见 repository/tenant_module.go 同处注释）
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "scope"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"input_tokens_remaining", "output_tokens_remaining", "synced_at",
		}),
	}).Create(row).Error
}

// GetMirror 读本租户的镜像行。租户作用域不在这里手拼 WHERE：llm_wallets 是
// 租户表，隔离 callback 按 ctx 绑定的租户注入过滤（off 模式=默认租户行）——
// 与 ApplyMirror 的写作用域同一口径，读到的就是本租户自己那一份快照。
// 无行返回 (nil, nil)（从未在线查过余额 ≠ 故障）；db 未就绪返回错误。
func (r *llmWalletRepo) GetMirror(ctx context.Context) (*model.LLMWallet, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("[llm-wallet] 仓储未初始化（DB 未就绪）")
	}
	var row model.LLMWallet
	err := r.db.WithContext(ctx).Order("id ASC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *llmWalletRepo) ListTxns(ctx context.Context, reason string, page, pageSize int) ([]*model.LLMWalletTxn, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("[llm-wallet] 仓储未初始化（DB 未就绪）")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	// Count 与 Find 必须各起一条 query：同一条链上先 Count 再 Find 会就地改 Statement。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.LLMWalletTxn{})
		if reason != "" {
			q = q.Where("reason = ?", reason)
		}
		return q
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*model.LLMWalletTxn
	err := base().Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}
