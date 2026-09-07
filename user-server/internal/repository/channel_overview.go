package repository

import (
	"context"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// ChannelOverviewRepository 渠道概览数据访问
//
// 聚合 13 渠道账号表的计数与客户渠道绑定读写。
// 计数均为 best-effort 场景（调用方对失败返回 0），错误原样上抛由 service 决策。
type ChannelOverviewRepository struct {
	db *gorm.DB
}

// NewChannelOverviewRepository 创建渠道概览仓储
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

// CountBridgeOnline 统计指定 Bridge 渠道在线账号数
func (r *ChannelOverviewRepository) CountBridgeOnline(ctx context.Context, channel string) (int64, error) {
	return r.countWhere(ctx, "bridge_accounts", "channel = ? AND status = ?", channel, "online")
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

// UpsertCustomerChannel 幂等写入客户渠道绑定（one_id+channel 唯一）
func (r *ChannelOverviewRepository) UpsertCustomerChannel(ctx context.Context, cc *model.CustomerChannel, assignVals map[string]any) error {
	return r.db.WithContext(ctx).
		Where("one_id = ? AND channel = ?", cc.OneID, cc.Channel).
		Assign(assignVals).
		FirstOrCreate(cc).Error
}

// UpdateCustomerChannelField 同步更新 customers 表的渠道主字段（best-effort 场景，错误上抛）
func (r *ChannelOverviewRepository) UpdateCustomerChannelField(ctx context.Context, oneID, field, value string) error {
	return r.db.WithContext(ctx).Table("customers").Where("unified_id = ?", oneID).Update(field, value).Error
}

// CountCustomersByID 统计指定主键的客户数（诊断用）
func (r *ChannelOverviewRepository) CountCustomersByID(ctx context.Context, customerID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM customers WHERE id = ?`, customerID).Scan(&n).Error
	return n, err
}

// GetCustomerIdentity 读取客户基础身份四字段
func (r *ChannelOverviewRepository) GetCustomerIdentity(ctx context.Context, customerID string) (*model.Customer, error) {
	var cust model.Customer
	row := r.db.WithContext(ctx).Raw(`SELECT unified_id, phone, email, name FROM customers WHERE id = ?`, customerID).Row()
	if err := row.Scan(&cust.UnifiedID, &cust.Phone, &cust.Email, &cust.Name); err != nil {
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
