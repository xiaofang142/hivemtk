package service

import (
	"context"
	"testing"
)

// 闸门决策必须区分未迁移与故障，且两模块各自独立判定。
func TestModuleGateDecision_States(t *testing.T) {
	cases := []struct {
		name string
		in   ModuleGateInput
		want ModuleGateDecision
	}{
		{"未迁移放行", ModuleGateInput{Bound: false, LookupErr: nil}, ModuleGateUnmigrated},
		{"解析故障关闭", ModuleGateInput{Bound: false, LookupErr: context.DeadlineExceeded}, ModuleGateError},
		{"账号关闭", ModuleGateInput{Bound: true, Status: 0}, ModuleGateDisabled},
		{"未绑定Agent关闭", ModuleGateInput{Bound: true, Status: 1, AgentID: 0}, ModuleGateDisabled},
		{"绑定Agent开启", ModuleGateInput{Bound: true, Status: 1, AgentID: 9}, ModuleGateEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideModuleGate(tc.in); got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}
