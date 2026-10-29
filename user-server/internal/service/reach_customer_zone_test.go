package service

import (
	"context"
	"testing"
	"time"
)

// J-12（选项③）客户时区接入：解析链/优先级/跨时区窗口判定。
// 分层口径钉死：触达静默窗口按客户时区；配额与合规域维持 cstZone。

func TestCustomerZoneFromAttributes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // 期望时区名；"" = 应回退（返回 nil）
	}{
		{"合法 IANA", `{"timezone":"America/New_York"}`, "America/New_York"},
		{"带空格要 Trim", `{"timezone":" Europe/Berlin "}`, "Europe/Berlin"},
		{"坏 JSON", `not-json`, ""},
		{"缺键", `{"foo":1}`, ""},
		{"坏时区名", `{"timezone":"Mars/Olympus"}`, ""},
		{"非字符串值", `{"timezone":123}`, ""},
		{"空档案", ``, ""},
	}
	for _, c := range cases {
		got := CustomerZoneFromAttributes(c.in)
		if c.want == "" {
			if got != nil {
				t.Errorf("%s: 应回退 nil，实得 %v", c.name, got)
			}
			continue
		}
		if got == nil || got.String() != c.want {
			t.Errorf("%s: want %s got %v", c.name, c.want, got)
		}
	}
}

func TestResolveCustomerZonePrecedence(t *testing.T) {
	profile := NewTestZoneResolver(map[string]string{
		"cust-ny": "America/New_York",
		"cust-bad": "Mars/Olympus", // 坏值 → 跳过档案，回退
	})
	cfg := &SendPipelineConfig{CustomerTimeZoneLookup: profile}

	// metadata 优先于档案
	req := &ReachSendRequest{CustomerID: "cust-ny", Metadata: map[string]string{"timezone": "Asia/Tokyo"}}
	if z := resolveCustomerZone(context.Background(), cfg, req); z.String() != "Asia/Tokyo" {
		t.Fatalf("metadata 应优先于档案，实得 %s", z)
	}
	// 无 metadata → 档案
	req = &ReachSendRequest{CustomerID: "cust-ny"}
	if z := resolveCustomerZone(context.Background(), cfg, req); z.String() != "America/New_York" {
		t.Fatalf("档案应生效，实得 %s", z)
	}
	// 档案坏值 → cstZone 回退
	req = &ReachSendRequest{CustomerID: "cust-bad"}
	if z := resolveCustomerZone(context.Background(), cfg, req); z != cstZone {
		t.Fatalf("坏值应回退 cstZone，实得 %s", z)
	}
	// 无 metadata 无档案 → cstZone（零行为变更路径）
	req = &ReachSendRequest{CustomerID: "cust-none"}
	if z := resolveCustomerZone(context.Background(), cfg, req); z != cstZone {
		t.Fatalf("无数据应回退 cstZone，实得 %s", z)
	}
	// lookup 为 nil → 只认 metadata，否则回退
	req = &ReachSendRequest{CustomerID: "cust-ny"}
	if z := resolveCustomerZone(context.Background(), &SendPipelineConfig{}, req); z != cstZone {
		t.Fatalf("lookup 缺省应回退 cstZone，实得 %s", z)
	}
}

func TestQuietHoursWindowRespectsCustomerZone(t *testing.T) {
	// 全局窗口 22:00-8:00（跨零点）。
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 构造纽约当地 23:30（应在窗口内）——同一瞬间在东八区是次日 12:30（窗口外）。
	nyLate := time.Date(2026, 10, 30, 23, 30, 0, 0, ny)
	if !inQuietHoursWindow(nyLate, 22, 8, ny) {
		t.Fatal("纽约当地 23:30 应在静默窗口内")
	}
	if inQuietHoursWindow(nyLate, 22, 8, cstZone) {
		t.Fatal("同一瞬间按东八区（12:30）不应在窗口内——旧口径会误判")
	}
	// 反向：东八区 23:30 在窗口内，纽约（同时刻=11:30）不在。
	cstLate := time.Date(2026, 10, 30, 23, 30, 0, 0, cstZone)
	if !inQuietHoursWindow(cstLate, 22, 8, cstZone) {
		t.Fatal("东八区 23:30 应在窗口内")
	}
	if inQuietHoursWindow(cstLate, 22, 8, ny) {
		t.Fatal("同一瞬间按纽约（11:30）不应在窗口内")
	}
	// zone=nil 回退 cstZone（调用方兼容面）
	if !inQuietHoursWindow(cstLate, 22, 8, nil) {
		t.Fatal("nil zone 应回退 cstZone")
	}
}

func TestNextQuietHoursReleaseUsesZone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 纽约 10-30 23:30 命中窗口 → 释放=纽约当地 10-31 08:00（不是东八区日历）。
	nyLate := time.Date(2026, 10, 30, 23, 30, 0, 0, ny)
	got := nextQuietHoursRelease(nyLate, 8, ny)
	want := time.Date(2026, 10, 31, 8, 0, 0, 0, ny)
	if !got.Equal(want) {
		t.Fatalf("释放时刻应按客户当地日历=%s，实得 %s", want, got)
	}
	// 东八区视角校验同一瞬间：nextQuietHoursRelease(now, 8, cstZone) 是东八区次日 8 点
	cstView := nextQuietHoursRelease(nyLate, 8, cstZone)
	if cstView.Equal(want) {
		t.Fatal("两种时区的释放时刻不应相同")
	}
	// nil zone 回退
	if nextQuietHoursRelease(nyLate, 8, nil) != nextQuietHoursRelease(nyLate, 8, cstZone) {
		t.Fatal("nil zone 应回退 cstZone")
	}
}

// NewTestZoneResolver 契约=回传 custom_attributes 原始串（与生产装配一致：
// 查档器只取串，timezone 键由 service 层统一解析）。
func NewTestZoneResolver(m map[string]string) CustomerTimeZoneLookup {
	return func(ctx context.Context, customerID string) (string, bool) {
		v, ok := m[customerID]
		if !ok {
			return "", false
		}
		return `{"timezone":"` + v + `"}`, true
	}
}
