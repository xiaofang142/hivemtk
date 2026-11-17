package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ChannelOverviewRepository 渠道概览数据访问
//
// 聚合 13 渠道账号表的计数与客户渠道绑定读写。
// 计数均为 best-effort 场景（调用方对失败返回 0），错误原样上抛由 service 决策。
type ChannelOverviewRepository struct {
	db *gorm.DB
}

func NewChannelOverviewRepository(db *gorm.DB) *ChannelOverviewRepository {
	return &ChannelOverviewRepository{db: db}
}

func (r *ChannelOverviewRepository) countRows(ctx context.Context, table string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Table(table).Count(&n).Error
	return n, err
}

func (r *ChannelOverviewRepository) countWhere(ctx context.Context, table, query string, args ...any) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Table(table).Where(query, args...).Count(&n).Error
	return n, err
}

// CountTelegram 统计 Telegram 账号总数
func (r *ChannelOverviewRepository) CountTelegram(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "telegram_accounts")
}

// CountTelegramActive 统计启用中的 Telegram 账号数
func (r *ChannelOverviewRepository) CountTelegramActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "telegram_accounts", "status = ?", "active")
}

// CountWhatsApp 统计 WhatsApp 账号总数
func (r *ChannelOverviewRepository) CountWhatsApp(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "whatsapp_accounts")
}

// CountWhatsAppActive 统计启用中的 WhatsApp 账号数
func (r *ChannelOverviewRepository) CountWhatsAppActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "whatsapp_accounts", "status = ?", "active")
}

// CountFeishu 统计飞书账号总数
func (r *ChannelOverviewRepository) CountFeishu(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "feishu_accounts")
}

// CountFeishuActive 统计启用中的飞书账号数
func (r *ChannelOverviewRepository) CountFeishuActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "feishu_accounts", "status = ?", 1)
}

// CountWeCom 统计企微账号总数
func (r *ChannelOverviewRepository) CountWeCom(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "wecom_accounts")
}

// CountWeComOnline 统计在线企微账号数
func (r *ChannelOverviewRepository) CountWeComOnline(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "wecom_accounts", "login_state = ?", "online")
}

// CountDingTalk 统计钉钉账号总数
func (r *ChannelOverviewRepository) CountDingTalk(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "dingtalk_app_accounts")
}

// CountDingTalkActive 统计启用中的钉钉账号数
func (r *ChannelOverviewRepository) CountDingTalkActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "dingtalk_app_accounts", "status = ?", "active")
}

// CountQQ 统计 QQ 机器人账号总数
func (r *ChannelOverviewRepository) CountQQ(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "qq_accounts")
}

// CountQQActive 统计启用中的 QQ 机器人账号数
func (r *ChannelOverviewRepository) CountQQActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "qq_accounts", "status = ?", 1)
}

// CountSMS 统计短信配置总数
func (r *ChannelOverviewRepository) CountSMS(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "sms_configs")
}

// CountSMSActive 统计已配置 provider 的短信配置数
func (r *ChannelOverviewRepository) CountSMSActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "sms_configs", "provider IS NOT NULL AND provider != ''")
}

// CountEmail 统计邮件账号总数
func (r *ChannelOverviewRepository) CountEmail(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "email_accounts")
}

// CountEmailActive 统计启用中的邮件账号数
func (r *ChannelOverviewRepository) CountEmailActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "email_accounts", "status = ?", "active")
}

// CountBridge 统计指定 Bridge 渠道账号数
func (r *ChannelOverviewRepository) CountBridge(ctx context.Context, channel string) (int64, error) {
	return r.countWhere(ctx, "bridge_accounts", "channel = ?", channel)
}

// CountBridgeOnline 统计指定 Bridge 渠道此刻可达的账号数。
//
// 判定串见 bridgeOnlineSQLPredicate（那里写明了为什么不能按 status='online' 数），
// 窗口来自 bridge/online_grace_window，由调用方读好传进来，仓库层不自己找配置。
func (r *ChannelOverviewRepository) CountBridgeOnline(ctx context.Context, channel string, graceSeconds int) (int64, error) {
	return r.countWhere(ctx, "bridge_accounts",
		"channel = ? AND "+bridgeOnlineSQLPredicate,
		channel, bridgeStatusOffline, graceSeconds)
}

// CountWechat 统计微信公众号账号总数
func (r *ChannelOverviewRepository) CountWechat(ctx context.Context) (int64, error) {
	return r.countRows(ctx, "wechat_accounts")
}

// CountWechatActive 统计启用中的微信公众号账号数
func (r *ChannelOverviewRepository) CountWechatActive(ctx context.Context) (int64, error) {
	return r.countWhere(ctx, "wechat_accounts", "status = ?", "active")
}

// GetCustomerUnifiedID 按 customer 主键查 unified_id；未命中返回 ""
func (r *ChannelOverviewRepository) GetCustomerUnifiedID(ctx context.Context, customerID string) (string, error) {
	var cust struct {
		UnifiedID string
	}
	err := r.db.WithContext(ctx).Table("customers").Select("unified_id").Where("id = ?", customerID).Scan(&cust).Error
	if err != nil {
		return "", err
	}
	return cust.UnifiedID, nil
}

// UpsertCustomerChannel 幂等写入客户渠道绑定（specs/024 T502 两段式 upsert）。
//
// 顺序固定：① 身份锚（含软删）→ ② 业务键 → ③ 插入 → ④ 并发冲突重查一次。
//
// 为什么不是单条 ON CONFLICT：
//   - 业务键和身份锚是两条**不同**的唯一键，一条 ON CONFLICT 只能指定一个目标；
//   - 身份锚命中时语义不是「更新」，而是「这个平台账号已经属于某个客户」——必须沿用旧 one_id，
//     新 one_id 只记账不覆盖（覆盖会把同一平台账号的历史消息挪到另一个客户名下）；
//   - 软删行要能复活：ON CONFLICT 看不见被 WHERE 谓词排除的软删行，会直接插入第二行。
func (r *ChannelOverviewRepository) UpsertCustomerChannel(ctx context.Context, cc *model.CustomerChannel) error {
	if cc == nil {
		return nil
	}
	// 租户键必须与写入一致：调用方常常不填（由路由/中间件把租户放在 ctx 里），
	// 用 0 去查身份锚在 on 模式下永远查不到 → 每次都走插入分支 → 撞唯一键。
	tid := cc.TenantID
	if tid == 0 {
		tid = tenant.WriteTenantID(ctx)
	}
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	cc.TenantID = tid
	cc.AccountID = strings.TrimSpace(cc.AccountID)
	cc.ChannelUserID = strings.TrimSpace(cc.ChannelUserID)

	// ① 身份锚：unscoped 才能看见软删行（复活分支靠它）。
	if cc.ChannelUserID != "" {
		var prev model.CustomerChannel
		err := r.db.WithContext(ctx).Unscoped().
			Where("tenant_id = ? AND channel = ? AND account_id = ? AND channel_user_id = ?",
				cc.TenantID, cc.Channel, cc.AccountID, cc.ChannelUserID).
			Order("deleted_at IS NULL DESC, last_seen_at DESC, id DESC").
			First(&prev).Error
		switch {
		case err == nil:
			return r.applyIdentityAnchor(ctx, &prev, cc)
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 继续走业务键
		default:
			return err
		}
	}

	// ② 业务键：同客户同渠道同账号只应有一行。
	var prev model.CustomerChannel
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND one_id = ? AND channel = ? AND account_id = ?",
			cc.TenantID, cc.OneID, cc.Channel, cc.AccountID).
		First(&prev).Error
	switch {
	case err == nil:
		return r.updateCustomerChannel(ctx, prev.ID, cc, false)
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 继续插入
	default:
		return err
	}

	// ③ 插入；④ 并发首绑撞唯一键 → 重查一次后按命中分支收敛。
	if err := r.db.WithContext(ctx).Create(cc).Error; err != nil {
		if !IsDuplicateKeyErr(err) {
			return err
		}
		return r.upsertAfterConflict(ctx, cc)
	}
	return nil
}

// upsertAfterConflict 并发插入冲突后的收敛：重查身份锚 → 业务键，命中即更新。
func (r *ChannelOverviewRepository) upsertAfterConflict(ctx context.Context, cc *model.CustomerChannel) error {
	if cc.ChannelUserID != "" {
		var prev model.CustomerChannel
		err := r.db.WithContext(ctx).Unscoped().
			Where("tenant_id = ? AND channel = ? AND account_id = ? AND channel_user_id = ?",
				cc.TenantID, cc.Channel, cc.AccountID, cc.ChannelUserID).
			Order("deleted_at IS NULL DESC, last_seen_at DESC, id DESC").
			First(&prev).Error
		if err == nil {
			return r.applyIdentityAnchor(ctx, &prev, cc)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	var prev model.CustomerChannel
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND one_id = ? AND channel = ? AND account_id = ?",
			cc.TenantID, cc.OneID, cc.Channel, cc.AccountID).
		First(&prev).Error
	if err == nil {
		return r.updateCustomerChannel(ctx, prev.ID, cc, false)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 竞态窗口里对方又被删了：再插一次，失败就如实上报（不在仓库层无限重试）。
		return r.db.WithContext(ctx).Create(cc).Error
	}
	return err
}

// applyIdentityAnchor 身份锚命中：沿用旧 one_id，刷新绑定字段，必要时复活软删行。
func (r *ChannelOverviewRepository) applyIdentityAnchor(ctx context.Context, prev *model.CustomerChannel, cc *model.CustomerChannel) error {
	conflict := prev.OneID != "" && cc.OneID != "" && prev.OneID != cc.OneID
	incoming := cc.OneID
	if conflict {
		// 不覆盖：这个平台账号的历史消息都挂在旧 one_id 上，改绑会把两个客户的历史搅在一起。
		cc.OneID = prev.OneID
	}
	revive := prev.DeletedAt.Valid
	if err := r.updateCustomerChannel(ctx, prev.ID, cc, revive); err != nil {
		return err
	}
	if conflict {
		r.recordIdentityConflict(ctx, cc.TenantID, prev.OneID, incoming, cc.Channel, cc.AccountID, cc.ChannelUserID, prev.ID)
	}
	return nil
}

// updateCustomerChannel 按主键刷新绑定字段；revive=true 时把软删行复活。
//
// 为什么不用 Save：Save 会写 zero 值（is_primary=false 等）覆盖调用方没打算改的列，
// 而这里的入参是「本次入站看到的身份信息」，只应更新明确列出的字段。
func (r *ChannelOverviewRepository) updateCustomerChannel(ctx context.Context, id uint, cc *model.CustomerChannel, revive bool) error {
	updates := map[string]any{
		"one_id":          cc.OneID,
		"channel_name":    cc.ChannelName,
		"channel_user_id": cc.ChannelUserID,
		"account_id":      cc.AccountID,
		"is_primary":      cc.IsPrimary,
		"group_id":        cc.GroupID,
		"last_seen_at":    time.Now(),
		"updated_at":      time.Now(),
	}
	if revive {
		updates["deleted_at"] = nil
	}
	q := r.db.WithContext(ctx).Model(&model.CustomerChannel{}).Where("id = ?", id)
	if revive {
		q = q.Unscoped()
	}
	return q.Updates(updates).Error
}

// recordIdentityConflict 记一次「同一平台账号落到两个 OneID」的冲突（best-effort）。
//
// 这不是错误：入站身份识别本来就可能先猜错再纠正。但必须留证据——
// 否则客户问「为什么这个人的历史消息看不到了」时无从追起。
func (r *ChannelOverviewRepository) recordIdentityConflict(ctx context.Context, tenantID uint, keptOneID, incomingOneID, channel, accountID, channelUserID string, rowID uint) {
	log := &model.OperationLog{
		TenantID:   tenantID,
		Username:   "system",
		Action:     "customer_channel.identity_conflict",
		Module:     "channel_overview",
		Resource:   "customer_channel",
		ResourceID: fmt.Sprintf("%d", rowID),
		NewValue: fmt.Sprintf("kept one_id=%s, incoming one_id=%s, channel=%s, account_id=%s, channel_user_id=%s",
			keptOneID, incomingOneID, channel, accountID, channelUserID),
	}
	if err := r.db.WithContext(ctx).Create(log).Error; err != nil {
		// 审计失败不能影响绑定写入：调用方拿到的是「绑定成功、冲突已记录」或「绑定成功」。
		logger.Warnf("[CustomerChannel] 身份冲突记账失败 row=%d: %v", rowID, err)
	}
}

// UpdateCustomerChannelField 同步更新 customers 表的渠道主字段（best-effort 场景，错误上抛）
func (r *ChannelOverviewRepository) UpdateCustomerChannelField(ctx context.Context, oneID, field, value string) error {
	return r.db.WithContext(ctx).Table("customers").Where("unified_id = ?", oneID).Update(field, value).Error
}

// CountCustomersByID 统计指定主键的客户数（诊断用）
func (r *ChannelOverviewRepository) CountCustomersByID(ctx context.Context, customerID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", customerID).Count(&n).Error
	return n, err
}

// GetCustomerIdentity 读取客户基础身份四字段
func (r *ChannelOverviewRepository) GetCustomerIdentity(ctx context.Context, customerID string) (*model.Customer, error) {
	var cust model.Customer
	err := r.db.WithContext(ctx).Model(&model.Customer{}).
		Select("unified_id, phone, email, name").
		Where("id = ?", customerID).
		Scan(&cust).Error
	if err != nil {
		return nil, err
	}
	return &cust, nil
}

// ListCustomerChannelsByOneID 列出客户的全部渠道绑定（主绑定优先，偏好序次之）
func (r *ChannelOverviewRepository) ListCustomerChannelsByOneID(ctx context.Context, oneID string) ([]map[string]any, error) {
	var rows []map[string]any
	err := r.db.WithContext(ctx).Table("customer_channels").
		Where("one_id = ?", oneID).
		Order("is_primary DESC, preferred_rank ASC").
		Find(&rows).Error
	return rows, err
}
