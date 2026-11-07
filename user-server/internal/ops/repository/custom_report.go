package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/ops/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// CustomReportRepository 自定义报表仓库
type CustomReportRepository struct {
	db *gorm.DB
}

// NewCustomReportRepository 创建自定义报表仓库
func NewCustomReportRepository() *CustomReportRepository {
	return &CustomReportRepository{
		db: _db.GetDB(),
	}
}

// Create 创建报表
func (r *CustomReportRepository) Create(ctx context.Context, report *model.CustomReport) error {
	return r.db.WithContext(ctx).Create(report).Error
}

// GetByID 根据 ID 获取报表
func (r *CustomReportRepository) GetByID(ctx context.Context, id uint) (*model.CustomReport, error) {
	var report model.CustomReport
	err := r.db.WithContext(ctx).First(&report, id).Error
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// GetAll 获取所有报表列表。
//
// Count 与 Find 必须各起一条链：同一 *gorm.DB 上先 Count 再追加分页子句，
// GORM 会把分页子句叠进 Count 语句（判例 #133/#150）。分页夹取 1..200。
func (r *CustomReportRepository) GetAll(ctx context.Context, page, pageSize int) ([]*model.CustomReport, int64, error) {
	page, pageSize = clampPage(page, pageSize)
	var reports []*model.CustomReport
	var total int64

	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.CustomReport{})
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := base().
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&reports).Error
	if err != nil {
		return nil, 0, err
	}

	return reports, total, nil
}

// GetByCreatorOrPublic 获取当前用户创建的或公开的报表列表（普通用户视角；
// Count/Find 拆链 + 夹取，理由同 GetAll）。
func (r *CustomReportRepository) GetByCreatorOrPublic(ctx context.Context, creatorID uint, page, pageSize int) ([]*model.CustomReport, int64, error) {
	page, pageSize = clampPage(page, pageSize)
	var reports []*model.CustomReport
	var total int64

	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.CustomReport{}).
			Where("created_by = ? OR is_public = ?", creatorID, true)
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := base().
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&reports).Error
	if err != nil {
		return nil, 0, err
	}

	return reports, total, nil
}

// Update 整行回写报表。走 db.SaveWholeRow：裸 Save 命中 0 行会回退成
// `INSERT ... ON CONFLICT (id) DO UPDATE`（gorm@v1.31.2），把「编辑页读出后、
// 保存前被删掉」的报表按原 id 插回。
func (r *CustomReportRepository) Update(ctx context.Context, report *model.CustomReport) error {
	if report == nil || report.ID == 0 {
		return errors.New("custom report: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), report)
}

// Delete 删除报表。0 行 ⇒ ErrRecordNotFound。
func (r *CustomReportRepository) Delete(ctx context.Context, id uint) error {
	return _db.EnsureAffected(r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CustomReport{}))
}

// GetPublicTemplates 获取公开报表模板（limit 兜底 200：模板是运营手工建的
// 配置型数据，行数可控，但无界 Find 的习惯不能留）。
func (r *CustomReportRepository) GetPublicTemplates(ctx context.Context) ([]*model.CustomReport, error) {
	var reports []*model.CustomReport
	err := r.db.WithContext(ctx).Where("is_public = ?", true).
		Order("created_at DESC").
		Limit(200).
		Find(&reports).Error
	return reports, err
}

// UseTemplate 使用模板创建报表
func (r *CustomReportRepository) UseTemplate(ctx context.Context, templateID uint, createdBy uint) (*model.CustomReport, error) {
	var template model.CustomReport
	err := r.db.WithContext(ctx).First(&template, templateID).Error
	if err != nil {
		return nil, err
	}

	report := &model.CustomReport{
		Name:        template.Name,
		Description: template.Description,
		DataSource:  template.DataSource,
		Dimensions:  template.Dimensions,
		Metrics:     template.Metrics,
		Filters:     template.Filters,
		ChartType:   template.ChartType,
		ChartConfig: template.ChartConfig,
		IsPublic:    false,
		CreatedBy:   createdBy,
	}

	err = r.db.WithContext(ctx).Create(report).Error
	return report, err
}

// NewCustomReportRepositoryWithDB 创建指定数据库连接的自定义报表仓库实例（用于测试）
func NewCustomReportRepositoryWithDB(db *gorm.DB) *CustomReportRepository {
	return &CustomReportRepository{db: db}
}

// ============================================================================
// 报表数据源聚合查询（AV-04c 批次7 刀2 自 ops/service/custom_report.go 下沉）。
//
// 五段聚合 SQL（sessions/messages/clues/users/agents）与维度表达式映射逐字保留；
// 谓词拼接 applyConds 自 service/report_filter.go 下沉（逻辑逐字保留），过滤条件
// 由 service 侧 BuildReportFilterSQL 按字段白名单解析后传入。
// ============================================================================

// ApplyConds 报表过滤谓词拼接（自 service/report_filter.go 下沉，逻辑逐字保留）。
// conds 为空时原样返回 base；否则按 AND 合并为一条 Where。
func ApplyConds(base *gorm.DB, conds []string, args []any) *gorm.DB {
	if len(conds) == 0 {
		return base
	}
	return base.Where(strings.Join(conds, " AND "), args...)
}

// CustomReportAggRow 通用维度聚合行（messages/clues/users 三源共用）。
type CustomReportAggRow struct {
	DimValue string
	Count    int64
}

// CustomReportSessionAggRow 会话源聚合行（会话数 + 消息数双指标）。
type CustomReportSessionAggRow struct {
	DimValue     string
	SessionCount int64
	MessageSum   int64
}

// CustomReportAgentAggRow 智能体维度聚合行。
type CustomReportAgentAggRow struct {
	AgentName       string
	SessionCount    int64
	AvgResponseTime float64
}

// AggregateSessionsByDim 按维度聚合 customer_sessions（dimField→SQL 表达式映射逐字保留）。
// 与 messages 源 / report_filter 白名单对齐：platform 是合法维度，不能静默落到
// default 的 status 列（曾把平台分布画成会话状态分布）。
func (r *CustomReportRepository) AggregateSessionsByDim(ctx context.Context, dimField string, conds []string, args []any) ([]CustomReportSessionAggRow, error) {
	var groupExpr, dimValueExpr string // 分支（含 default）各自赋值
	switch dimField {
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	case "agent_name":
		groupExpr = "COALESCE(NULLIF(agent_name, ''), '未分配')"
		dimValueExpr = "COALESCE(NULLIF(agent_name, ''), '未分配')"
	case "platform":
		groupExpr = "COALESCE(NULLIF(platform, ''), 'unknown')"
		dimValueExpr = "COALESCE(NULLIF(platform, ''), 'unknown')"
	default:
		groupExpr = "status"
		dimValueExpr = "status::text"
	}

	q := r.db.WithContext(ctx).Table("customer_sessions").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS session_count, COALESCE(SUM(message_count), 0) AS message_sum").
		Group(groupExpr).
		Order(groupExpr)
	q = ApplyConds(q, conds, args)
	var aggs []CustomReportSessionAggRow
	if err := q.Scan(&aggs).Error; err != nil {
		return nil, err
	}
	return aggs, nil
}

// AggregateMessagesByDim 按维度聚合 unified_messages（dimField→SQL 表达式映射逐字保留）。
func (r *CustomReportRepository) AggregateMessagesByDim(ctx context.Context, dimField string, conds []string, args []any) ([]CustomReportAggRow, error) {
	var dimValueExpr, groupExpr string // 三个分支（含 default）各自赋值
	switch dimField {
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	case "platform":
		groupExpr = "COALESCE(platform, 'unknown')"
		dimValueExpr = "COALESCE(platform, 'unknown')"
	default:
		groupExpr = "COALESCE(content_type, 'unknown')"
		dimValueExpr = "COALESCE(content_type, 'unknown')"
	}

	q := r.db.WithContext(ctx).
		Table("unified_messages").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = ApplyConds(q, conds, args)
	var aggs []CustomReportAggRow
	if err := q.Scan(&aggs).Error; err != nil {
		return nil, err
	}
	return aggs, nil
}

// AggregateCluesByDim 按维度聚合 clues（dimField→SQL 表达式映射逐字保留）。
func (r *CustomReportRepository) AggregateCluesByDim(ctx context.Context, dimField string, conds []string, args []any) ([]CustomReportAggRow, error) {
	var dimValueExpr, groupExpr string // 各分支（含 default）各自赋值
	switch dimField {
	case "is_verify":
		dimValueExpr = "is_verify::text"
		groupExpr = "is_verify"
	case "level":
		dimValueExpr = "COALESCE(NULLIF(level, ''), 'warm')"
		groupExpr = "COALESCE(NULLIF(level, ''), 'warm')"
	case "is_group":
		dimValueExpr = "is_group::text"
		groupExpr = "is_group"
	default:
		dimValueExpr = "type::text"
		groupExpr = "type"
	}

	q := r.db.WithContext(ctx).Table("clues").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = ApplyConds(q, conds, args)
	var aggs []CustomReportAggRow
	if err := q.Scan(&aggs).Error; err != nil {
		return nil, err
	}
	return aggs, nil
}

// AggregateUsersByDim 按维度聚合 customers（dimField→SQL 表达式映射逐字保留）。
func (r *CustomReportRepository) AggregateUsersByDim(ctx context.Context, dimField string, conds []string, args []any) ([]CustomReportAggRow, error) {
	var groupExpr, dimValueExpr string // 三个分支（含 default）各自赋值
	switch dimField {
	case "user_type", "churn_risk":
		groupExpr = "churn_risk"
		dimValueExpr = "churn_risk::text"
	case "date", "created_at":
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	default:
		groupExpr = "DATE(created_at)"
		dimValueExpr = "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	}

	q := r.db.WithContext(ctx).
		Table("customers").
		Select(dimValueExpr + " AS dim_value, COUNT(*) AS count").
		Group(groupExpr).
		Order(groupExpr)
	q = ApplyConds(q, conds, args)
	var aggs []CustomReportAggRow
	if err := q.Scan(&aggs).Error; err != nil {
		return nil, err
	}
	return aggs, nil
}

// AggregateAgentSessions 按坐席聚合会话数与平均响应时长（SQL 逐字保留）。
func (r *CustomReportRepository) AggregateAgentSessions(ctx context.Context) ([]CustomReportAgentAggRow, error) {
	var aggs []CustomReportAgentAggRow
	if err := r.db.WithContext(ctx).
		Table("customer_sessions").
		Select("agent_name AS agent_name, COUNT(*) AS session_count, COALESCE(AVG(avg_response_time), 0) AS avg_response_time").
		Where("agent_name IS NOT NULL AND agent_name <> ''").
		Group("agent_name").
		Order("session_count DESC").
		Scan(&aggs).Error; err != nil {
		return nil, err
	}
	return aggs, nil
}
