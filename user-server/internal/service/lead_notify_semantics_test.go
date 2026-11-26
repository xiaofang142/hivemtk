package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
)

// TestT05_LeadNotifyConfigIsSystemEmployeeConfig 固化 T05 裁决：
// 线索通知配置属于系统配置，收件人只能是本系统员工（system_users.id），
// 不是客服智能体 / 线索智能体 ID；投递通道仅站内 / TG / 飞书。
func TestT05_LeadNotifyConfigIsSystemEmployeeConfig(t *testing.T) {
	valid := &model.LeadMiningNotifyRoute{
		NotifyUserIDs: model.JSONStrings{"18", " 7 "},
		NotifyTypes:   model.JSONStrings{"in_app", "telegram", "feishu"},
	}
	if err := validateNotifyRouteSemantics(valid); err != nil {
		t.Fatalf("系统员工数字 ID + 合法通道应通过，实得 %v", err)
	}

	// 空收件人允许：投递时回退账号维护人（service/lead_notify.go fallbackAccountRoute）。
	empty := &model.LeadMiningNotifyRoute{NotifyUserIDs: model.JSONStrings{}, NotifyTypes: model.JSONStrings{}}
	if err := validateNotifyRouteSemantics(empty); err != nil {
		t.Fatalf("空收件人/空通道应允许回退，实得 %v", err)
	}

	cases := []struct {
		name    string
		route   *model.LeadMiningNotifyRoute
		wantErr error
	}{
		{
			name:    "智能体 ID 被拒绝",
			route:   &model.LeadMiningNotifyRoute{NotifyUserIDs: model.JSONStrings{"agent_54"}},
			wantErr: ErrInvalidNotifyRecipient,
		},
		{
			name:    "线索智能体注册表数字 ID 也不能当员工 ID 之外的自由文本",
			route:   &model.LeadMiningNotifyRoute{NotifyUserIDs: model.JSONStrings{"lead_mining_agent:54"}},
			wantErr: ErrInvalidNotifyRecipient,
		},
		{
			name:    "非数字自由文本被拒绝",
			route:   &model.LeadMiningNotifyRoute{NotifyUserIDs: model.JSONStrings{"not-a-user-id"}},
			wantErr: ErrInvalidNotifyRecipient,
		},
		{
			name:    "0 不是有效系统用户 ID",
			route:   &model.LeadMiningNotifyRoute{NotifyUserIDs: model.JSONStrings{"0"}},
			wantErr: ErrInvalidNotifyRecipient,
		},
		{
			name:    "未知投递通道被拒绝",
			route:   &model.LeadMiningNotifyRoute{NotifyTypes: model.JSONStrings{"sms"}},
			wantErr: ErrInvalidNotifyType,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNotifyRouteSemantics(tc.route)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("期望 %v，实得 %v", tc.wantErr, err)
			}
			if !errors.Is(err, utils.ErrInvalidInput) {
				t.Fatalf("校验错误必须走 utils.ErrInvalidInput 通道以获得 HTTP 400，实得 %v", err)
			}
		})
	}
}

// TestT05_NotifyRouteHasNoAgentBinding 证明通知路由与客服/线索智能体注册表无关：
// 通知路由模型无任何 agent 字段；两类智能体绑定只存在于渠道账号主数据表。
func TestT05_NotifyRouteHasNoAgentBinding(t *testing.T) {
	routeType := reflect.TypeOf(model.LeadMiningNotifyRoute{})
	for i := 0; i < routeType.NumField(); i++ {
		field := routeType.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "agent") {
			t.Fatalf("线索通知路由不应含智能体字段，发现 %s", field.Name)
		}
	}

	accountType := reflect.TypeOf(model.ChannelAccount{})
	for _, want := range []string{"AutoReplyAgentID", "LeadMiningAgentID"} {
		if _, ok := accountType.FieldByName(want); !ok {
			t.Fatalf("客服/线索智能体绑定应只在渠道账号主数据上，缺 %s", want)
		}
	}

	// 收件人字段语义是系统用户 ID 数组，而不是智能体绑定。
	field, ok := routeType.FieldByName("NotifyUserIDs")
	if !ok {
		t.Fatal("通知路由缺 NotifyUserIDs 字段")
	}
	if field.Type != reflect.TypeOf(model.JSONStrings{}) {
		t.Fatalf("NotifyUserIDs 类型应为系统用户 ID 字符串数组，实得 %v", field.Type)
	}
}
