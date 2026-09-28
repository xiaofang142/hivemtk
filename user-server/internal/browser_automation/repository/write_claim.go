package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserWriteClaimRepository 不可逆写的存储层独占声明（批20f / A12）。
//
// 三个方法就是这把坑的全生命周期：占（Claim）→ 别人已占则拿回事实（holder）→
// 证明从未发生则释放（Release）。没有 Update：跨越之后事实住在台账里，本表只管键位。
type BrowserWriteClaimRepository interface {
	// ClaimWriteSlot 占坑。返回 (nil, nil)=拿到（同一步重复占坑幂等地返回 nil，
	// 因为 prepared→sent 之间同一个键会被碰多次，把自己判成别人就等于把正常路径写死）；
	// (holder, nil)=别的步正持有这把坑；(nil, err)=占坑本身没做成（含键不完整、
	// 以及「插了 0 行却查不到持有者」这种并发释放的空窗——一律按 fail-close 上抛）。
	ClaimWriteSlot(ctx context.Context, taskID, sessionID, stepRowID uint, textHash string) (*model.BrowserWriteClaim, error)
	// ReleaseWriteSlot 释放**自己那把**：只删 step_row_id 与 text_hash 同时命中的行。
	// 条件少了 step_row_id 就成了「按文本释放」，一条被判 *_not_found 的步会把别人
	// 正跨着提交点的声明一起删掉——双发闸当场消失。返回删除行数（0=本来就不持有）。
	ReleaseWriteSlot(ctx context.Context, stepRowID uint, textHash string) (int64, error)
	// FindWriteClaim 当前持有者；无声明返回 gorm.ErrRecordNotFound。
	FindWriteClaim(ctx context.Context, taskID uint, textHash string) (*model.BrowserWriteClaim, error)
}

type browserWriteClaimRepo struct {
	db *gorm.DB
}

func NewBrowserWriteClaimRepository() BrowserWriteClaimRepository {
	return &browserWriteClaimRepo{db: _db.GetDB()}
}

// NewBrowserWriteClaimRepositoryWithDB 显式注入 gormDB（路由装配与测试用）
func NewBrowserWriteClaimRepositoryWithDB(db *gorm.DB) BrowserWriteClaimRepository {
	return &browserWriteClaimRepo{db: db}
}

// ClaimWriteSlot 用一条 INSERT ... ON CONFLICT DO NOTHING 完成「判定 + 占坑」：
// 两者在同一条语句里原子生效，这是本批唯一新增的并发防护，也是唯一有效的那一层。
// 刻意不走 gorm 的 Create()：Create 撞到唯一约束返回的是 23505 错误，调用方得再去
// 分辨「是谁撞的」；DO NOTHING 把「已被占」变成 0 行这个可判的读数，错误通道留给真故障。
func (r *browserWriteClaimRepo) ClaimWriteSlot(ctx context.Context, taskID, sessionID, stepRowID uint, textHash string) (*model.BrowserWriteClaim, error) {
	if textHash == "" {
		return nil, fmt.Errorf("写声明键不完整（task=%d step=%d）：空 text_hash 占坑会把整表的键位并成一个", taskID, stepRowID)
	}
	if stepRowID == 0 {
		return nil, fmt.Errorf("写声明键不完整（task=%d hash=%s）：step_row_id=0 的声明永远无法释放", taskID, textHash)
	}
	res := r.db.WithContext(ctx).Exec(
		`INSERT INTO browser_write_claims (task_id, text_hash, step_row_id, session_id, created_at)
			VALUES (?, ?, ?, ?, now()) ON CONFLICT (task_id, text_hash) DO NOTHING`,
		taskID, textHash, stepRowID, sessionID)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 1 {
		return nil, nil
	}
	holder, err := r.FindWriteClaim(ctx, taskID, textHash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 0 行 + 查不到 = 占坑与回读之间被并发释放过（或库本身异常）。这里不猜「那我再占一次」：
			// 再来一次就把 fail-close 变成了「总能转到绿」。上层会把这一步判成可见的红。
			return nil, fmt.Errorf("占坑返回 0 行却查不到持有者（task=%d hash=%s）：声明在两步之间被释放，本步拒绝下发", taskID, textHash)
		}
		return nil, err
	}
	if holder.StepRowID == stepRowID {
		return nil, nil
	}
	return holder, nil
}

func (r *browserWriteClaimRepo) ReleaseWriteSlot(ctx context.Context, stepRowID uint, textHash string) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM browser_write_claims WHERE step_row_id = ? AND text_hash = ?`,
		stepRowID, textHash)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *browserWriteClaimRepo) FindWriteClaim(ctx context.Context, taskID uint, textHash string) (*model.BrowserWriteClaim, error) {
	var row model.BrowserWriteClaim
	if err := r.db.WithContext(ctx).Where("task_id = ? AND text_hash = ?", taskID, textHash).
		Order("id ASC").First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
