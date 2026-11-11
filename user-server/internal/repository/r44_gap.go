package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
)

type BackupGapRepo struct {
	db *gorm.DB
}

func NewBackupGapRepo() *BackupGapRepo { return NewBackupGapRepoWithDB(db.GetDB()) }

// NewBackupGapRepoWithDB 装配/测试注入（db 为 nil 时返回 nil，service 以 repo == nil 守卫）。
func NewBackupGapRepoWithDB(gdb *gorm.DB) *BackupGapRepo {
	if gdb == nil {
		return nil
	}
	return &BackupGapRepo{db: gdb}
}

func (r *BackupGapRepo) CountBackups(ctx context.Context) (int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.Backup{}).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *BackupGapRepo) LastSuccess(ctx context.Context) (*model.Backup, error) {
	var last model.Backup
	// 备份完成态在库里是 `completed`（历史版本写过 `success`）：只按 success 过滤
	// 会让 116 条已完成备份全部看不见，页面「最近成功」恒为 "-"。两种都收。
	err := r.db.WithContext(ctx).
		Where("status IN ?", []string{"success", "completed"}).
		Order("created_at DESC").First(&last).Error
	if err != nil {
		return nil, err
	}
	return &last, nil
}

// SumCompletedSize 已完成备份的文件大小合计（字节）。
// 统计卡的「总大小」是合计口径，不是最近一条的大小；旧实现只取 LastSuccess.FileSize，
// 于是在没有 success 行时恒为 0，与下方列表里每条都有大小自相矛盾。
func (r *BackupGapRepo) SumCompletedSize(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Model(&model.Backup{}).
		Where("status IN ?", []string{"success", "completed"}).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&total).Error
	return total, err
}

type TableStatsRow struct {
	Table string `gorm:"column:table_name"`
	Rows  int64  `gorm:"column:rows"`
}

func (r *BackupGapRepo) TableStats(ctx context.Context) ([]TableStatsRow, error) {
	var rows []TableStatsRow
	err := r.db.WithContext(ctx).
		Table("pg_stat_user_tables").
		Select("relname AS table_name, GREATEST(n_live_tup, 0) AS rows").
		Order("n_live_tup DESC").Limit(10).
		Scan(&rows).Error
	return rows, err
}

type BackupListRow struct {
	ID         uint
	BackupName string
	BackupType string
	Status     string
	FileSize   int64
	CreatedAt  string
}

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

func (r *BackupGapRepo) CoreTableStats(ctx context.Context, tables []string) ([]TableStatsRow, error) {
	rows := make([]TableStatsRow, 0, len(tables))
	for _, t := range tables {
		var stat TableStatsRow
		if err := r.db.WithContext(ctx).
			Table("pg_stat_user_tables").
			Select("?::text AS table_name, GREATEST(n_live_tup,0) AS rows", t).
			Where("relname = ?", t).
			Scan(&stat).Error; err == nil && stat.Table != "" {
			rows = append(rows, stat)
		}
	}
	return rows, nil
}

type RagEvalGapRepo struct {
	db *gorm.DB
}

func NewRagEvalGapRepo() *RagEvalGapRepo { return NewRagEvalGapRepoWithDB(db.GetDB()) }

// NewRagEvalGapRepoWithDB 装配/测试注入（nil db 返回 nil，service 以 repo == nil 守卫）。
func NewRagEvalGapRepoWithDB(gdb *gorm.DB) *RagEvalGapRepo {
	if gdb == nil {
		return nil
	}
	return &RagEvalGapRepo{db: gdb}
}

func (r *RagEvalGapRepo) CreateQuestion(ctx context.Context, q *model.RagEvalQuestion) error {
	return r.db.WithContext(ctx).Create(q).Error
}

// CreateQuestionsInTx 批量落评测题库：单事务，任一行失败整体回滚。
//
// UploadCSV 原逐行 CreateQuestion（无事务），中断/中途失败会留下部分已提交行；
// 收口为整批一事务。批内按 500 行分批 INSERT（CreateInBatches），既避免单条
// INSERT 撞 Postgres 65535 参数上限（本表 16 列 ≈ 4000 行/语句），又不破坏
// 事务原子性——分批语句同处一个 tx，失败一并回滚。
func (r *RagEvalGapRepo) CreateQuestionsInTx(ctx context.Context, questions []*model.RagEvalQuestion) error {
	if len(questions) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.CreateInBatches(&questions, 500).Error
	})
}

func (r *RagEvalGapRepo) CreateRun(ctx context.Context, run *model.RagEvalRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

// MarkRunFailed 把评测标记为失败：total=-2 是「无有效结果」的哨兵值（前端显示 '-'），
// 同时落 status/error_msg，避免列表里只剩一行 0 指标让人误以为跑成功。
func (r *RagEvalGapRepo) MarkRunFailed(ctx context.Context, runID uint, msg string) error {
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).
		Updates(map[string]any{
			"total":         -2,
			"eval_set_size": 0,
			"status":        "failed",
			"error_msg":     msg,
		}).Error
}

// MarkInterruptedRuns 把上个进程残留的 running 行标记为失败。
// 评测在后台 goroutine 里跑，进程重启后这些行不会再有结果，不清理会永远显示「进行中」。
func (r *RagEvalGapRepo) MarkInterruptedRuns(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		// running：上次进程的 goroutine 已死；total=-1 且 completed：历史占位行从未回填结果，同样属中断
		Where("status = ? OR (status = ? AND total = ?)", "running", "completed", -1).
		Updates(map[string]any{
			"status":    "failed",
			"total":     -2,
			"error_msg": "评测被中断（服务重启），请重新运行",
		})
	return res.RowsAffected, res.Error
}

func (r *RagEvalGapRepo) UpdateRunResult(ctx context.Context, runID uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.RagEvalRun{}).
		Where("id = ?", runID).Updates(updates).Error
}

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

func (r *RagEvalGapRepo) CreateRunCompleted(ctx context.Context, run *model.RagEvalRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

type CohortGapRepo struct {
	db *gorm.DB
}

func NewCohortGapRepo() *CohortGapRepo { return NewCohortGapRepoWithDB(db.GetDB()) }

// NewCohortGapRepoWithDB 装配/测试注入（nil db 返回 nil，service 以 repo == nil 守卫）。
func NewCohortGapRepoWithDB(gdb *gorm.DB) *CohortGapRepo {
	if gdb == nil {
		return nil
	}
	return &CohortGapRepo{db: gdb}
}

func (r *CohortGapRepo) CountCustomersBetween(ctx context.Context, start, end time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Customer{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&n).Error
	return n, err
}

func (r *CohortGapRepo) CountRetainedEvents(ctx context.Context, cohortStart, cohortEnd, weekStart, weekEnd time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerEvent{}).
		Joins("JOIN customers ON customers.unified_id = customer_events.customer_id OR customers.id::text = customer_events.customer_id").
		Where("customers.created_at >= ? AND customers.created_at < ?", cohortStart, cohortEnd).
		Where("customer_events.created_at >= ? AND customer_events.created_at < ?", weekStart, weekEnd).
		Distinct("customer_events.customer_id").
		Count(&n).Error
	return n, err
}

type CustomerEventRow struct {
	CustomerID string
	EventType  string
	CreatedAt  time.Time
}

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

type EmailGapRepo struct {
	db *gorm.DB
}

func NewEmailGapRepo() *EmailGapRepo { return NewEmailGapRepoWithDB(db.GetDB()) }

// NewEmailGapRepoWithDB 装配/测试注入（nil db 返回 nil，service 以 repo == nil 守卫）。
func NewEmailGapRepoWithDB(gdb *gorm.DB) *EmailGapRepo {
	if gdb == nil {
		return nil
	}
	return &EmailGapRepo{db: gdb}
}

func (r *EmailGapRepo) CountSent(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("created_at >= ?", since).Count(&n).Error
	return n, err
}

func (r *EmailGapRepo) CountEventByType(ctx context.Context, eventType string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("event_type = ? AND timestamp >= ?", eventType, since).Count(&n).Error
	return n, err
}

func (r *EmailGapRepo) ListBounces(ctx context.Context, since time.Time) ([]model.EmailTrackingEvent, error) {
	var evs []model.EmailTrackingEvent
	err := r.db.WithContext(ctx).
		Where("event_type IN ? AND timestamp >= ?", []string{"bounce", "soft_bounce", "hard_bounce"}, since).
		Order("timestamp DESC").Limit(2000).Find(&evs).Error
	return evs, err
}

type BounceByISPRow struct {
	Domain string `gorm:"column:domain"`
	Cnt    int64  `gorm:"column:cnt"`
}

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

type SMTPServerRow struct {
	Host string `gorm:"column:host"`
	User string `gorm:"column:username"`
}

func (r *EmailGapRepo) ListSMTPServers(ctx context.Context) ([]SMTPServerRow, error) {
	var rows []SMTPServerRow
	err := r.db.WithContext(ctx).Table("email_smtp").
		Select("server AS host, username").Where("deleted_at IS NULL").Limit(10).Scan(&rows).Error
	return rows, err
}

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

func (r *EmailGapRepo) CountSentToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailSend{}).
		Where("to LIKE ? AND created_at >= ?", "%@"+domain, since).Count(&n).Error
	return n, err
}

func (r *EmailGapRepo) CountEventToDomain(ctx context.Context, eventType string, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type IN ? AND timestamp >= ?", "%@"+domain, []string{eventType}, since).Count(&n).Error
	return n, err
}

func (r *EmailGapRepo) CountSpamReportToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type = ? AND timestamp >= ?", "%@"+domain, "spam_report", since).Count(&n).Error
	return n, err
}

func (r *EmailGapRepo) CountBouncesToDomain(ctx context.Context, domain string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.EmailTrackingEvent{}).
		Where("email LIKE ? AND event_type IN ? AND timestamp >= ?", "%@"+domain,
			[]string{"bounce", "soft_bounce", "hard_bounce"}, since).Count(&n).Error
	return n, err
}

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

func (r *EmailGapRepo) UpdateClueFields(ctx context.Context, clueID int64, updates map[string]any) error {
	return r.db.WithContext(ctx).Table("clues").
		Where("id = ?", clueID).Updates(updates).Error
}

func (r *EmailGapRepo) CreateClueRecord(ctx context.Context, rec map[string]any) error {
	return r.db.WithContext(ctx).Table("clues").Create(rec).Error
}
