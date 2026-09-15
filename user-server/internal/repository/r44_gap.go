package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

// BackupGapRepo backup 断链相关仓储
type BackupGapRepo struct {
	db *gorm.DB
}

// NewBackupGapRepo 构造
func NewBackupGapRepo() *BackupGapRepo { return NewBackupGapRepoWithDB(db.GetDB()) }

// NewBackupGapRepoWithDB 装配/测试注入
//
// ARC-01：db 为 nil 时返回 nil 指针，service 侧以 repo == nil 作为未初始化守卫，
// 避免持有非 nil 仓储却在方法内对 nil *gorm.DB 解引用 panic。
func NewBackupGapRepoWithDB(gdb *gorm.DB) *BackupGapRepo {
	if gdb == nil {
		return nil
	}
	return &BackupGapRepo{db: gdb}
}

// CountBackups 总备份数
func (r *BackupGapRepo) CountBackups(ctx context.Context) (int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.Backup{}).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// LastSuccess 最后一次成功的备份
func (r *BackupGapRepo) LastSuccess(ctx context.Context) (*model.Backup, error) {
	var last model.Backup
	err := r.db.WithContext(ctx).
		Where("status = ?", "success").
		Order("created_at DESC").First(&last).Error
	if err != nil {
		return nil, err
	}
	return &last, nil
}

// TableStatsRow pg_stat_user_tables 行
type TableStatsRow struct {
	Table string `gorm:"column:table_name"`
	Rows  int64  `gorm:"column:rows"`
}

// TableStats 取前 10 张最大表的规模估算
func (r *BackupGapRepo) TableStats(ctx context.Context) ([]TableStatsRow, error) {
	var rows []TableStatsRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT relname AS table_name, GREATEST(n_live_tup, 0) AS rows
		FROM pg_stat_user_tables ORDER BY n_live_tup DESC LIMIT 10`).Scan(&rows).Error
	return rows, err
}

// BackupListRow backups 列表行
type BackupListRow struct {
	ID         uint
	BackupName string
	BackupType string
	Status     string
	FileSize   int64
	CreatedAt  string
}

// ListBackups 列出 backups 表（最多 100 条，按 id 降序）
func (r *BackupGapRepo) ListBackups(ctx context.Context) ([]BackupListRow, error) {
	var rows []BackupListRow
	if err := r.db.WithContext(ctx).
		Table("backups").
		Select("id, backup_name, backup_type, status, file_size, TO_CHAR(created_at,'YYYY-MM-DD HH24:MI') as created_at").
		Order("id DESC").Limit(100).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// CoreTableStats 核心表行数估算（pg_stat_user_tables 按表名逐张查询）
func (r *BackupGapRepo) CoreTableStats(ctx context.Context, tables []string) ([]TableStatsRow, error) {
	rows := make([]TableStatsRow, 0, len(tables))
	for _, t := range tables {
		var stat TableStatsRow
		if err := r.db.WithContext(ctx).Raw(
			"SELECT ?::text AS table_name, GREATEST(n_live_tup,0) AS rows FROM pg_stat_user_tables WHERE relname = ?", t, t,
		).Scan(&stat).Error; err == nil && stat.Table != "" {
			rows = append(rows, stat)
		}
	}
	return rows, nil
}

// RagEvalGapRepo RAG 评测仓储
type RagEvalGapRepo struct {
	db *gorm.DB
}

// NewRagEvalGapRepo 构造
func NewRagEvalGapRepo() *RagEvalGapRepo { return NewRagEvalGapRepoWithDB(db.GetDB()) }

// NewRagEvalGapRepoWithDB 装配/测试注入（ARC-01：nil db 返回 nil，service 以 repo == nil 守卫）
func NewRagEvalGapRepoWithDB(gdb *gorm.DB) *RagEvalGapRepo {
	if gdb == nil {
		return nil
	}
	return &RagEvalGapRepo{db: gdb}
}

// CreateQuestion 入库一条评测题
func (r *RagEvalGapRepo) CreateQuestion(ctx context.Context, q *model.RagEvalQuestion) error {
	return r.db.WithContext(ctx).Create(q).Error
}

// CreateRun 创建一条评测 run 记录（返回带 ID 的 run）
func (r *RagEvalGapRepo) CreateRun(ctx context.Context, run *model.RagEvalRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// MarkRunFailed 将 run 标记为失败
func (r *RagEvalGapRepo) MarkRunFailed(ctx context.Context, runID uint) error {
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).
		Updates(map[string]any{"total": -2, "eval_set_size": 0}).Error
}

// UpdateRunResult 回填 run 结果
func (r *RagEvalGapRepo) UpdateRunResult(ctx context.Context, runID uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).Updates(updates).Error
}

// ListQuestions 拉取评测题集（按 productID 过滤，最多 200 条）
func (r *RagEvalGapRepo) ListQuestions(ctx context.Context, productID string) ([]model.RagEvalQuestion, error) {
	var qs []model.RagEvalQuestion
	q := r.db.WithContext(ctx).Model(&model.RagEvalQuestion{})
	if productID != "" {
		q = q.Where("product_id = ? OR product_id = ''", productID)
	}
	if err := q.Order("id ASC").Limit(200).Find(&qs).Error; err != nil {
		return nil, err
	}
	return qs, nil
}

// CreateRunCompleted 计算完成后落库
func (r *RagEvalGapRepo) CreateRunCompleted(ctx context.Context, run *model.RagEvalRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// LatestRun 最新一次 run
func (r *RagEvalGapRepo) LatestRun(ctx context.Context) (*model.RagEvalRun, error) {
	var run model.RagEvalRun
	if err := r.db.WithContext(ctx).Order("id DESC").First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &model.RagEvalRun{}, nil
		}
		return nil, err
	}
	return &run, nil
}

// ListRuns 历史
func (r *RagEvalGapRepo) ListRuns(ctx context.Context, limit int) ([]*model.RagEvalRun, error) {
	var runs []*model.RagEvalRun
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// GetRun 按 ID 取一次 run（Diff 用）
func (r *RagEvalGapRepo) GetRun(ctx context.Context, id uint) (*model.RagEvalRun, error) {
	var run model.RagEvalRun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// CohortGapRepo 留存/路径分析仓储
type CohortGapRepo struct {
	db *gorm.DB
}

// NewCohortGapRepo 构造
func NewCohortGapRepo() *CohortGapRepo { return NewCohortGapRepoWithDB(db.GetDB()) }

// NewCohortGapRepoWithDB 装配/测试注入（ARC-01：nil db 返回 nil，service 以 repo == nil 守卫）
func NewCohortGapRepoWithDB(gdb *gorm.DB) *CohortGapRepo {
	if gdb == nil {
		return nil
	}
	return &CohortGapRepo{db: gdb}
}

// CountCustomersBetween 注册区间内的客户数
func (r *CohortGapRepo) CountCustomersBetween(ctx context.Context, start, end time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Customer{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&n).Error
	return n, err
}

// CountRetainedEvents 区间客户在活跃周内的事件去重数
func (r *CohortGapRepo) CountRetainedEvents(ctx context.Context, cohortStart, cohortEnd, weekStart, weekEnd time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerEvent{}).
		Joins("JOIN customers ON customers.one_id = customer_events.customer_id OR customers.id::text = customer_events.customer_id").
		Where("customers.created_at >= ? AND customers.created_at < ?", cohortStart, cohortEnd).
		Where("customer_events.created_at >= ? AND customer_events.created_at < ?", weekStart, weekEnd).
		Distinct("customer_events.customer_id").
		Count(&n).Error
	return n, err
}

// CustomerEventRow Path 分析行
type CustomerEventRow struct {
	CustomerID string
	EventType  string
	CreatedAt  time.Time
}

// ListCustomerEvents 按 customer_id 排序取前 5000 条事件（Path 桑基分析）
func (r *CohortGapRepo) ListCustomerEvents(ctx context.Context) ([]CustomerEventRow, error) {
	var evs []CustomerEventRow
	if err := r.db.WithContext(ctx).Model(&model.CustomerEvent{}).
		Select("customer_id, event_type, created_at").
		Order("customer_id ASC, created_at ASC").Limit(5000).
		Scan(&evs).Error; err != nil {
		return nil, err
	}
	return evs, nil
}

// EmailGapRepo 邮件送达分析仓储
type EmailGapRepo struct {
	db *gorm.DB
}

// NewEmailGapRepo 构造
func NewEmailGapRepo() *EmailGapRepo { return NewEmailGapRepoWithDB(db.GetDB()) }

// NewEmailGapRepoWithDB 装配/测试注入（ARC-01：nil db 返回 nil，service 以 repo == nil 守卫）
func NewEmailGapRepoWithDB(gdb *gorm.DB) *EmailGapRepo {
	if gdb == nil {
		return nil
	}
	return &EmailGapRepo{db: gdb}
}

// CountSent 时间范围内发送数
func (r *EmailGapRepo) CountSent(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("created_at >= ?", since).Count(&n).Error
	return n, err
}

// CountEventByType 按事件类型统计
func (r *EmailGapRepo) CountEventByType(ctx context.Context, eventType string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("event_type = ? AND timestamp >= ?", eventType, since).Count(&n).Error
	return n, err
}

// ListBounces 取 bounce 事件分软硬桶（最多 2000 条）
func (r *EmailGapRepo) ListBounces(ctx context.Context, since time.Time) ([]model.EmailTrackingEvent, error) {
	var evs []model.EmailTrackingEvent
	err := r.db.WithContext(ctx).
		Where("event_type IN ? AND timestamp >= ?", []string{"bounce", "soft_bounce", "hard_bounce"}, since).
		Order("timestamp DESC").Limit(2000).Find(&evs).Error
	return evs, err
}

// BounceByISPRow ISP 分桶行
type BounceByISPRow struct {
	Domain string `gorm:"column:domain"`
	Cnt    int64  `gorm:"column:cnt"`
}

// BounceByISP ISP 分桶（最多 8 条）
func (r *EmailGapRepo) BounceByISP(ctx context.Context, since time.Time) ([]BounceByISPRow, error) {
	var rows []BounceByISPRow
	if err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Select("SPLIT_PART(email, '@', 2) AS domain, COUNT(*) AS cnt").
		Where("event_type IN ? AND timestamp >= ?", []string{"bounce", "soft_bounce", "hard_bounce"}, since).
		Group("SPLIT_PART(email, '@', 2)").Order("cnt DESC").Limit(8).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SMTPServerRow SMTP 配置行
type SMTPServerRow struct {
	Host string `gorm:"column:host"`
	User string `gorm:"column:username"`
}

// ListSMTPServers 自有 SMTP 配置取前 10
func (r *EmailGapRepo) ListSMTPServers(ctx context.Context) ([]SMTPServerRow, error) {
	var rows []SMTPServerRow
	err := r.db.WithContext(ctx).Table("email_smtp").
		Select("server AS host, username").Where("deleted_at IS NULL").Limit(10).Scan(&rows).Error
	return rows, err
}

// DistinctTrackingDomains 从 tracking 邮箱兜底取域名（最多 5 个）
func (r *EmailGapRepo) DistinctTrackingDomains(ctx context.Context) ([]string, error) {
	var rows []struct{ Domain string }
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Select("DISTINCT SPLIT_PART(email, '@', 2) AS domain").Limit(5).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Domain != "" {
			out = append(out, r.Domain)
		}
	}
	return out, nil
}

// CountSentToDomain 发往指定域名的发送数（since24h）
func (r *EmailGapRepo) CountSentToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("to LIKE ? AND created_at >= ?", "%@"+domain, since).Count(&n).Error
	return n, err
}

// CountEventToDomain 域名匹配 + 事件类型 + 时间范围的 tracking 计数
func (r *EmailGapRepo) CountEventToDomain(ctx context.Context, eventType string, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type IN ? AND timestamp >= ?", "%@"+domain, []string{eventType}, since).Count(&n).Error
	return n, err
}

// CountSpamReportToDomain 域名匹配 + spam_report 计数
func (r *EmailGapRepo) CountSpamReportToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type = ? AND timestamp >= ?", "%@"+domain, "spam_report", since).Count(&n).Error
	return n, err
}

// CountBouncesToDomain 域名匹配 + 三类 bounce 事件计数
func (r *EmailGapRepo) CountBouncesToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type IN ? AND timestamp >= ?", "%@"+domain,
			[]string{"bounce", "soft_bounce", "hard_bounce"}, since).Count(&n).Error
	return n, err
}

// ResetDeadLetters 将 message_hub 中 dead_letter 重置为 pending，返回影响行数
func (r *EmailGapRepo) ResetDeadLetters(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).
		Table("message_hub").
		Where("status = 'dead_letter'").
		Update("status", "pending")
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// UpdateClueFields 按 ID 更新线索字段（导入合并用）
func (r *EmailGapRepo) UpdateClueFields(ctx context.Context, clueID int64, updates map[string]any) error {
	return r.db.WithContext(ctx).Table("clues").
		Where("id = ?", clueID).Updates(updates).Error
}

// CreateClueRecord 强制创建单条线索
func (r *EmailGapRepo) CreateClueRecord(ctx context.Context, rec map[string]any) error {
	return r.db.WithContext(ctx).Table("clues").Create(rec).Error
}
