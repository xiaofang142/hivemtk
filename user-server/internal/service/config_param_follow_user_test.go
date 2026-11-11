package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// M-6（2026-11-11）自动关注动作参数：解析、默认值与种子对账。
// 默认值必须与 config_param_seeds.go 里 bridge/follow_user_enabled /
// bridge/follow_user_channels 的 DefaultValue 同值（改动要一起改，这里做防漂移对账）。

func TestParseFollowUserChannels_DefaultMatchesSeed(t *testing.T) {
	var seededEnabled *string
	var seededChannels *string
	for _, def := range DefaultParamDefs() {
		if def.Group != "bridge" {
			continue
		}
		switch def.Key {
		case FollowUserEnabledParam:
			v := def.DefaultValue
			seededEnabled = &v
		case FollowUserChannelsParam:
			v := def.DefaultValue
			seededChannels = &v
		}
	}
	if seededEnabled == nil || seededChannels == nil {
		t.Fatalf("种子清单里缺 bridge/follow_user_enabled 或 bridge/follow_user_channels：参数中心下发面与读取口必须成对登记")
	}
	if *seededEnabled != "false" {
		t.Errorf("follow_user_enabled 种子默认值 = %q，期望 \"false\"（默认关，硬性边界）", *seededEnabled)
	}
	var seeded []string
	if err := json.Unmarshal([]byte(*seededChannels), &seeded); err != nil {
		t.Fatalf("follow_user_channels 种子 DefaultValue 不是合法 JSON 数组：%v", err)
	}
	if !reflect.DeepEqual(seeded, parseFollowUserChannels(FollowUserChannelsDefault)) {
		t.Errorf("常量默认清单与种子 DefaultValue 不一致：const=%v seed=%v",
			parseFollowUserChannels(FollowUserChannelsDefault), seeded)
	}
}

func TestParseFollowUserChannels(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"合法数组原样返回", `["douyin","bilibili"]`, []string{"douyin", "bilibili"}},
		{"空数组=全渠道关（合法）", `[]`, []string{}},
		{"非法 JSON 回落默认", `{bad json`, parseFollowUserChannels(FollowUserChannelsDefault)},
		{"非数组 JSON 回落默认", `"douyin"`, parseFollowUserChannels(FollowUserChannelsDefault)},
		{"元素非字符串回落默认", `[1,2]`, parseFollowUserChannels(FollowUserChannelsDefault)},
		{"null 回落默认", `null`, parseFollowUserChannels(FollowUserChannelsDefault)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseFollowUserChannels(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parse(%q) = %v，期望 %v", tc.raw, got, tc.want)
			}
		})
	}
}

// 参数未装配（全局单例为空，nil-safe stub）→ 开关默认关、清单回落默认。
func TestFollowUserParams_NotWired_DefaultOff(t *testing.T) {
	SetGlobalForTest(nil)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	ctx := context.Background()
	if FollowUserEnabled(ctx) {
		t.Errorf("参数未装配时 FollowUserEnabled = true，期望默认关（false）")
	}
	got := FollowUserChannels(ctx)
	want := parseFollowUserChannels(FollowUserChannelsDefault)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("未装配时渠道清单 = %v，期望默认 %v", got, want)
	}
}

// 种子读回（需要测试库）：seed 后 GetBool/GetString 能取到定义值（默认关）。
func TestFollowUserParams_Seeded_ReadsBack(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if gdb == nil {
		t.Skip("no DB")
	}
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}
	ctx := context.Background()
	if FollowUserEnabled(ctx) {
		t.Errorf("seed 后 follow_user_enabled = true，期望 false（默认关）")
	}
	channels := FollowUserChannels(ctx)
	if len(channels) == 0 {
		t.Fatalf("seed 后 follow_user_channels 为空，期望默认清单")
	}
	for _, ch := range channels {
		if ch != "douyin" && ch != "tiktok" && ch != "bilibili" {
			t.Errorf("默认清单含意外渠道 %q（xianyu 好友制无关注模型，不应默认开）", ch)
		}
	}
}
