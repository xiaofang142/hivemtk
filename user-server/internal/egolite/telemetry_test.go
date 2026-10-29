package egolite

import (
	"testing"
	"time"
)

// TestSanitizeTelemetry_DropsPageContent 守卫：任何页面内容字段都不得进入遥测。
func TestSanitizeTelemetry_DropsPageContent(t *testing.T) {
	raw := map[string]any{
		"kind":        "run",
		"duration_ms": 120,
		"ok":          true,
		"skill_id":    "github-search",
		"runner_id":   "dev-1",
		// 以下全部必须被丢
		"page_html":  "<html>客户数据</html>",
		"page_text":  "正文",
		"url":        "https://example.com/a?token=x",
		"screenshot": "base64...",
		"message":    "错误详情",
		"selector":   "#main",
		"content":    "内容",
	}
	ev, dropped, err := SanitizeTelemetry(raw)
	if err != nil {
		t.Fatalf("合法样本被拒: %v", err)
	}
	if dropped != 7 {
		t.Fatalf("dropped = %d，期望 7（7 个禁止字段）", dropped)
	}
	if ev.Kind != "run" || ev.DurationMS != 120 || !ev.OK || ev.SkillID != "github-search" {
		t.Fatalf("白名单字段未正确收敛: %+v", ev)
	}
}

// TestSanitizeTelemetry_RejectsFreeText 自由文本（空格/斜杠/尖括号）不得混进字符串字段。
func TestSanitizeTelemetry_RejectsFreeText(t *testing.T) {
	raw := map[string]any{
		"kind":        "run",
		"error_class": "timeout in page https://x",
	}
	ev, dropped, err := SanitizeTelemetry(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ErrorClass != "" || dropped != 1 {
		t.Fatalf("自由文本未被丢: %+v dropped=%d", ev, dropped)
	}
	// 超长也丢
	raw["error_class"] = string(make([]byte, maxTelemetryStringLen+1))
	if ev, d, _ := SanitizeTelemetry(raw); ev.ErrorClass != "" || d != 1 {
		t.Fatalf("超长值未被丢: %+v d=%d", ev, d)
	}
}

// TestSanitizeTelemetry_RequiresKind 缺 kind ⇒ 错（遥测必须以事件类型为维度）。
func TestSanitizeTelemetry_RequiresKind(t *testing.T) {
	if _, _, err := SanitizeTelemetry(map[string]any{"ok": true}); err == nil {
		t.Fatal("缺 kind 应报错")
	}
}

// TestAggregateTelemetry 聚合：计数/成功率/p95（样本同值时 p95 = 该值）。
func TestAggregateTelemetry(t *testing.T) {
	at := time.Now()
	events := []TelemetryEvent{
		{Kind: "run", At: at, DurationMS: 100, OK: true},
		{Kind: "run", At: at, DurationMS: 200, OK: true},
		{Kind: "run", At: at, DurationMS: 300, OK: false},
		{Kind: "install", At: at, DurationMS: 50, OK: true},
	}
	got := AggregateTelemetry(events)
	if len(got) != 2 || got[0].Kind != "install" || got[1].Kind != "run" {
		t.Fatalf("聚合形状不对: %+v", got)
	}
	run := got[1]
	if run.Count != 3 || run.OKCount != 2 || run.FailCount != 1 || run.P95MS != 300 {
		t.Fatalf("run 聚合 = %+v", run)
	}
	if run.SuccessPct < 66 || run.SuccessPct > 67 {
		t.Fatalf("成功率 = %v，期望 ~66.7", run.SuccessPct)
	}
}

// TestForbiddenKeyListIsEnforced 守卫自检：禁止面里的键名确实被 Sanitize 丢弃。
func TestForbiddenKeyListIsEnforced(t *testing.T) {
	for _, k := range TelemetryForbiddenKeys {
		if !IsForbiddenTelemetryKey(k + "_v2") {
			t.Errorf("禁止键 %q 的后缀变体未被判禁止", k)
		}
		_, dropped, _ := SanitizeTelemetry(map[string]any{"kind": "run", k: "x"})
		if dropped == 0 {
			t.Errorf("禁止键 %q 未被丢弃", k)
		}
	}
}
