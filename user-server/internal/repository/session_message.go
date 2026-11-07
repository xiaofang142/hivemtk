package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// clampSessionMessageLimit 夹取「取最近 N 条」类读口的 N。
//
// GORM 的 `Limit(0)` 等于**不加 limit**：任何一个调用方把配置项/请求参数里的 0
// 原样传下来，读的就是整张 session_messages（本表 2000+ 行且按会话单调增长）。
func clampSessionMessageLimit(limit int) int {
	if limit < 1 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}

// SessionMessageRepository 会话消息仓库
type SessionMessageRepository struct {
	db *gorm.DB
}

// NewSessionMessageRepository 创建会话消息仓库实例
func NewSessionMessageRepository() *SessionMessageRepository {
	return &SessionMessageRepository{
		db: _db.GetDB(),
	}
}

// NewSessionMessageRepositoryWithDB 创建指定数据库连接的 SessionMessageRepository 实例（用于测试）
func NewSessionMessageRepositoryWithDB(db *gorm.DB) *SessionMessageRepository {
	return &SessionMessageRepository{
		db: db,
	}
}

// GetDB 暴露底层连接（供跨库查询场景使用，如 trace_learning 洞察读取）
func (r *SessionMessageRepository) GetDB() *gorm.DB {
	return r.db
}

// errSessionMessageUnavailable 仓储句柄缺失（装配顺序错，不是「没数据」）。
var errSessionMessageUnavailable = errors.New("session message repository not initialized")

// Create 创建消息。
//
// nil 句柄守卫不是形式主义：`NewSessionMessageRepository()` 在构造时取
// `_db.GetDB()`，而 `init()` 时刻那个全局句柄必然还是 nil（见 tracker #44 的
// 系统性形状）。裸解引用会在「装配顺序错了」时 panic，而不是返回一个能读懂的错误。
func (r *SessionMessageRepository) Create(ctx context.Context, message *model.SessionMessage) error {
	if r == nil || r.db == nil {
		return errSessionMessageUnavailable
	}
	if message == nil {
		return errors.New("session message: nil 不是写入对象")
	}
	return r.db.WithContext(ctx).Create(message).Error
}

// FindRecentDuplicate 查找最近 N 秒内同 (session, content, sender_type, sender_id) 的消息
// 用于：visitor 端 + orchestrator 双保存的去重（修复 chat 双发 bug）
// 返回找到的最近一条消息（如果存在），否则返回 nil
func (r *SessionMessageRepository) FindRecentDuplicate(ctx context.Context, sessionID, senderType, senderID, content string, window time.Duration) (*model.SessionMessage, error) {
	var existing model.SessionMessage
	threshold := time.Now().Add(-window)
	if r == nil || r.db == nil {
		return nil, errSessionMessageUnavailable
	}
	err := r.db.WithContext(ctx).Where("session_id = ? AND sender_type = ? AND sender_id = ? AND content = ? AND created_at >= ?",
		sessionID, senderType, senderID, content, threshold).
		Order("id DESC").
		First(&existing).Error
	if err != nil {
		// 「没有重复」是正常结果，不是错误。三个调用方（chat_visitor / orchestrator
		// 两条保存路径）都写成 `existing, _ := ...; if existing != nil`：把 NotFound
		// 当错误返回，它们的 `_` 会把「查库失败」和「没有重复」混成同一件事 ——
		// 查库失败时同样插一条，去重形同不存在。这里把 NotFound 收成 (nil, nil)，
		// 让「有错误」只剩真正需要调用方处理的那一类。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &existing, nil
}

// GetBySessionID 获取会话的消息列表
func (r *SessionMessageRepository) GetBySessionID(ctx context.Context, sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error) {
	var messages []*model.SessionMessage
	var total int64

	if r == nil || r.db == nil {
		return nil, 0, errSessionMessageUnavailable
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	// Count 与 Find 各起一条 query。在同一条 `*gorm.DB` 上先 Count 再 Find 是
	// 靠 GORM 内部子句还原侥幸能跑 —— 链式调用会就地改 Statement（Count 把
	// Selects 换成 count(*)），下一次 Find 拿到的就是被改过的那份
	// （判例 #133 GetOrderList、#150 ListLoginEvents/ListSecurityAlerts）。
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.SessionMessage{}).Where("session_id = ?", sessionID)
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := base().Order("created_at ASC").Offset(offset).Limit(pageSize).Find(&messages).Error
	return messages, total, err
}

// ListBySessionIDsBatch 批量按多个 session_id 取消息，返回按 session_id 分组的 map（CC- N+1 优化）
//
// 单次 SQL 拉取所有 session 的消息，service 层按 session_id 分桶，
// 避免「for session → GetBySessionID」造成的 N+1 查询。
//   - sessionIDs: 待查询的 session id 列表，空时返回 nil
//   - perSessionLimit: 每个 session 最多取多少条（<=0 时默认 100）；为防止单 session 拉爆整体
//
// 返回值：map[sessionID][]*SessionMessage，key 一定是入参中的 sessionID 之一。
func (r *SessionMessageRepository) ListBySessionIDsBatch(ctx context.Context, sessionIDs []string, perSessionLimit int) (map[string][]*model.SessionMessage, error) {
	result := make(map[string][]*model.SessionMessage, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return result, nil
	}
	if perSessionLimit <= 0 {
		perSessionLimit = 100
	}
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
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
	var rows []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id IN ?", unique).
		Order("session_id ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		bucket := result[m.SessionID]
		if len(bucket) >= perSessionLimit {
			continue
		}
		result[m.SessionID] = append(bucket, m)
	}
	return result, nil
}

// MarkAsRead 标记消息已读
func (r *SessionMessageRepository) MarkAsRead(ctx context.Context, sessionID string, beforeTime time.Time) error {
	if r == nil || r.db == nil {
		return errSessionMessageUnavailable
	}
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id = ? AND is_read = ? AND created_at <= ?", sessionID, false, beforeTime).
		Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		}).Error
}

// HasTable 检查 session_messages 表是否存在（用于统一收件箱合并消息流时的兼容判定）
func (r *SessionMessageRepository) HasTable(ctx context.Context) bool {
	if r == nil || r.db == nil {
		return false
	}
	return r.db.WithContext(ctx).Migrator().HasTable(&model.SessionMessage{})
}

// ListAllBySessionID 按 session_id 拉取全部消息（不分页，用于统一收件箱会话视图合并展示）
func (r *SessionMessageRepository) ListAllBySessionID(ctx context.Context, sessionID string) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var rows []*model.SessionMessage
	if err := r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id = ?", sessionID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListOfflineBySessionID 拉取访客离线期间未投递的坐席/AI 回复消息
//
// 离线消息定义（与 service.GetOfflineMessages 一致）：
//   - sender_type IN ('ai', 'agent')
//   - delivered_at IS NULL
//   - 按 created_at ASC 排序（投递顺序与产生顺序一致）
func (r *SessionMessageRepository) ListOfflineBySessionID(ctx context.Context, sessionID string) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var messages []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Where("sender_type IN ?", []string{"ai", "agent"}).
		Where("delivered_at IS NULL").
		Order("created_at ASC").
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// MarkDelivered 批量标记消息已投递
//
// 行为与原 service.MarkMessagesDelivered 一致：
//   - 仅更新 session_id + id IN 范围内的消息
//   - delivered_at = now（由调用方传入，便于测试与统一时间基准）
//   - messageIDs 为空时无操作
func (r *SessionMessageRepository) MarkDelivered(ctx context.Context, sessionID string, messageIDs []uint, deliveredAt time.Time) error {
	if r == nil || r.db == nil || len(messageIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("session_id = ? AND id IN ?", sessionID, messageIDs).
		Update("delivered_at", &deliveredAt).Error
}

// UpdateCitations 补写答案引用快照（T2 引用溯源）：编排器先行落行的场景
// 由去重命中方补齐，仅覆盖空快照，避免覆盖真实历史。
func (r *SessionMessageRepository) UpdateCitations(ctx context.Context, messageID uint, citations string) error {
	if r == nil || r.db == nil || messageID == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("id = ?", messageID).
		Update("citations", citations).Error
}

// Delete 按 id 删除单条会话消息（统一收件箱消息删除）。
//
// 0 行 ⇒ ErrRecordNotFound：删一条已经被删掉的消息不该回成功，否则前端把
// 「别人已经删过了」读成「我删成功了」，下一次刷新才发现列表没变。
func (r *SessionMessageRepository) Delete(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errSessionMessageUnavailable
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.SessionMessage{}))
}

// DeleteInSession 带会话归属的删除：消息不属于该会话时命中 0 行 ⇒ ErrRecordNotFound。
// 供收件箱按「会话 + 消息」双键删除使用（消息 id 全局自增，只按 id 删等于允许
// 借一个可见会话删掉另一个会话的消息，2026-11-02 对抗性复核实测）。
func (r *SessionMessageRepository) DeleteInSession(ctx context.Context, sessionID string, id uint) error {
	if r == nil || r.db == nil {
		return errSessionMessageUnavailable
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).
		Where("id = ? AND session_id = ?", id, sessionID).
		Delete(&model.SessionMessage{}))
}

// ListInternalBySession 会话内部备注列表（仅 is_internal=true）
func (r *SessionMessageRepository) ListInternalBySession(ctx context.Context, sessionID string, limit int) ([]*model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, errSessionMessageUnavailable
	}
	var list []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND is_internal = ?", sessionID, true).
		Order("created_at DESC").
		Limit(clampSessionMessageLimit(limit)).
		Find(&list).Error
	return list, err
}

// ListRecentDescBySessionID 按 session_id 倒序取最近 N 条消息（AI agent loop 上下文用）
func (r *SessionMessageRepository) ListRecentDescBySessionID(ctx context.Context, sessionID string, limit int) ([]model.SessionMessage, error) {
	if r == nil || r.db == nil {
		return nil, errSessionMessageUnavailable
	}
	var hist []model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id desc").Limit(clampSessionMessageLimit(limit)).Find(&hist).Error
	return hist, err
}

// MarkMessageDelivered 标记消息已送达（访客在线推送成功后回写 delivered_at）。
//
// AV-04c 批次5 刀1 自 service/customer_session.go pushToVisitor 下沉：
// 原 service 经 sessionRepo.GetDB(ctx) 直拼 Model(&SessionMessage{}).Update，
// 谓词与写入字段（仅 delivered_at，白名单单字段）逐字保留；错误路径不变 ——
// 调用方（pushToVisitor）依旧丢弃错误（best-effort 记账）。
func (r *SessionMessageRepository) MarkMessageDelivered(ctx context.Context, messageID uint, deliveredAt time.Time) error {
	if r == nil || r.db == nil {
		return errSessionMessageUnavailable
	}
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("id = ?", messageID).Update("delivered_at", &deliveredAt).Error
}
