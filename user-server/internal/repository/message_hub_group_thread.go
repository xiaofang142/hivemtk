package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"hivemtk-user/internal/model"
)

// T10 统一收件箱群聊展示的只读查询。
//
// 硬纪律：这里只做投影与筛选，不写库、不回填历史行——线上 215 条会话中 20 组
// 旧 APK 群聊数据保持原样，群聊归并完全在读路径完成。

// InboxGroupMember 群内发言人（去重后给前端做「高亮成员」下拉）。
type InboxGroupMember struct {
	SenderID     string `json:"sender_id"`
	SenderName   string `json:"sender_name"`
	SenderAvatar string `json:"sender_avatar"`
	MessageCount int    `json:"message_count"`
}

// ResolveGroupConversationIDs 按 (tenant, platform, account, 群名) 找出参与该群的全部会话键。
//
// 两条互补来源：
//   - inbox_conversations.conversation_id == groupName：旧 APK 把群名当会话键；
//   - message_hub.group_id == groupName：新协议群帧把 group_id 当会话键。
//
// 两者都显式带 tenant/platform/account，绝不跨账号/渠道归并。
func (r *MessageHubRepository) ResolveGroupConversationIDs(ctx context.Context, tenantID uint, platform, accountID, groupName string) ([]string, error) {
	groupName = strings.TrimSpace(groupName)
	if r == nil || r.db == nil || platform == "" || accountID == "" || groupName == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var convIDs []string
	var rows []struct{ ConversationID string }
	if err := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Select("DISTINCT conversation_id").
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND conversation_id = ?", tenantID, platform, accountID, groupName).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ConversationID != "" && !seen[row.ConversationID] {
			seen[row.ConversationID] = true
			convIDs = append(convIDs, row.ConversationID)
		}
	}
	rows = nil
	if err := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Select("DISTINCT conversation_id").
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND group_id = ?", tenantID, platform, accountID, groupName).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ConversationID != "" && !seen[row.ConversationID] {
			seen[row.ConversationID] = true
			convIDs = append(convIDs, row.ConversationID)
		}
	}
	sort.Strings(convIDs)
	return convIDs, nil
}

// HasVisibleConversationInGroup 非超管的数据权限守卫：该群下是否至少有一个会话分配给该坐席。
// 与 ResolveGroupConversationIDs 同一作用域口径，缺分配即拒绝（fail-closed）。
func (r *InboxConversationRepository) HasVisibleConversationInGroup(ctx context.Context, tenantID uint, platform, accountID, groupName, username string) (bool, error) {
	if r == nil || r.db == nil || platform == "" || accountID == "" || groupName == "" || username == "" {
		return false, nil
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND conversation_id = ? AND assigned_to = ?", tenantID, platform, accountID, groupName, username).
		Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	n = 0
	err := r.db.WithContext(ctx).Table("message_hub AS mh").
		Joins("JOIN inbox_conversations AS c ON c.conversation_id = mh.conversation_id AND c.platform = mh.platform AND c.account_id = mh.account_id AND c.tenant_id = mh.tenant_id").
		Where("mh.tenant_id = ? AND mh.platform = ? AND mh.account_id = ? AND mh.group_id = ? AND c.assigned_to = ?", tenantID, platform, accountID, groupName, username).
		Count(&n).Error
	return n > 0, err
}

// ListByConversationIDs 跨同一群的多个会话键拉消息，按时间倒序分页（一次查询，避免 46 人群 46 次请求）。
func (r *MessageHubRepository) ListByConversationIDs(ctx context.Context, tenantID uint, platform, accountID string, convIDs []string, page, pageSize int) ([]*model.MessageHub, int64, error) {
	if r == nil || r.db == nil || len(convIDs) == 0 {
		return []*model.MessageHub{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND conversation_id IN ?", tenantID, platform, accountID, convIDs)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.MessageHub
	if err := q.Order("sent_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListGroupMembers 去重列出群内发言人（按 sender_id），供「高亮成员」下拉。
func (r *MessageHubRepository) ListGroupMembers(ctx context.Context, tenantID uint, platform, accountID string, convIDs []string) ([]InboxGroupMember, error) {
	if r == nil || r.db == nil || len(convIDs) == 0 {
		return []InboxGroupMember{}, nil
	}
	type row struct {
		SenderID     string
		SenderName   string
		SenderAvatar string
		C            int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Select("sender_id, MAX(sender_name) AS sender_name, MAX(sender_avatar) AS sender_avatar, COUNT(*) AS c").
		Where("tenant_id = ? AND platform = ? AND account_id = ? AND conversation_id IN ? AND sender_id <> ''", tenantID, platform, accountID, convIDs).
		Group("sender_id").Order("c DESC, sender_id ASC").Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]InboxGroupMember, 0, len(rows))
	for _, x := range rows {
		out = append(out, InboxGroupMember{
			SenderID: x.SenderID, SenderName: x.SenderName,
			SenderAvatar: x.SenderAvatar, MessageCount: int(x.C),
		})
	}
	return out, nil
}

// AttachT10ConversationMeta 给会话列表页附加 T10 展示字段（只读投影）。
//
// 群聊判定与 SQL 谓词 inboxGroupConvPredicate 同口径：消息侧标记 OR 共享 conversation_id。
func (r *InboxConversationRepository) AttachT10ConversationMeta(ctx context.Context, list []*model.InboxConversation) error {
	if r == nil || r.db == nil || len(list) == 0 {
		return nil
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		if c != nil && c.ConversationID != "" {
			ids = append(ids, c.ConversationID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	// 1) 共享会话键 → 成员数（旧 APK 群聊的真实形态）。
	type sharedRow struct {
		TenantID       uint
		Platform       string
		AccountID      string
		ConversationID string
		CustomerID     string
	}
	var shared []sharedRow
	if err := r.db.WithContext(ctx).Model(&model.InboxConversation{}).
		Select("tenant_id, platform, account_id, conversation_id, customer_id").
		Where("conversation_id IN ? AND conversation_id <> ''", ids).
		Scan(&shared).Error; err != nil {
		return err
	}
	type convScope struct {
		tenant            uint
		platform, account string
	}
	customerSet := map[string]map[string]bool{} // key: tenant|platform|account|convID
	for _, row := range shared {
		k := convScopeKey(row.TenantID, row.Platform, row.AccountID, row.ConversationID)
		if customerSet[k] == nil {
			customerSet[k] = map[string]bool{}
		}
		if row.CustomerID != "" {
			customerSet[k][row.CustomerID] = true
		}
	}

	// 2) 消息侧群标记 / 群名 / 群 ID（新 APK / 官方群渠道）。
	type hubRow struct {
		TenantID       uint
		Platform       string
		AccountID      string
		ConversationID string
		SenderID       string
		IsGroup        bool
		GroupID        string
		Extra          string
	}
	var hubs []hubRow
	if err := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Select("tenant_id, platform, account_id, conversation_id, sender_id, is_group, COALESCE(group_id, '') AS group_id, COALESCE(extra, '') AS extra").
		Where("conversation_id IN ?", ids).
		Scan(&hubs).Error; err != nil {
		return err
	}
	type hubMeta struct {
		isGroup   bool
		groupID   string
		groupName string
		senders   map[string]bool
	}
	hubByConv := map[string]*hubMeta{}
	for i := range hubs {
		h := &hubs[i]
		k := convScopeKey(h.TenantID, h.Platform, h.AccountID, h.ConversationID)
		m := hubByConv[k]
		if m == nil {
			m = &hubMeta{senders: map[string]bool{}}
			hubByConv[k] = m
		}
		if m.senders == nil {
			m.senders = map[string]bool{}
		}
		if h.SenderID != "" {
			m.senders[h.SenderID] = true
		}
		if h.IsGroup {
			m.isGroup = true
		}
		if strings.TrimSpace(h.GroupID) != "" {
			m.groupID = strings.TrimSpace(h.GroupID)
		}
		var extra map[string]any
		if h.Extra != "" && json.Unmarshal([]byte(h.Extra), &extra) == nil {
			if name, ok := extra["group_name"].(string); ok && strings.TrimSpace(name) != "" {
				m.groupName = strings.TrimSpace(name)
			}
		}
	}

	// 3) 投影到每个会话；group_key 用群名（新 APK 优先 extra.group_name），
	//    旧 APK 即 conversation_id。空名 fail-closed 落单聊。
	for _, c := range list {
		if c == nil {
			continue
		}
		c.ConversationType = "single"
		if c.ConversationID == "" {
			continue
		}
		k := convScopeKey(c.TenantID, c.Platform, c.AccountID, c.ConversationID)
		hm := hubByConv[k]
		isGroup := len(customerSet[k]) >= 2
		groupName, groupID := "", ""
		if hm != nil {
			isGroup = isGroup || hm.isGroup || hm.groupID != "" || hm.groupName != ""
			groupName, groupID = hm.groupName, hm.groupID
		}
		if !isGroup {
			continue
		}
		name := t10FirstNonEmpty(groupName, groupID, strings.TrimSpace(c.ConversationID))
		if name == "" {
			continue
		}
		c.IsGroup = true
		c.ConversationType = "group"
		c.GroupName = name
		c.GroupID = groupID
		c.GroupKey = c.Platform + "|" + c.AccountID + "|" + name
		c.MemberCount = len(customerSet[k])
		if hm != nil && len(hm.senders) > c.MemberCount {
			c.MemberCount = len(hm.senders)
		}
		c.ConversationIDs = []string{c.ConversationID}
	}
	return nil
}

func convScopeKey(tenant uint, platform, accountID, convID string) string {
	return strings.Join([]string{
		itoaUint(tenant), platform, accountID, convID,
	}, "|")
}

func itoaUint(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func t10FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
