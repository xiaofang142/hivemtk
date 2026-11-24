package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync/atomic"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ChannelDispatchService 账号自动均衡收发（路由2）。
// 策略：会话粘性优先（已有会话绑定账号）-> 启用账号 round-robin。
type ChannelDispatchService struct {
	db      *gorm.DB
	accRepo repository.ChannelAccountRepository
	rr      atomic.Uint64
}

// NewChannelDispatchService 构造。
func NewChannelDispatchService(db *gorm.DB) *ChannelDispatchService {
	return &ChannelDispatchService{db: db, accRepo: repository.NewChannelAccountRepositoryWithDB(db)}
}

type DispatchPickInput struct {
	TenantID       uint
	Channel        string // platform code, e.g. wechat
	ChannelID      uint
	ConversationID string
	CustomerID     string
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
		var ch model.Channel
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND code = ?", in.TenantID, in.Channel).First(&ch).Error; err == nil {
			in.ChannelID = ch.ID
		}
	}
	if in.ChannelID == 0 {
		return nil, fmt.Errorf("channel required")
	}

	// 1) 会话粘性：已有会话绑定账号
	if in.ConversationID != "" || in.CustomerID != "" {
		q := s.db.WithContext(ctx).Model(&model.InboxConversation{}).Where("tenant_id = ? AND platform = ?", in.TenantID, in.Channel)
		if in.ConversationID != "" {
			q = q.Where("conversation_id = ?", in.ConversationID)
		} else {
			q = q.Where("customer_id = ?", in.CustomerID)
		}
		var conv model.InboxConversation
		if err := q.Order("id DESC").First(&conv).Error; err == nil && conv.AccountID != "" {
			// account_id in inbox conversation is string
			var aid uint
			_, _ = fmt.Sscanf(conv.AccountID, "%d", &aid)
			if aid > 0 {
				if acc, _ := s.accRepo.GetByID(ctx, in.TenantID, aid); acc != nil && acc.Status == 1 {
					return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "sticky"}, nil
				}
			}
		}
	}

	// 2) preferred
	if in.PreferredAccountID > 0 {
		if acc, _ := s.accRepo.GetByID(ctx, in.TenantID, in.PreferredAccountID); acc != nil && acc.Status == 1 && acc.ChannelID == in.ChannelID {
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
