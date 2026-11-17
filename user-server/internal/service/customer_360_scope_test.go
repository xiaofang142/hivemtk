package service

import (
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// TestAssertSessionsVisible 客户域行级隔离判据（2026-11-15 审计 H-1 回归钉）。
//
// 口径必须与 customer_session_scope.go 的 sessionVisible 一致：超管全量；
// 其余角色该客户任一会话归属自己或属未分配池即放行，全归他人即拒绝。
// 这条判据挡的是「列表看不到别人的客户、按 id/查询参数却读得到」的旁路。
func TestAssertSessionsVisible(t *testing.T) {
	mk := func(agentID uint) *model.CustomerSession {
		return &model.CustomerSession{AgentID: agentID}
	}
	admin := SessionScope{Role: "admin"}
	self := SessionScope{Role: "customer_service", UserID: 7}
	other := SessionScope{Role: "staff", UserID: 9}

	cases := []struct {
		name     string
		sessions []*model.CustomerSession
		scope    SessionScope
		wantErr  error
	}{
		{
			name:     "超管全量可见",
			sessions: []*model.CustomerSession{mk(42)},
			scope:    admin,
			wantErr:  nil,
		},
		{
			name:     "自己的会话放行",
			sessions: []*model.CustomerSession{mk(3), mk(7)},
			scope:    self,
			wantErr:  nil,
		},
		{
			name:     "未分配池放行（与 sessionVisible 同口径）",
			sessions: []*model.CustomerSession{mk(0)},
			scope:    self,
			wantErr:  nil,
		},
		{
			name:     "全部归属他人即拒绝",
			sessions: []*model.CustomerSession{mk(3), mk(4)},
			scope:    other,
			wantErr:  ErrCustomerForbidden,
		},
		{
			name:     "空会话集对受限角色拒绝",
			sessions: nil,
			scope:    other,
			wantErr:  ErrCustomerForbidden,
		},
		{
			name:     "空会话集对超管放行",
			sessions: nil,
			scope:    admin,
			wantErr:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertSessionsVisible(tc.sessions, tc.scope)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("assertSessionsVisible(%d sessions) err = %v, want %v", len(tc.sessions), err, tc.wantErr)
			}
		})
	}
}
