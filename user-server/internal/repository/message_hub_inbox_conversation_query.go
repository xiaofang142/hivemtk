package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
)

// ListByQuery 按条件分页查询会话（行为与原 service.List 一致）
func (r *InboxConversationRepository) ListByQuery(ctx context.Context, q InboxConversationQuery) ([]*model.InboxConversation, int64, error) {
	if r == nil || r.db == nil {
		return []*model.InboxConversation{}, 0, nil
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 200 {
		q.PageSize = 20
	}

	tx := r.db.WithContext(ctx).Model(&model.InboxConversation{})
	if q.Platform != "" {
		tx = tx.Where("platform = ?", q.Platform)
	}
	if q.AccountID != "" {
		tx = tx.Where("account_id = ?", q.AccountID)
	}
	if q.CustomerID != "" {
		tx = tx.Where("customer_id = ?", q.CustomerID)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.AssignedTo != "" {
		tx = tx.Where("assigned_to = ?", q.AssignedTo)
	}
	if q.AssignedSOP > 0 {
		tx = tx.Where("assigned_to_sop = ?", q.AssignedSOP)
	}
	if q.Pinned != nil {
		tx = tx.Where("pinned = ?", *q.Pinned)
	}
	if q.Starred != nil {
		tx = tx.Where("starred = ?", *q.Starred)
	}
	if q.Muted != nil {
		tx = tx.Where("muted = ?", *q.Muted)
	}
	if q.Keyword != "" {
		tx = tx.Where("last_message_preview LIKE ?", "%"+q.Keyword+"%")
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderBy := "pinned DESC, id DESC"
	switch q.OrderBy {
	case "pinned_first":
		orderBy = "pinned DESC, last_message_at DESC"
	case "id_desc":
		orderBy = "id DESC"
	case "unread_desc":
		orderBy = "unread_count DESC, last_message_at DESC"
	case "oldest_asc":
		orderBy = "last_message_at ASC"
	case "latest_desc":
		orderBy = "last_message_at DESC"
	}

	var list []*model.InboxConversation
	if err := tx.Order(orderBy).
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}

	r.attachLatestMessages(ctx, list)
	return list, total, nil
}

func (r *InboxConversationRepository) attachLatestMessages(ctx context.Context, list []*model.InboxConversation) {
	if len(list) == 0 {
		return
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		if c.ConversationID != "" {
			ids = append(ids, c.ConversationID)
		}
	}
	if len(ids) == 0 {
		return
	}
	type latestHub struct {
		ConversationID string
		ID             uint
		Content        string
		SentAt         *time.Time
		Direction      string
		IsAIReply      bool
	}
	var rows []latestHub
	if err := r.db.WithContext(ctx).
		Table("message_hub").
		Select("DISTINCT ON (conversation_id) conversation_id, id, content, sent_at, direction, is_ai_reply").
		Where("conversation_id IN ?", ids).
		Order("conversation_id, sent_at DESC").
		Find(&rows).Error; err != nil {
		return
	}
	byConv := make(map[string]*latestHub, len(rows))
	for i := range rows {
		byConv[rows[i].ConversationID] = &rows[i]
	}
	for _, c := range list {
		l, ok := byConv[c.ConversationID]
		if !ok {
			continue
		}
		c.LastMessageID = l.ID
		preview := l.Content
		if len(preview) > 200 {
			preview = preview[:200]
		}
		c.LastMessagePreview = preview
		c.LastMessageAt = l.SentAt
		switch {
		case l.Direction == "inbound":
			c.LastMessageFrom = "customer"
		case l.IsAIReply:
			c.LastMessageFrom = "ai"
		default:
			c.LastMessageFrom = "staff"
		}
	}
	r.attachSessionMessageFallback(ctx, list)
}

func (r *InboxConversationRepository) attachSessionMessageFallback(ctx context.Context, list []*model.InboxConversation) {
	missing := make([]*model.InboxConversation, 0, len(list))
	for _, c := range list {
		if c.ConversationID != "" && (c.LastMessagePreview == "") {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return
	}
	ids := make([]string, 0, len(missing))
	for _, c := range missing {
		ids = append(ids, c.ConversationID)
	}
	type latestSM struct {
		SessionID string
		Content   string
	}
	var rows []latestSM
	if err := r.db.WithContext(ctx).
		Table("session_messages").
		Select("DISTINCT ON (session_id) session_id, content").
		Where("session_id IN ?", ids).
		Order("session_id, id DESC").
		Find(&rows).Error; err != nil {
		return
	}
	bySession := make(map[string]string, len(rows))
	for i := range rows {
		content := rows[i].Content
		if len(content) > 200 {
			content = content[:200]
		}
		bySession[rows[i].SessionID] = content
	}
	for _, c := range missing {
		if preview, ok := bySession[c.ConversationID]; ok && preview != "" {
			c.LastMessagePreview = preview
		}
	}
}

// ReconcileUnread 以 message_hub 最后一条消息为准，批量重算全部会话的未读计数与状态。
//   - 最后一条是我方（AI/坐席）消息 → 未读清零；原“未读”态转“待处理/消息池”。
//   - 最后一条是客户消息 → 未读记为 1（至少有一条未读）；已分配/已关闭保持不变，其余转“未读”。
//   - 历史脏状态（非 unread/open/assigned/closed 四值字典，如 webhook 早期写入的 'active'）一并归一。
//
// 仅更新存在 message_hub 记录的会话；无消息记录的会话保持原状。
func (r *InboxConversationRepository) ReconcileUnread(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Exec(`
		WITH latest AS (
			SELECT DISTINCT ON (conversation_id) conversation_id, direction
			FROM message_hub
			ORDER BY conversation_id, sent_at DESC
		)
		UPDATE inbox_conversations ic
		SET unread_count = CASE
		        WHEN l.direction = 'inbound' THEN GREATEST(COALESCE(ic.unread_count, 0), 1)
		        ELSE 0
		    END,
		    status = CASE
		        WHEN l.direction = 'inbound'
		            THEN CASE WHEN ic.status IN ('assigned', 'closed') THEN ic.status ELSE 'unread' END
		        ELSE CASE WHEN ic.status IN ('unread', 'active') THEN 'open' ELSE ic.status END
		    END
		FROM latest l
		WHERE ic.conversation_id = l.conversation_id
	`)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// NormalizeStatuses 将历史遗留的非标准状态值归一为四值字典：
//   - active（webhook_inbox 旧写入值）→ unread（未读计数>0）或 open（无未读）
//   - 其它未知值同样按有无未读落到 unread / open
//
// 返回归一化行数。幂等，可在每次对账时调用。
func (r *InboxConversationRepository) NormalizeStatuses(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Exec(`
		UPDATE inbox_conversations
		SET status = CASE WHEN unread_count > 0 THEN 'unread' ELSE 'open' END
		WHERE status NOT IN ('unread', 'open', 'assigned', 'closed')
	`)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// FindOverdueConversations 查询“超时未响应”的会话：
// 最后一条是客户消息、且早于 threshold、且仍处于活跃态（未读/待处理/已分配）。
func (r *InboxConversationRepository) FindOverdueConversations(ctx context.Context, threshold time.Time, limit int) ([]*model.InboxConversation, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var list []*model.InboxConversation
	q := r.db.WithContext(ctx).
		Where("last_message_from = ?", "customer").
		Where("last_message_at IS NOT NULL AND last_message_at < ?", threshold).
		Where("status IN ?", []string{"unread", "open", "assigned"})
	q = applyListLimit(q, limit)
	if err := q.Order("last_message_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// InboxStatsResult 收件箱统计结果（与 service.InboxStats 字段对齐）
type InboxStatsResult struct {
	Total        int64
	Unread       int64
	Open         int64
	Assigned     int64
	Closed       int64
	ByPlatform   map[string]int64
	ByAssignedTo map[string]int64
	OverdueCount int64
}

// GetStats 收件箱多维度统计（行为与原 service.GetStats 一致）
//
// 参数：
//   - validStatuses: 参与状态分布统计的状态集合
//   - activeStatuses: 客服活跃会话状态集合（用于 ByAssignedTo）
//   - from: last_message_from 过滤值（通常 "customer"）
//   - threshold: 超时阈值（last_message_at <= threshold 视为 overdue）
func (r *InboxConversationRepository) GetStats(ctx context.Context, validStatuses, activeStatuses []string, from string, threshold time.Time) (*InboxStatsResult, error) {
	if r == nil || r.db == nil {
		return &InboxStatsResult{
			ByPlatform: map[string]int64{}, ByAssignedTo: map[string]int64{},
		}, nil
	}
	stats := &InboxStatsResult{
		ByPlatform:   map[string]int64{},
		ByAssignedTo: map[string]int64{},
	}

	type sc struct {
		Status string
		C      int64
	}
	var scs []sc
	if err := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Select("status, COUNT(*) AS c").
		Where("status IN ?", validStatuses).
		Group("status").Scan(&scs).Error; err != nil {
		return nil, err
	}
	for _, s := range scs {
		switch s.Status {
		case "unread":
			stats.Unread = s.C
		case "open":
			stats.Open = s.C
		case "assigned":
			stats.Assigned = s.C
		case "closed":
			stats.Closed = s.C
		}
		stats.Total += s.C
	}

	type pc struct {
		Platform string
		C        int64
	}
	var pcs []pc
	r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Select("platform AS platform, COUNT(*) AS c").
		Group("platform").Scan(&pcs)
	for _, p := range pcs {
		stats.ByPlatform[p.Platform] = p.C
	}

	type ac struct {
		AssignedTo string
		C          int64
	}
	var acs []ac
	r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Select("assigned_to, COUNT(*) AS c").
		Where("assigned_to <> '' AND status IN ?", activeStatuses).
		Group("assigned_to").Scan(&acs)
	for _, a := range acs {
		stats.ByAssignedTo[a.AssignedTo] = a.C
	}

	var overdue int64
	r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("status IN ? AND last_message_from = ? AND last_message_at <= ?",
			[]string{"unread", "open", "assigned"}, from, threshold).
		Count(&overdue)
	stats.OverdueCount = overdue

	return stats, nil
}
