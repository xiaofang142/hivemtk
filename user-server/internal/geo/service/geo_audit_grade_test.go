package service

import (
	"strings"
	"testing"
)

func TestAuditGradeBands(t *testing.T) {
	cases := map[int]string{95: "优秀", 86: "优秀", 85: "良好", 68: "良好", 67: "待改进", 36: "待改进", 35: "差", 0: "差"}
	for score, want := range cases {
		if got := AuditGrade(score); got != want {
			t.Errorf("AuditGrade(%d)=%s want %s", score, got, want)
		}
	}
}

func TestAuditReportHasGradeAndFixes(t *testing.T) {
	svc := NewTechConfigService()
	rep := svc.RunGEOAudit("https://example.com/a", "t", "短内容", "", "")
	if rep.Grade == "" {
		t.Fatal("Grade 为空")
	}
	if rep.Grade != AuditGrade(rep.Score) {
		t.Errorf("Grade %s 与 Score %d 不一致", rep.Grade, rep.Score)
	}
	if len(rep.Fixes) == 0 {
		t.Fatal("短内容应有未通过项修复清单")
	}
	for i := 1; i < len(rep.Fixes); i++ {
		if rep.Fixes[i].Weight > rep.Fixes[i-1].Weight {
			t.Fatalf("Fixes 未按权重降序: %+v", rep.Fixes)
		}
		if rep.Fixes[i].Suggestion == "" {
			t.Fatalf("Fix 缺少建议: %+v", rep.Fixes[i])
		}
	}
}

func TestSafeAuditBaseURLRejectsPrivate(t *testing.T) {
	for _, raw := range []string{
		"http://localhost/x", "http://127.0.0.1/", "http://10.0.0.5/",
		"http://192.168.1.1/", "http://169.254.169.254/", "ftp://a.com/",
		"http://foo.local/", "",
	} {
		if _, err := safeAuditBaseURL(raw); err == nil {
			t.Errorf("%q 应被拒绝", raw)
		}
	}
	if base, err := safeAuditBaseURL("https://example.com/a/b?x=1"); err != nil || base != "https://example.com" {
		t.Errorf("公网 URL 应通过: base=%q err=%v", base, err)
	}
}

func TestAuditScoreRecompute(t *testing.T) {
	fs := []GeoAuditFactor{{Factor: "a", Pass: true, Weight: 3}, {Factor: "b", Pass: false, Weight: 1}}
	if got := auditScore(fs); got != 75 {
		t.Errorf("auditScore=%d want 75", got)
	}
	if !strings.Contains(fixSuggestion("H1标题存在", ""), "一级标题") {
		t.Error("fixSuggestion 未命中 H1 指引")
	}
}
