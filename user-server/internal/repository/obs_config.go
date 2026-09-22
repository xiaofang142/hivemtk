package repository

import (
	"context"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ObsConfigRepository OBS 配置仓储接口
//
// 全局统一走 is_default 列（无 License 关联接口）。
// 选取默认行的唯一判据是 `is_default AND status = active`，且全表最多一条 ——
// 后者由库侧偏唯一索引兜底（db.postMigrateObsDefaultUniqueIndex）。
// 刻意不提供"只清默认不置新默认"的方法：全站上传必须有默认行，
// 单独暴露清除入口只会让人在事务外先清后设，留下零默认窗口（批M 修掉的就是它）。
type ObsConfigRepository interface {
	Create(ctx context.Context, config *model.ObsConfig) error
	GetByID(ctx context.Context, id string) (*model.ObsConfig, error)
	GetList(ctx context.Context, page int, limit int, provider string, status string) ([]*model.ObsConfig, int64, error)
	// Update 只写"管理员可编辑"那 12 列；is_default/status/用量两列一律不经这个口落库。
	Update(ctx context.Context, config *model.ObsConfig) error
	// DeleteNonDefault 删除一条非默认配置；判据（不许删默认行）写在这条语句里。
	DeleteNonDefault(ctx context.Context, id string) error
	GetDefault(ctx context.Context) (*model.ObsConfig, error)
	SetDefault(ctx context.Context, id string) error
	// IncrementUsage 累加用量计数；只写 file_count/total_size 两列，不整行写回。
	IncrementUsage(ctx context.Context, id string, size int64) error
	UpdateStatus(ctx context.Context, id string, status model.ObsStatus) error
	CountByStatus(ctx context.Context, status model.ObsStatus) (int64, error)
	Count(ctx context.Context) (int64, error)
}

type obsConfigRepo struct {
	db *gorm.DB
}

func NewObsConfigRepository() ObsConfigRepository {
	return &obsConfigRepo{db: _db.GetDB()}
}

func NewObsConfigRepositoryWithDB(db *gorm.DB) ObsConfigRepository {
	return &obsConfigRepo{db: db}
}

func (r *obsConfigRepo) Create(ctx context.Context, config *model.ObsConfig) error {
	return r.db.Create(config).Error
}

func (r *obsConfigRepo) GetByID(ctx context.Context, id string) (*model.ObsConfig, error) {
	var config model.ObsConfig
	err := r.db.Where("id = ?", id).First(&config).Error
	return &config, err
}

func (r *obsConfigRepo) GetList(ctx context.Context, page int, limit int, provider string, status string) ([]*model.ObsConfig, int64, error) {
	var configs []*model.ObsConfig
	var total int64
	offset := (page - 1) * limit

	query := r.db.Model(&model.ObsConfig{})

	if provider != "" {
		query = query.Where("provider = ?", provider)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&configs).Error
	return configs, total, err
}

// Update 写回一台配置的**可编辑列白名单**（编辑口；唯一生产调用方 = service.UpdateConfig）。
//
// 为什么不是整行 Save：调用方传回来的多半是一份快照，而快照除了可编辑列还带着
// is_default / status / file_count / total_size —— 前两列归 SetDefault 与库级偏唯一
// 索引管，后两列归 IncrementUsage 管。整行写回会把"读完快照之后"发生的两件事一起
// 抹掉：默认切换 ⇒ 刚被摘掉的那台复活成第二条默认；上传计数 ⇒ 计数器被旧值覆盖
// 且无人知晓。用**白名单**而不是 Omit 黑名单，是因为模型以后加列时黑名单会静默失守
// （与 IncrementUsage 只点两列同一个理由）。
//
// 为什么也不是 Save 的另一半：Save 在主键为零值时会退化成 INSERT，编辑口不该有
// 第二身份；建配置走 Create。
func (r *obsConfigRepo) Update(ctx context.Context, config *model.ObsConfig) error {
	if config == nil || config.ID == "" {
		return gorm.ErrRecordNotFound
	}
	res := r.db.Model(&model.ObsConfig{}).Where("id = ?", config.ID).
		Updates(map[string]any{
			"name":        config.Name,
			"provider":    config.Provider,
			"access_key":  config.AccessKey,
			"secret_key":  config.SecretKey,
			"bucket":      config.Bucket,
			"region":      config.Region,
			"endpoint":    config.Endpoint,
			"domain":      config.Domain,
			"path_prefix": config.PathPrefix,
			"config":      config.Config,
			"max_size":    config.MaxSize,
			"max_count":   config.MaxCount,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteNonDefault 删除一条**非默认**配置；删除默认行和删除不存在的行都返回
// gorm.ErrRecordNotFound。
//
// 为什么"是不是默认"这条判据写在 SQL 里而不是留给调用方预读：服务层是
// 先 GetByID 看一眼 is_default 再删，两句之间另一个请求可以把这一行抬成默认
// （管理页上"设为默认"和"删除"是两次独立点击）。抬成默认之后再删掉的就是
// 全站唯一那条默认 —— 两条上传口一起断，入站媒体静默改落本地盘。
// 而 `InitDefaultStorageIfEmpty` 只在**表为空**时兜底 seed，表里还有别的行，
// 所以这个状态没有任何自愈路径。与 SetDefault 同一口径：判据落在写语句本身。
//
// 需要"连默认行一起删"（清库/重建）时必须显式另开入口，不许顺手放宽这里。
func (r *obsConfigRepo) DeleteNonDefault(ctx context.Context, id string) error {
	res := r.db.Where("id = ? AND is_default = ?", id, false).Delete(&model.ObsConfig{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *obsConfigRepo) GetDefault(ctx context.Context) (*model.ObsConfig, error) {
	var config model.ObsConfig
	err := r.db.Where("is_default = ? AND status = ?", true, model.ObsStatusActive).
		Order("created_at ASC").
		First(&config).Error
	return &config, err
}

// SetDefault 把 id 这一行设为唯一默认，并在**同一个事务**里摘掉其余默认行。
//
// 顺序是先摘别人、后置自己，与直觉相反，原因在库侧那道偏唯一索引
// （`idx_obs_config_single_default`，见 db.postMigrateObsDefaultUniqueIndex）：
// 先给自己置真会当场撞唯一冲突，事务直接失败，永远切不过去。
// 反过来则存在一个"两条都为真"的瞬间——它只活在事务内，外部读不到，
// 提交前一定会被第二句清掉。
//
// 目标行不存在时必须报错并回滚：旧实现把 ClearDefault 先提交了，第二句影响 0 行
// 仍返回 nil，库里留下"零条默认"，全站上传与媒体转存从此再没有自愈路径。
func (r *obsConfigRepo) SetDefault(ctx context.Context, id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ObsConfig{}).
			Where("is_default = ? AND id <> ?", true, id).
			Update("is_default", false).Error; err != nil {
			return err
		}
		res := tx.Model(&model.ObsConfig{}).Where("id = ?", id).Update("is_default", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// IncrementUsage 只累加两个用量列，别的列一概不写。
//
// 为什么不用 Update(整行)：上传路径手上的 config 是**上传开始时**读到的快照，
// 整行写回会把这份快照里的 is_default 一起落库。管理员在上传期间切换默认时，
// 陈旧的 is_default=true 会被复活 ⇒ 库里两条默认行，之后每次选取都由 uuid 主键序
// 决定（等于抛硬币），媒体落到哪台存储没人说得清。
//
// 用 UpdateColumns 而不是 Updates：后者会让 GORM 顺手刷 updated_at，
// 而"又传了一个文件"不是配置被改过，不该顶掉管理页上的"最后修改时间"。
func (r *obsConfigRepo) IncrementUsage(ctx context.Context, id string, size int64) error {
	res := r.db.Model(&model.ObsConfig{}).Where("id = ?", id).
		UpdateColumns(map[string]any{
			"file_count": gorm.Expr("file_count + 1"),
			"total_size": gorm.Expr("total_size + ?", size),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *obsConfigRepo) UpdateStatus(ctx context.Context, id string, status model.ObsStatus) error {
	return r.db.Model(&model.ObsConfig{}).Where("id = ?", id).Update("status", status).Error
}

func (r *obsConfigRepo) CountByStatus(ctx context.Context, status model.ObsStatus) (int64, error) {
	var count int64
	err := r.db.Model(&model.ObsConfig{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *obsConfigRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.Model(&model.ObsConfig{}).Count(&count).Error
	return count, err
}
