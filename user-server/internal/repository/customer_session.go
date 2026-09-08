// Package repository 数据库仓库层
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// DefaultSessionActiveTTL 客服会话的默认活跃 TTL
//
// 设计：
//   - 24h 内有消息互动的会话视为「活跃」，AI / 坐席可继续复用
//   - 超过 24h 未互动的会话自动 close（由 service.CustomerSessionService.AutoCloseStaleSessions 定时任务驱动）
//   - 该常量与 service.CustomerSessionActiveTTL 必须保持单一源（service 层会覆盖本地为同值）
//
// 修改本值需同步：service/customer_session.go CustomerSessionActiveTTL + DEVELOPMENT.md。
const DefaultSessionActiveTTL = 24 * time.Hour

// CustomerSessionRepository 客服会话仓库
type CustomerSessionRepository struct {
	db *gorm.DB
}

// GetDB 返回数据库连接
func (r *CustomerSessionRepository) GetDB(ctx context.Context) *gorm.DB {
	return r.db
}

// NewCustomerSessionRepository 创建客服会话仓库实例
func NewCustomerSessionRepository() *CustomerSessionRepository {
	return &CustomerSessionRepository{
		db: _db.GetDB(),
	}
}

// NewCustomerSessionRepositoryWithDB 创建指定数据库连接的 CustomerSessionRepository 实例（用于测试）
func NewCustomerSessionRepositoryWithDB(db *gorm.DB) *CustomerSessionRepository {
	return &CustomerSessionRepository{
		db: db,
	}
}

// Create 创建会话
func (r *CustomerSessionRepository) Create(ctx context.Context, session *model.CustomerSession) error {
	return r.db.Create(session).Error
}

// Update 更新会话
func (r *CustomerSessionRepository) Update(ctx context.Context, session *model.CustomerSession) error {
	return r.db.Save(session).Error
}

// GetByID 根据ID获取会话
func (r *CustomerSessionRepository) GetByID(ctx context.Context, id uint) (*model.CustomerSession, error) {
	var session model.CustomerSession
	err := r.db.First(&session, id).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// GetByIDString 根据字符串类型主键获取会话（用于稳定 session_id 派生场景）
func (r *CustomerSessionRepository) GetByIDString(ctx context.Context, id string) (*model.CustomerSession, error) {
	var session model.CustomerSession
	if err := r.db.Where("session_id = ?", id).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *CustomerSessionRepository) UpsertByOneID(ctx context.Context, platform, accountID, oneID, userID, userName, lastMessage string, lastMessageAt *time.Time, dncBlocked bool) (string, error) {
	stableID := fmt.Sprintf("sess_%s_%s_%s", platform, accountID, oneID)
	now := time.Now()
	if lastMessageAt == nil {
		lastMessageAt = &now
	}
	insertSQL := `
INSERT INTO customer_sessions
  (session_id, platform, account_id, user_id, one_id, user_name, status, handler_type,
   last_message, last_message_at, last_message_by, message_count, ai_reply_count, human_reply_count, avg_response_time, rating, dnc_blocked, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (session_id) DO NOTHING
`
	err := r.db.WithContext(ctx).Exec(insertSQL,
		stableID, platform, accountID, userID, oneID, userName,
		model.SessionStatusPending, model.HandlerTypeAI,
		lastMessage, lastMessageAt, "user", 0, 0, 0, 0, 0, dncBlocked,
		&now, &now,
	).Error
	if err != nil {
		return "", fmt.Errorf("upsert insert: %w", err)
	}
	return stableID, nil
}

// GetBySessionID 根据SessionID获取会话
func (r *CustomerSessionRepository) GetBySessionID(ctx context.Context, sessionID string) (*model.CustomerSession, error) {
	var session model.CustomerSession
	err := r.db.Where("session_id = ?", sessionID).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *CustomerSessionRepository) GetByMerchant(ctx context.Context, status model.SessionStatus, page, pageSize int) ([]*model.CustomerSession, int64, error) {
	var sessions []*model.CustomerSession
	var total int64

	query := r.db.Model(&model.CustomerSession{}).Where("1 = 1")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize

	err = query.Order("priority DESC, last_message_at DESC").Offset(offset).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}

// GetByUserID 获取某用户的所有会话（CC- N+1 优化）
//
// 替代 service 层「GetByMerchant(空 merchant, 1, 1000) + 内存过滤」模式，
// 直接按 user_id 走索引，单 SQL 拉全该用户会话。
func (r *CustomerSessionRepository) GetByUserID(ctx context.Context, userID string) ([]*model.CustomerSession, error) {
	if userID == "" {
		return nil, nil
	}
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// GetByOneID 按客户 OneID（unified_id）拉取该客户的所有会话。
//
// 客户档案（customers 表）的主键 id 与客户会话（customer_sessions 表）的 user_id 字段
// 语义不同：会话的 user_id 是会话创建时的访客 ID（如 web_embed 的 v_xxx），而真正关联到
// 客户档案的键是 one_id（= customers.unified_id）。因此「按客户 id 查 360」应先解析出
// unified_id，再用本方法按 one_id 查询会话，而不是把客户 id 直接当 user_id 查。
func (r *CustomerSessionRepository) GetByOneID(ctx context.Context, oneID string) ([]*model.CustomerSession, error) {
	if oneID == "" {
		return nil, nil
	}
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("one_id = ?", oneID).
		Order("created_at ASC").
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// ListByOneIDsBatch 批量按 one_id 拉取会话，返回按 one_id 分组的 map（CC- N+1 优化）
//
// 用于「客户列表 → 每客户 360 视图」场景：单次 SQL 拉所有客户的会话，
// 避免 N 个客户各跑一次 GetByOneID 造成的 N+1。
func (r *CustomerSessionRepository) ListByOneIDsBatch(ctx context.Context, oneIDs []string) (map[string][]*model.CustomerSession, error) {
	result := make(map[string][]*model.CustomerSession, len(oneIDs))
	if len(oneIDs) == 0 {
		return result, nil
	}
	unique := make([]string, 0, len(oneIDs))
	seen := make(map[string]struct{}, len(oneIDs))
	for _, id := range oneIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var sessions []*model.CustomerSession
	if err := r.db.WithContext(ctx).
		Where("one_id IN ?", unique).
		Order("created_at ASC").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	for _, s := range sessions {
		oid := s.OneID
		result[oid] = append(result[oid], s)
	}
	return result, nil
}

// ListByUserIDsBatch 批量按 user_id 拉取会话，返回按 user_id 分组的 map（CC- N+1 优化）
//
// 用于「客户列表 → 每用户 360 视图」场景：单次 SQL 拉所有用户的会话，
// 避免 N 个用户各跑一次 GetByUserID 造成的 N+1。
//   - userIDs: 待查询的 user id 列表，空时返回 (empty map, nil)
//   - 入参去重 + 跳过空串
//
// 返回值：map[userID][]*CustomerSession，未命中的 userID 不会出现在 map 中。
func (r *CustomerSessionRepository) ListByUserIDsBatch(ctx context.Context, userIDs []string) (map[string][]*model.CustomerSession, error) {
	result := make(map[string][]*model.CustomerSession, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	unique := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("user_id IN ?", unique).
		Order("user_id ASC, created_at ASC").
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		result[s.UserID] = append(result[s.UserID], s)
	}
	return result, nil
}

// GetPendingSessions 获取等待处理的会话
func (r *CustomerSessionRepository) GetPendingSessions(ctx context.Context) ([]*model.CustomerSession, error) {
	var sessions []*model.CustomerSession
	q := r.db.Where("status IN ?", []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
	}).Order("priority DESC, last_message_at ASC")
	// 无界兜底:默认 200,防止会话表增长后定时分配任务全表载入;已达上限时调用方需分页重取
	q = applyListLimit(q, 200)
	err := q.Find(&sessions).Error
	return sessions, err
}

// GetAgentSessions 获取客服的活跃会话
func (r *CustomerSessionRepository) GetAgentSessions(ctx context.Context, agentID uint) ([]*model.CustomerSession, error) {
	var sessions []*model.CustomerSession
	err := r.db.Where("agent_id = ? AND status IN ?", agentID, []model.SessionStatus{
		model.SessionStatusHumanHandling,
		model.SessionStatusWaiting,
	}).Order("last_message_at DESC").Find(&sessions).Error
	return sessions, err
}

// GetActiveByUserID 根据用户 ID 获取其当前活跃会话（用于营销流程 assign_agent 动作）。
// 活跃状态：pending / ai_handling / waiting / human_handling。
// 若存在多条，返回最近一条有消息记录的会话。
//
// 24h TTL（与 service.CustomerSessionActiveTTL 对齐）：只返回 last_message_at 在
// SessionActiveTTL 之内的会话。超过 24h 未互动的会话视为历史会话，避免被复用导致
// AI 上下文被无关历史污染。
func (r *CustomerSessionRepository) GetActiveByUserID(ctx context.Context, userID string) (*model.CustomerSession, error) {
	if userID == "" {
		return nil, errors.New("user_id 不能为空")
	}
	cutoff := time.Now().Add(-DefaultSessionActiveTTL)
	var session model.CustomerSession
	err := r.db.Where("user_id = ? AND status IN ?", userID, []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
		model.SessionStatusHumanHandling,
	}).Where("COALESCE(last_message_at, created_at) > ?", cutoff).
		Order("last_message_at DESC, id DESC").First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// GetActiveByUserIDWithin 根据用户 ID 在指定时间窗口内获取活跃会话
//
// 用于：① 业务自定义 TTL（不像 24h 那么严苛）② 测试注入自定义窗口。
// 命中条件：status IN 活跃状态 且 (last_message_at OR created_at) > cutoff。
func (r *CustomerSessionRepository) GetActiveByUserIDWithin(ctx context.Context, userID string, within time.Duration) (*model.CustomerSession, error) {
	if userID == "" {
		return nil, errors.New("user_id 不能为空")
	}
	cutoff := time.Now().Add(-within)
	var session model.CustomerSession
	err := r.db.Where("user_id = ? AND status IN ?", userID, []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
		model.SessionStatusHumanHandling,
	}).Where("COALESCE(last_message_at, created_at) > ?", cutoff).
		Order("last_message_at DESC, id DESC").First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// GetActiveByOneID 根据 OneID 获取活跃会话（跨渠道合并辅助键；S3-1）
//
// 业务场景：用户从 web 客服进 → OneID 已记录；后从 Telegram 进 → 新的 user_id。
// 优先按 OneID 匹配，复用前一会话（同一客户连续对话）。
//
// 命中条件：one_id = ? AND status IN 活跃状态 AND 在 TTL 内。
// one_id 为空时直接返回 nil（让调用方降级走 user_id 路径）。
func (r *CustomerSessionRepository) GetActiveByOneID(ctx context.Context, oneID string) (*model.CustomerSession, error) {
	if oneID == "" {
		return nil, nil
	}
	cutoff := time.Now().Add(-DefaultSessionActiveTTL)
	var session model.CustomerSession
	err := r.db.Where("one_id = ? AND status IN ?", oneID, []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
		model.SessionStatusHumanHandling,
	}).Where("COALESCE(last_message_at, created_at) > ?", cutoff).
		Order("last_message_at DESC, id DESC").First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

// ListStaleActiveSessions 列出超过指定时间窗口仍处于「活跃」状态的会话
//
// 用于会话 24h TTL 自动关闭任务的扫描：批量拉取活跃但超时的会话，
// 避免一次性 UPDATE 全表锁竞争。
func (r *CustomerSessionRepository) ListStaleActiveSessions(ctx context.Context, within time.Duration, limit int) ([]*model.CustomerSession, error) {
	cutoff := time.Now().Add(-within)
	var sessions []*model.CustomerSession
	err := r.db.Where("status IN ?", []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
		model.SessionStatusHumanHandling,
	}).Where("COALESCE(last_message_at, created_at) <= ?", cutoff).
		Order("COALESCE(last_message_at, created_at) ASC").
		Limit(limit).
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// AutoCloseStaleSessions 批量关闭超过 TTL 的活跃会话
//
// 返回：实际关闭数（rows affected）。
// 关闭：status=closed, closed_at=now()。
// 仅作用于「活跃」状态（pending/ai_handling/waiting/human_handling），
// 已 resolved/closed 的会话不会被重复关闭。
//
// batchSize > 0 时通过 LIMIT 控制单次 UPDATE 影响的行数（避免大表锁竞争）。
// batchSize == 0 时一次性处理所有匹配行。
//
// 重要：PostgreSQL 不允许在 UPDATE … SET 中直接使用 LIMIT，标准 SQL 也不支持。
// 实现方式：先在子查询中按 last_message_at 升序取 batchSize 个 id，再按 id 集合 UPDATE。
// 这样既限定批次又不破坏 GORM 跨方言抽象。
func (r *CustomerSessionRepository) AutoCloseStaleSessions(ctx context.Context, within time.Duration, batchSize int) (int64, error) {
	cutoff := time.Now().Add(-within)
	now := time.Now()
	updates := map[string]any{
		"status":    model.SessionStatusClosed,
		"closed_at": &now,
	}
	if batchSize <= 0 {
		res := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
			Where("status IN ?", []model.SessionStatus{
				model.SessionStatusPending,
				model.SessionStatusAIHandling,
				model.SessionStatusWaiting,
				model.SessionStatusHumanHandling,
			}).
			Where("COALESCE(last_message_at, created_at) <= ?", cutoff).
			Updates(updates)
		if res.Error != nil {
			return 0, res.Error
		}
		return res.RowsAffected, nil
	}

	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("status IN ?", []model.SessionStatus{
			model.SessionStatusPending,
			model.SessionStatusAIHandling,
			model.SessionStatusWaiting,
			model.SessionStatusHumanHandling,
		}).
		Where("COALESCE(last_message_at, created_at) <= ?", cutoff).
		Order("COALESCE(last_message_at, created_at) ASC").
		Limit(batchSize).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("id IN ?", ids).
		Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// UpdateStatus 更新会话状态
func (r *CustomerSessionRepository) UpdateStatus(ctx context.Context, id uint, status model.SessionStatus) error {
	updates := map[string]any{"status": status}
	if status == model.SessionStatusResolved {
		now := time.Now()
		updates["resolved_at"] = &now
	} else if status == model.SessionStatusClosed {
		now := time.Now()
		updates["closed_at"] = &now
	}
	return r.db.Model(&model.CustomerSession{}).Where("id = ?", id).Updates(updates).Error
}

// AssignAgent 分配客服
func (r *CustomerSessionRepository) AssignAgent(ctx context.Context, id uint, agentID uint, agentName string) error {
	return r.db.Model(&model.CustomerSession{}).Where("id = ?", id).Updates(map[string]any{
		"agent_id":     agentID,
		"agent_name":   agentName,
		"handler_type": model.HandlerTypeHuman,
		"status":       model.SessionStatusHumanHandling,
	}).Error
}

// UpdateLastMessage 更新最后消息
func (r *CustomerSessionRepository) UpdateLastMessage(ctx context.Context, id uint, message string, messageBy string) error {
	now := time.Now()
	return r.db.Model(&model.CustomerSession{}).Where("id = ?", id).Updates(map[string]any{
		"last_message":    message,
		"last_message_at": &now,
		"last_message_by": messageBy,
	}).Update("message_count", gorm.Expr("message_count + 1")).Error
}

// UpdateLastMessageBySessionID 按 session_id 更新最后消息
//
// SOP 节点执行器只知道 session_id（业务字段），不知道 DB 主键 id，
// 此方法封装按 session_id 更新操作，便于执行器直接调用。
// 失败不阻塞 SOP 流程（best-effort 语义）。
func (r *CustomerSessionRepository) UpdateLastMessageBySessionID(ctx context.Context, sessionID string, message string, messageBy string) error {
	if sessionID == "" {
		return errors.New("session_id is empty")
	}
	now := time.Now()
	return r.db.Model(&model.CustomerSession{}).Where("session_id = ?", sessionID).Updates(map[string]any{
		"last_message":    message,
		"last_message_at": &now,
		"last_message_by": messageBy,
	}).Update("message_count", gorm.Expr("message_count + 1")).Error
}

// IncrementAIReplyCount 增加AI回复计数
func (r *CustomerSessionRepository) IncrementAIReplyCount(ctx context.Context, id uint) error {
	return r.db.Model(&model.CustomerSession{}).Where("id = ?", id).
		Update("ai_reply_count", gorm.Expr("ai_reply_count + 1")).Error
}

// IncrementHumanReplyCount 增加人工回复计数
func (r *CustomerSessionRepository) IncrementHumanReplyCount(ctx context.Context, id uint) error {
	return r.db.Model(&model.CustomerSession{}).Where("id = ?", id).
		Update("human_reply_count", gorm.Expr("human_reply_count + 1")).Error
}

// UpdateFirstHumanReplyAt D20：转人工 episode 终点打点（幂等语义由调用方保证：
// 仅在 handoff_at 非空且 first_human_reply_at 为空时调用；此处条件更新双保险防并发覆盖）
func (r *CustomerSessionRepository) UpdateFirstHumanReplyAt(ctx context.Context, id uint, at time.Time) error {
	return r.db.Model(&model.CustomerSession{}).
		Where("id = ? AND handoff_at IS NOT NULL AND first_human_reply_at IS NULL", id).
		Update("first_human_reply_at", at).Error
}

// UpdateFields 按主键原子更新部分字段（不覆盖全对象）
//
// 用于 incrementAIReplyCount 等场景：先 IncrementAIReplyCount 原子 +1，
// 再 UpdateFields 更新 status / last_message_at 等附属字段。
// 避免 sessionRepo.Update(ctx, session) 把内存中的零值 message_count
// 回写到 DB，破坏其他原子操作。
func (r *CustomerSessionRepository) UpdateFields(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("id = ?", id).Updates(fields).Error
}

// UpdateHandlerType 更新会话处理方类型（ai / human）
//
// 用于关键词触发自动转人工但 AutoAssign 失败的场景：
// 仅修改 handler_type，不动 status（由调用方另行控制）。
func (r *CustomerSessionRepository) UpdateHandlerType(ctx context.Context, id uint, handlerType model.HandlerType) error {
	return r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("id = ?", id).
		Update("handler_type", handlerType).Error
}

// GetBySessionIDPlatformAccountUser 按 (session_id, platform, account_id, user_id) 查询会话
//
// 用于访客侧按 session_id 拉取会话时校验归属：
// 仅当 4 元组完全匹配才返回，避免越权访问。
// 未命中返回 (nil, gorm.ErrRecordNotFound)（与 service 层历史 errors.Is 检查兼容）。
func (r *CustomerSessionRepository) GetBySessionIDPlatformAccountUser(
	ctx context.Context, sessionID string, platform model.Platform,
	accountID, userID string,
) (*model.CustomerSession, error) {
	var session model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND platform = ? AND account_id = ? AND user_id = ?",
			sessionID, platform, accountID, userID).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// GetLatestActiveByPlatformAccountUser 拉取最近一条未结束会话
//
// 用于访客 OpenSession(Resume=true) 与离线消息续接：
//   - 按 (platform, account_id, user_id) 过滤
//   - 排除已结束状态（excludeStatuses，通常为 [resolved, closed]）
//   - 按 last_message_at DESC NULLS LAST, created_at DESC 排序，取首条
//
// 未命中返回 (nil, gorm.ErrRecordNotFound)（与历史行为一致）。
func (r *CustomerSessionRepository) GetLatestActiveByPlatformAccountUser(
	ctx context.Context, platform model.Platform,
	accountID, userID string, excludeStatuses []model.SessionStatus,
) (*model.CustomerSession, error) {
	var session model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND user_id = ?", platform, accountID, userID).
		Where("status NOT IN ?", excludeStatuses).
		Order("last_message_at DESC NULLS LAST, created_at DESC").
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// ListRecentClosedByPlatformAccountUser 拉取最近 N 天内已结束会话
//
// 用于访客离线消息列表展示：
//   - 按 (platform, account_id, user_id) 过滤
//   - status IN includeStatuses（通常为 [resolved, closed]）
//   - created_at > since
//   - 按 created_at DESC 排序，limit 限制条数
func (r *CustomerSessionRepository) ListRecentClosedByPlatformAccountUser(
	ctx context.Context, platform model.Platform,
	accountID, userID string, includeStatuses []model.SessionStatus,
	since time.Time, limit int,
) ([]*model.CustomerSession, error) {
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND user_id = ?", platform, accountID, userID).
		Where("status IN ?", includeStatuses).
		Where("created_at > ?", since).
		Order("created_at DESC").
		Limit(limit).
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// UpdateTags 更新会话标签（JSON 数组字符串；标签规则 ApplyTagRule 使用）
func (r *CustomerSessionRepository) UpdateTags(ctx context.Context, sessionID, tagsJSON string) error {
	return r.db.WithContext(ctx).
		Model(&model.CustomerSession{}).
		Where("session_id = ?", sessionID).
		Update("tags", tagsJSON).Error
}

// CreateSegment 创建分群
func (r *CustomerSessionRepository) CreateSegment(ctx context.Context, seg *model.CustomerSegment) error {
	return r.db.WithContext(ctx).Create(seg).Error
}

// ListSegments 分群列表
func (r *CustomerSessionRepository) ListSegments(ctx context.Context, limit int) ([]*model.CustomerSegment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var list []*model.CustomerSegment
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// CountSegmentMembers 规则规模估算（rules JSON 里编译的 SQL 条件安全执行）
func (r *CustomerSessionRepository) CountSegmentMembers(ctx context.Context, whereSQL string) (int64, error) {
	if strings.TrimSpace(whereSQL) == "" {
		return 0, fmt.Errorf("空条件")
	}

	low := strings.ToLower(whereSQL)
	for _, bad := range []string{";", "drop", "delete", "update ", "insert", "alter", "truncate", "pg_", "--"} {
		if strings.Contains(low, bad) {
			return 0, fmt.Errorf("条件包含非法片段")
		}
	}
	var n int64
	err := r.db.WithContext(ctx).Table("customers").
		Where("deleted_at IS NULL AND (" + whereSQL + ")").
		Count(&n).Error
	return n, err
}

// ErrOptimisticLock 乐观锁冲突错误
var ErrOptimisticLock = fmt.Errorf("optimistic lock conflict: session version mismatch")

// OptimisticUpdate 使用乐观锁更新会话。
// 仅当数据库中当前 version == session.Version 时才会执行更新，
// 成功后 version 自增 1。版本不匹配时返回 ErrOptimisticLock。
func (r *CustomerSessionRepository) OptimisticUpdate(ctx context.Context, session *model.CustomerSession) error {
	oldVersion := session.Version
	res := r.db.WithContext(ctx).
		Model(&model.CustomerSession{}).
		Where("id = ? AND version = ?", session.ID, oldVersion).
		Updates(map[string]any{
			"status":            session.Status,
			"handler_type":      session.HandlerType,
			"agent_id":          session.AgentID,
			"agent_name":        session.AgentName,
			"priority":          session.Priority,
			"last_message":      session.LastMessage,
			"last_message_at":   session.LastMessageAt,
			"message_count":     session.MessageCount,
			"ai_reply_count":    session.AIReplyCount,
			"human_reply_count": session.HumanReplyCount,
			"rating":            session.Rating,
			"rating_comment":    session.RatingComment,
			"tags":              session.Tags,
			"resolved_at":       session.ResolvedAt,
			"closed_at":         session.ClosedAt,
			"version":           oldVersion + 1,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrOptimisticLock
	}
	session.Version = oldVersion + 1
	return nil
}

// CountPendingUnassigned 统计等待处理且未分配坐席的会话数
func (r *CustomerSessionRepository) CountPendingUnassigned(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("status IN ? AND agent_id = 0", []string{"pending", "waiting"}).
		Count(&count).Error
	return count, err
}

// CountSessionsByStatusSince 统计 since 之后指定状态集合的会话数（SLA 统计用）
func (r *CustomerSessionRepository) CountSessionsByStatusSince(ctx context.Context, since time.Time, statuses []string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("created_at >= ? AND status IN ?", since, statuses).
		Count(&n).Error
	return n, err
}

// CountSessionsSince 统计 since 之后全部会话数（SLA 统计用）
func (r *CustomerSessionRepository) CountSessionsSince(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("created_at >= ?", since).
		Count(&n).Error
	return n, err
}

// CountOpenSessionsSince24h 统计 24 小时内 open 状态会话数（SLA 巡检用）
func (r *CustomerSessionRepository) CountOpenSessionsSince24h(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("status = ? AND created_at > ?", "open", time.Now().Add(-24*time.Hour)).
		Count(&n).Error
	return n, err
}
