package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// inboxGroupConvPredicate T10 群聊判定 SQL（只读，不依赖新增列）。
//
// 两条证据任一命中即群聊：
//  1. 消息侧权威群标记（message_hub.is_group / group_id）——新 APK / 官方群渠道；
//  2. 同一 (tenant, platform, account_id, conversation_id) 下存在 >=2 个不同 customer_id
//     ——旧 APK 把群消息按发言人拆成多个会话，conversation_id 即群名（线上实测 20 组）。
//
// 租户列显式对齐，避免跨租户误归并；conversation_id 为空时第 2 条恒不成立。
const inboxGroupConvPredicate = `(
	EXISTS (
		SELECT 1 FROM message_hub mh
		WHERE mh.conversation_id = inbox_conversations.conversation_id
		  AND mh.platform = inbox_conversations.platform
		  AND mh.account_id = inbox_conversations.account_id
		  AND mh.tenant_id = inbox_conversations.tenant_id
		  AND (mh.is_group = true OR COALESCE(mh.group_id, '') <> '')
	)
	OR (
		SELECT COUNT(DISTINCT c2.customer_id) FROM inbox_conversations c2
		WHERE c2.platform = inbox_conversations.platform
		  AND c2.account_id = inbox_conversations.account_id
		  AND c2.tenant_id = inbox_conversations.tenant_id
		  AND c2.conversation_id = inbox_conversations.conversation_id
		  AND c2.conversation_id <> ''
	) >= 2
)`

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
	// T10 用户昵称：搜索 CRM/线索绑定的具体人（姓名或外部 ID），不是消息正文。
	if q.Nickname != "" {
		tx = tx.Where("(customer_name LIKE ? OR customer_id LIKE ?)", "%"+q.Nickname+"%", "%"+q.Nickname+"%")
	}
	// T10 会话类型：群聊谓词放到 SQL 里，保证 count 与分页口径一致。
	switch q.ConversationType {
	case "group":
		tx = tx.Where(inboxGroupConvPredicate)
	case "single":
		tx = tx.Where("NOT " + inboxGroupConvPredicate)
	}
	// 「待首响」筛选（specs/024 T604）：客户在等、我们还没回。
	// snoozed 排除在外——那是「我稍后再看」，不是「客户在等」；把它算进 SLA 会让数字虚高。
	if q.Filter == "awaiting_first_reply" {
		tx = tx.Where("waiting_since IS NOT NULL AND first_reply_at IS NULL AND closed_at IS NULL").
			Where("status <> ?", model.InboxStatusSnoozed)
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
	// 待首响默认按等待时长升序：等最久的最该先被看到。
	// 只有调用方没显式指定排序时才生效，避免覆盖前端用户的排序选择。
	if q.Filter == "awaiting_first_reply" && q.OrderBy == "" {
		orderBy = "waiting_since ASC"
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
	// 原为单条 CTE+DISTINCT ON+UPDATE FROM（GORM-only 清偿，specs/001-saas-multi-tenant T012）：
	// 拆为事务内「取每会话最新方向 → 按方向分组 gorm.Expr 批量更新」，幂等修复语义不变。
	db := r.db.WithContext(ctx)
	type latestRow struct {
		ConversationID string
		Direction      string
	}
	var latest []latestRow
	if err := db.Table("message_hub").
		Select("DISTINCT ON (conversation_id) conversation_id, direction").
		Order("conversation_id, sent_at DESC").
		Scan(&latest).Error; err != nil {
		return 0, err
	}
	inboundIDs := make([]string, 0, len(latest))
	outboundIDs := make([]string, 0, len(latest))
	for _, row := range latest {
		if row.Direction == "inbound" {
			inboundIDs = append(inboundIDs, row.ConversationID)
		} else {
			outboundIDs = append(outboundIDs, row.ConversationID)
		}
	}

	var total int64
	err := db.Transaction(func(tx *gorm.DB) error {
		total = 0
		if len(inboundIDs) > 0 {
			res := tx.Model(&model.InboxConversation{}).
				Where("conversation_id IN ?", inboundIDs).
				Updates(map[string]any{
					"unread_count": gorm.Expr("GREATEST(COALESCE(unread_count, 0), 1)"),
					"status":       gorm.Expr("CASE WHEN status IN ('assigned', 'closed') THEN status ELSE 'unread' END"),
				})
			if res.Error != nil {
				return res.Error
			}
			total += res.RowsAffected
		}
		if len(outboundIDs) > 0 {
			res := tx.Model(&model.InboxConversation{}).
				Where("conversation_id IN ?", outboundIDs).
				Updates(map[string]any{
					"unread_count": 0,
					"status":       gorm.Expr("CASE WHEN status IN ('unread', 'active') THEN 'open' ELSE status END"),
				})
			if res.Error != nil {
				return res.Error
			}
			total += res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// NormalizeStatuses 将遗留的非标准状态值归一为四值字典：
//   - active（webhook_inbox 旧写入值）→ unread（未读计数>0）或 open（无未读）
//   - 其它未知值同样按有无未读落到 unread / open
//
// 返回归一化行数。幂等，可在每次对账时调用。
func (r *InboxConversationRepository) NormalizeStatuses(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("status NOT IN ?", []string{"unread", "open", "assigned", "closed"}).
		Updates(map[string]any{
			"status": gorm.Expr("CASE WHEN unread_count > 0 THEN 'unread' ELSE 'open' END"),
		})
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
	return r.getStats(ctx, validStatuses, activeStatuses, from, threshold, "")
}

// GetStatsByAssignedTo 按坐席过滤的收件箱统计（数据权限：非超管只见分配给自己的）
//
// assignedTo 为空时退化为全量统计。其余语义与 GetStats 一致。
func (r *InboxConversationRepository) GetStatsByAssignedTo(ctx context.Context, validStatuses, activeStatuses []string, from string, threshold time.Time, assignedTo string) (*InboxStatsResult, error) {
	return r.getStats(ctx, validStatuses, activeStatuses, from, threshold, assignedTo)
}

func (r *InboxConversationRepository) getStats(ctx context.Context, validStatuses, activeStatuses []string, from string, threshold time.Time, assignedTo string) (*InboxStatsResult, error) {
	if r == nil || r.db == nil {
		return &InboxStatsResult{
			ByPlatform: map[string]int64{}, ByAssignedTo: map[string]int64{},
		}, nil
	}
	stats := &InboxStatsResult{
		ByPlatform:   map[string]int64{},
		ByAssignedTo: map[string]int64{},
	}
	scope := func(tx *gorm.DB) *gorm.DB {
		if assignedTo != "" {
			return tx.Where("assigned_to = ?", assignedTo)
		}
		return tx
	}

	type sc struct {
		Status string
		C      int64
	}
	var scs []sc
	if err := scope(r.db.WithContext(ctx).Model(&model.InboxConversation{})).
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
	scope(r.db.WithContext(ctx).Model(&model.InboxConversation{})).
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
	scope(r.db.WithContext(ctx).Model(&model.InboxConversation{})).
		Select("assigned_to, COUNT(*) AS c").
		Where("assigned_to <> '' AND status IN ?", activeStatuses).
		Group("assigned_to").Scan(&acs)
	for _, a := range acs {
		stats.ByAssignedTo[a.AssignedTo] = a.C
	}

	var overdue int64
	scope(r.db.WithContext(ctx).Model(&model.InboxConversation{})).
		Where("status IN ? AND last_message_from = ? AND last_message_at <= ?",
			[]string{"unread", "open", "assigned"}, from, threshold).
		Count(&overdue)
	stats.OverdueCount = overdue

	return stats, nil
}
