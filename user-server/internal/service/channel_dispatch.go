package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync/atomic"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ChannelDispatchService 账号自动均衡收发（路由2）。
// 策略：会话粘性优先（已有会话绑定账号）-> 启用账号 round-robin。
// 五层纪律：数据访问全部经 repository，本层不持有 *gorm.DB。
type ChannelDispatchService struct {
	channelRepo repository.ChannelRepository
	convRepo    ChannelDispatchConversationReader
	accRepo     repository.ChannelAccountRepository
	rr          atomic.Uint64
}

// ChannelDispatchConversationReader 是渠道分发只需要的最小会话查询端口。
type ChannelDispatchConversationReader interface {
	FindLatestByPlatformAndPeer(
		ctx context.Context,
		tenantID uint,
		platform string,
		byConversationID string,
		peer string,
	) (*model.InboxConversation, error)
}

// NewChannelDispatchService 使用全局 DB 对应的默认仓储构造。
func NewChannelDispatchService() *ChannelDispatchService {
	return NewChannelDispatchServiceWithRepos(
		repository.NewChannelRepository(),
		repository.NewInboxConversationRepository(),
		repository.NewChannelAccountRepository(),
	)
}

// NewChannelDispatchServiceWithRepos 用显式仓储构造（装配点/测试用）。
func NewChannelDispatchServiceWithRepos(
	channelRepo repository.ChannelRepository,
	convRepo ChannelDispatchConversationReader,
	accRepo repository.ChannelAccountRepository,
) *ChannelDispatchService {
	return &ChannelDispatchService{
		channelRepo: channelRepo,
		convRepo:    convRepo,
		accRepo:     accRepo,
	}
}

type DispatchPickInput struct {
	TenantID           uint
	Channel            string // platform code, e.g. wechat
	ChannelID          uint
	ConversationID     string
	CustomerID         string
	PreferredAccountID uint
}

type DispatchPickResult struct {
	AccountID    uint   `json:"account_id"`
	AccountName  string `json:"account_name"`
	IdentifyCode string `json:"identify_code"`
	Reason       string `json:"reason"`
}

// PickAccount 为发送选择账号。
func (s *ChannelDispatchService) PickAccount(ctx context.Context, in DispatchPickInput) (*DispatchPickResult, error) {
	if in.TenantID == 0 {
		if tid, ok := tenant.FromContext(ctx); ok {
			in.TenantID = tid
		}
	}
	if in.TenantID == 0 {
		return nil, fmt.Errorf("tenant required")
	}
	if in.ChannelID == 0 && in.Channel != "" {
		ch, err := s.channelRepo.GetByTenantAndCode(ctx, in.TenantID, in.Channel)
		if err != nil {
			return nil, fmt.Errorf("resolve dispatch channel: %w", err)
		}
		if ch != nil {
			in.ChannelID = ch.ID
		}
	}
	if in.ChannelID == 0 {
		return nil, fmt.Errorf("channel required")
	}

	// 1) 会话粘性：已有会话绑定账号
	if in.ConversationID != "" || in.CustomerID != "" {
		peer := in.CustomerID
		if in.ConversationID != "" {
			peer = in.ConversationID
		}
		conv, err := s.convRepo.FindLatestByPlatformAndPeer(ctx, in.TenantID, in.Channel, in.ConversationID, peer)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("query sticky conversation: %w", err)
		}
		if err == nil && conv != nil && conv.AccountID != "" {
			// account_id in inbox conversation is string
			var aid uint
			_, _ = fmt.Sscanf(conv.AccountID, "%d", &aid)
			if aid > 0 {
				acc, err := s.accRepo.GetByID(ctx, in.TenantID, aid)
				if err != nil {
					return nil, fmt.Errorf("query sticky account: %w", err)
				}
				if acc != nil && acc.Status == 1 {
					return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "sticky"}, nil
				}
			}
		}
	}

	// 2) preferred
	if in.PreferredAccountID > 0 {
		acc, err := s.accRepo.GetByID(ctx, in.TenantID, in.PreferredAccountID)
		if err != nil {
			return nil, fmt.Errorf("query preferred account: %w", err)
		}
		if acc != nil && acc.Status == 1 && acc.ChannelID == in.ChannelID {
			return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "preferred"}, nil
		}
	}

	// 3) round-robin among active accounts
	status := 1
	accs, err := s.accRepo.List(ctx, repository.ChannelAccountListOptions{TenantID: in.TenantID, ChannelID: in.ChannelID, Status: &status, Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(accs) == 0 {
		return nil, fmt.Errorf("no active account")
	}
	idx := int(s.rr.Add(1)-1) % len(accs)
	// deterministic offset by conversation for better distribution
	if in.ConversationID != "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(in.ConversationID))
		idx = int(h.Sum32()) % len(accs)
	}
	acc := accs[idx]
	return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "round_robin"}, nil
}

// ResolveExplicitOutboundAccount 解析出站账号：只有空账号允许自动分派；bridge 显式账号
// 必须精确命中租户内启用账号与渠道，任何无效或仓储错误都原样返回，绝不退化为其他账号。
func (s *ChannelDispatchService) ResolveExplicitOutboundAccount(ctx context.Context, tenantID uint, channel, accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if !isBridgeOutboundChannel(WebhookChannel(channel)) {
		return accountID, nil
	}
	if s == nil || s.accRepo == nil || s.channelRepo == nil {
		return "", fmt.Errorf("outbound account resolver unavailable")
	}
	if tenantID == 0 {
		if tid, ok := tenant.FromContext(ctx); ok {
			tenantID = tid
		}
	}
	if tenantID == 0 {
		return "", fmt.Errorf("outbound account tenant required")
	}
	if accountID == "" {
		pick, err := s.PickAccount(ctx, DispatchPickInput{TenantID: tenantID, Channel: channel})
		if err != nil {
			return "", fmt.Errorf("pick outbound account: %w", err)
		}
		if pick == nil || pick.AccountID == 0 {
			return "", fmt.Errorf("pick outbound account: no active account")
		}
		return strconv.FormatUint(uint64(pick.AccountID), 10), nil
	}

	id, err := strconv.ParseUint(accountID, 10, 32)
	if err != nil || id == 0 {
		return "", fmt.Errorf("invalid explicit outbound account ID %q", accountID)
	}
	acc, err := s.accRepo.GetByID(ctx, tenantID, uint(id))
	if err != nil {
		return "", fmt.Errorf("query explicit outbound account: %w", err)
	}
	if acc == nil {
		return "", fmt.Errorf("explicit outbound account %s not found", accountID)
	}
	if acc.Status != 1 {
		return "", fmt.Errorf("explicit outbound account %s is disabled", accountID)
	}
	ch, err := s.channelRepo.GetByID(ctx, tenantID, acc.ChannelID)
	if err != nil {
		return "", fmt.Errorf("query explicit outbound channel: %w", err)
	}
	if ch == nil {
		return "", fmt.Errorf("explicit outbound channel %d not found or disabled by edition", acc.ChannelID)
	}
	requested := strings.TrimSpace(channel)
	if !strings.EqualFold(strings.TrimSpace(ch.Code), requested) && strconv.FormatUint(uint64(ch.ID), 10) != requested {
		return "", fmt.Errorf("explicit outbound account %s channel mismatch: account=%s requested=%s", accountID, ch.Code, requested)
	}
	return strconv.FormatUint(uint64(acc.ID), 10), nil
}
