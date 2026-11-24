package service

import (
	"context"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// groupCustomerUnifiedID 生成群客户 OneID（D5/D16）。
//
// 格式 grp:<tenant>:<platform>:<account>:<group_id>。grp: 前缀保证群客户与个人客户
// 在 OneID 空间完全隔离（OneID 自动合并口已加排除）；账号+群ID一起进键，保证
// 跨账号同名群不串、同账号不同群独立。群名不入键——群改名不影响身份稳定。
func groupCustomerUnifiedID(tenantID uint, platform, accountID, groupID string) string {
	return fmt.Sprintf("grp:%d:%s:%s:%s", tenantID,
		strings.ToLower(strings.TrimSpace(platform)),
		strings.TrimSpace(accountID),
		strings.TrimSpace(groupID))
}

// resolveCustomerForHub 按消息场景解析客户（D2/D5/D16）。
// 群聊：以 tenant+platform+account_id+group_id 建群客户，customers.name=群名，
// grp: 前缀保证群档与个人档隔离（OneID 合并口已排除）。
// 单聊：沿用既有 resolveCustomer（OneID/legacy 兼容读）。
func (s *Service) resolveCustomerForHub(ctx context.Context, hub *model.MessageHub) (*model.Customer, error) {
	if s == nil || hub == nil {
		return nil, fmt.Errorf("无客户身份标识")
	}
	if !hub.IsGroup || strings.TrimSpace(hub.GroupID) == "" {
		return s.resolveCustomer(ctx, hub.Platform, hub.SenderID, hub.SenderName)
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		if tenant.IsMultiTenant() {
			return nil, fmt.Errorf("无租户归属，拒绝创建群客户")
		}
		tid = tenant.DefaultTenantID
	}
	uid := groupCustomerUnifiedID(tid, hub.Platform, hub.AccountID, hub.GroupID)
	if c, err := s.custRepo.GetByUnifiedID(ctx, uid); err == nil && c != nil {
		return c, nil
	}
	groupName := ""
	if v, exists := hub.Extra["group_name"]; exists {
		groupName = strings.TrimSpace(fmt.Sprint(v))
	}
	if groupName == "" {
		groupName = "群聊:" + hub.GroupID
	}
	c := &model.Customer{UnifiedID: uid, Name: groupName, SourceChannel: hub.Platform, SourceAccountID: hub.AccountID}
	if err := s.custRepo.Create(ctx, c); err != nil {
		if existing, gerr := s.custRepo.GetByUnifiedID(ctx, uid); gerr == nil && existing != nil {
			return existing, nil // 并发同群，回读取已有
		}
		return nil, err
	}
	return c, nil
}
