package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// M-5（2026-11-11）巡检未读优先渠道清单：解析与回落语义。
// 默认值必须与 config_param_seeds.go 里 bridge/patrol_unread_first_channels 的
// DefaultValue 同值（改动要一起改，这里做防漂移对账）。
func TestParsePatrolUnreadFirstChannels_DefaultMatchesSeed(t *testing.T) {
	var seeded []string
	// 从种子清单里找这条的 DefaultValue，防止两处默认值漂移
	found := false
	for _, def := range DefaultParamDefs() {
		if def.Group == "bridge" && def.Key == PatrolUnreadFirstChannelsParam {
			if err := json.Unmarshal([]byte(def.DefaultValue), &seeded); err != nil {
				t.Fatalf("种子 DefaultValue 不是合法 JSON 数组：%v", err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("种子清单里没有 bridge/%s：参数中心下发面与读取口必须成对登记", PatrolUnreadFirstChannelsParam)
	}
	if !reflect.DeepEqual(seeded, parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)) {
		t.Errorf("常量默认清单与种子 DefaultValue 不一致：const=%v seed=%v",
			parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault), seeded)
	}
}

func TestParsePatrolUnreadFirstChannels(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"合法数组原样返回", `["douyin","xhs"]`, []string{"douyin", "xhs"}},
		{"空数组=全渠道关（现行行为）", `[]`, []string{}},
		{"非法 JSON 回落默认", `{bad json`, parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)},
		{"非数组 JSON 回落默认", `"douyin"`, parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)},
		{"元素非字符串回落默认", `[1,2]`, parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)},
		{"null 回落默认", `null`, parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePatrolUnreadFirstChannels(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parse(%q) = %v，期望 %v", tc.raw, got, tc.want)
			}
		})
	}
}

// 参数未装配（全局单例为空，nil-safe stub）→ 回落默认清单。
func TestPatrolUnreadFirstChannels_NotWired_FallsBackToDefault(t *testing.T) {
	SetGlobalForTest(nil)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	got := PatrolUnreadFirstChannels(context.Background())
	want := parsePatrolUnreadFirstChannels(PatrolUnreadFirstChannelsDefault)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("未装配时 = %v，期望默认 %v", got, want)
	}
}
