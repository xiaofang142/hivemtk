// proactive_reach_repo.go 主动触达仓储（客户读取/偏好渠道/账号查找）（五层 L5）
package repository

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// ProactiveReachRepository 主动触达数据访问收口
type ProactiveReachRepository struct {
	db *gorm.DB
}

// NewProactiveReachRepository 构造
func NewProactiveReachRepository(db *gorm.DB) *ProactiveReachRepository {
	return &ProactiveReachRepository{db: db}
}

// GetCustomerByID 按 PK 读客户（未命中返回 nil）
func (r *ProactiveReachRepository) GetCustomerByID(ctx context.Context, id string) (*model.Customer, error) {
	if r.db == nil {
		return nil, nil
	}
	var c model.Customer
	if err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, nil
	}
	return &c, nil
}

// GetCustomerByUnifiedID 按 OneID 读客户（未命中返回 nil）
func (r *ProactiveReachRepository) GetCustomerByUnifiedID(ctx context.Context, oneID string) (*model.Customer, error) {
	if r.db == nil {
		return nil, nil
	}
	var c model.Customer
	if err := r.db.WithContext(ctx).First(&c, "unified_id = ?", oneID).Error; err != nil {
		return nil, nil
	}
	return &c, nil
}

// ListCustomerChannelsByOneID 读客户偏好渠道（主渠道优先、偏好序、最近活跃序）
func (r *ProactiveReachRepository) ListCustomerChannelsByOneID(ctx context.Context, oneID string) ([]model.CustomerChannel, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.CustomerChannel
	err := r.db.WithContext(ctx).
		Where("one_id = ?", oneID).
		Order("is_primary DESC, preferred_rank ASC, last_seen_at DESC").
		Find(&rows).Error
	return rows, err
}

// FindActiveAccountID 按渠道查找第一个可用账号，返回账号业务 ID
//
// 各渠道账号表结构不同，此处按渠道分派查询；无可用账号返回错误。
//
// bridgeGraceSeconds 只作用于五个桥接渠道（抖音/小红书/TikTok/快手/闲鱼）：
// 那些账号的可达性按"最后同步时间落在窗口内"判，与 bridge.isOnlineByLastSync、渠道总览计数同一条口径。
// 不能按 status='online' 选号——那一列只在 SSE 正常收尾时才改回离线，扩展崩溃/断网会把它粘在 online，
// 于是选中的是一台永远不会来取消息的账号：出站行安静地挂在 pending，客户收不到，日志里也没有失败。
// 无人在 grace 内 ⇒ 返回"查无账号"错误，由调用方报失败，而不是挑一台死的。
func (r *ProactiveReachRepository) FindActiveAccountID(ctx context.Context, channel string, bridgeGraceSeconds int) (string, error) {
	if r.db == nil {
		return "", errors.New("db not available")
	}
	switch channel {
	case "telegram":
		var acc struct {
			ID uint
		}
		if err := r.db.WithContext(ctx).Table("telegram_accounts").Where("status = ?", 1).Order("id ASC").First(&acc).Error; err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", acc.ID), nil
	case "whatsapp":
		var acc struct {
			ID string
		}
		if err := r.db.WithContext(ctx).Table("whatsapp_accounts").Where("status = ?", "active").Order("created_at ASC").First(&acc).Error; err != nil {
			return "", err
		}
		return acc.ID, nil
	case "feishu":
		var acc struct {
			ID uint
		}
		if err := r.db.WithContext(ctx).Table("feishu_accounts").Where("status = ?", 1).Order("id ASC").First(&acc).Error; err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", acc.ID), nil
	case "wecom":
		var acc struct {
			ID uint
		}
		if err := r.db.WithContext(ctx).Table("wecom_accounts").Where("login_state = ?", "online").Order("id ASC").First(&acc).Error; err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", acc.ID), nil
	case "douyin", "tiktok", "kuaishou", "xiaohongshu", "xianyu":

		var acc struct {
			AccountID string
		}
		if err := r.db.WithContext(ctx).Table("bridge_accounts").
			Where("channel = ? AND status <> ? AND last_sync_at IS NOT NULL"+
				" AND now() - last_sync_at < (? * interval '1 second')",
				channel, "offline", bridgeGraceSeconds).
			Order("last_sync_at DESC").First(&acc).Error; err != nil {
			return "", err
		}
		return acc.AccountID, nil
	case "wechat":

		var acc struct {
			ID uint
		}
		if err := r.db.WithContext(ctx).Table("wechat_accounts").
			Where("status = ?", "active").
			Where("app_id <> ? AND app_secret <> ?", "", "").
			Order("id ASC").First(&acc).Error; err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", acc.ID), nil
	}
	return "", fmt.Errorf("account lookup not supported for channel: %s", channel)
}
