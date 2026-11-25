package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

func profileReportResult(t *testing.T, customerID, industry, evidenceRef string, degraded bool) *CustomerAIResult {
	t.Helper()
	sug := AISuggestion{Kind: "profile", Content: "画像结论", Confidence: 0.7, ConfidenceSource: "model_estimate"}
	if degraded {
		sug.DegradeReason = "missing_evidence_ref"
		sug.Kind = "profile_degraded"
	} else {
		sug.Evidence = []AIEvidence{{Type: "customer_field", Ref: evidenceRef, Snippet: "依据", Source: "customers", OccurredAt: time.Now().UTC().Format(time.RFC3339)}}
	}
	return &CustomerAIResult{CustomerID: customerID, PromptVersion: customerAIPromptVersion, Suggestions: []AISuggestion{sug}}
}

// TestProfileReportFiltersAndEvidence 报告按账号/行业/阶段筛选，且每条结论带可回查引用；无引用不伪装已证实。
func TestProfileReportFiltersAndEvidence(t *testing.T) {
	rows := []ProfileReportSource{
		{Customer: &model.Customer{ID: "c-1", SourceAccountID: "acc-1", Industry: "电子", Name: "甲"}, Stage: "lead", Result: profileReportResult(t, "c-1", "电子", "industry", false)},
		{Customer: &model.Customer{ID: "c-2", SourceAccountID: "acc-2", Industry: "食品", Name: "乙"}, Stage: "won", Result: profileReportResult(t, "c-2", "食品", "industry", false)},
		{Customer: &model.Customer{ID: "c-3", SourceAccountID: "acc-1", Industry: "电子", Name: "丙"}, Stage: "lead", Result: profileReportResult(t, "c-3", "电子", "", true)},
	}
	report, err := BuildProfileReport(ProfileReportInput{
		Accounts: []string{"acc-1"}, Industries: []string{"电子"}, Stages: []string{"lead"},
		Sources: rows, GeneratedAt: time.Unix(1700000000, 0).UTC(), Version: "v1",
	})
	if err != nil {
		t.Fatalf("生成报告失败: %v", err)
	}
	if report.Count != 2 || len(report.Entries) != 2 || report.Entries[0].CustomerID != "c-1" || !report.Entries[1].Degraded {
		t.Fatalf("三维度筛选/降级标记失败：count=%d entries=%+v", report.Count, report.Entries)
	}
	if len(report.Entries[0].Evidence) == 0 || report.Entries[0].Evidence[0].Source == "" {
		t.Fatalf("报告结论缺引用：%+v", report.Entries[0])
	}
	if report.Version != "v1" || report.GeneratedAt.IsZero() {
		t.Fatalf("报告缺生成时间/版本：%+v", report)
	}
	// 撤权/空交集：账号不匹配时不返回任何行，也不泄露其他账号结论。
	empty, err := BuildProfileReport(ProfileReportInput{Accounts: []string{"acc-x"}, Sources: rows})
	if err != nil || empty.Count != 0 {
		t.Fatalf("空交集应返回空报告：count=%d err=%v", empty.Count, err)
	}
}

// TestProfileReportDegradedConclusion 无证据结论在报告里必须标 degraded，不得写成已证实画像。
func TestProfileReportDegradedConclusion(t *testing.T) {
	report, err := BuildProfileReport(ProfileReportInput{Sources: []ProfileReportSource{{
		Customer: &model.Customer{ID: "c-9", Name: "丁"}, Stage: "lead",
		Result: profileReportResult(t, "c-9", "", "", true),
	}}})
	if err != nil {
		t.Fatalf("生成报告失败: %v", err)
	}
	if len(report.Entries) != 1 || !report.Entries[0].Degraded || len(report.Entries[0].Evidence) != 0 {
		t.Fatalf("无引用结论必须降级：%+v", report.Entries)
	}
	raw := string(mustJSON(report))
	if !strings.Contains(raw, "degraded") {
		t.Fatalf("报告序列化未标记降级：%s", raw)
	}
}

// TestProfileReportRestrictedDownload 下载重验当前权限：创建者/范围/过期/跨租户均需拒绝。
func TestProfileReportRestrictedDownload(t *testing.T) {
	store := NewProfileReportStore()
	report, _ := BuildProfileReport(ProfileReportInput{
		Accounts: []string{"acc-1"}, Sources: []ProfileReportSource{{Customer: &model.Customer{ID: "c-1", SourceAccountID: "acc-1", Name: "甲"}, Result: profileReportResult(t, "c-1", "", "industry", false)}},
	})
	_, err := store.Save(context.Background(), ProfileReportSaveInput{
		Report: report, TenantID: 1, OperatorID: 7, Scope: CustomerDataScope{UserID: 7, AccountIDs: []string{"acc-1"}},
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("保存报告失败: %v", err)
	}
	snapshot, ok := store.Get("1|7|" + report.Version)
	if !ok {
		t.Fatal("报告快照未保存")
	}
	cases := []struct {
		name string
		tid  uint
		uid  uint
		ids  []string
		want error
	}{
		{"owner", 1, 7, []string{"acc-1"}, nil},
		{"other_employee", 1, 8, []string{"acc-1"}, ErrProfileReportForbidden},
		{"narrowed_scope", 1, 7, []string{"acc-2"}, ErrProfileReportForbidden},
		{"cross_tenant", 2, 7, []string{"acc-1"}, ErrProfileReportForbidden},
	}
	for _, c := range cases {
		_, gotErr := store.Download(context.Background(), ProfileReportDownloadInput{
			Snapshot: snapshot, TenantID: c.tid, ActorID: c.uid, CurrentAccounts: c.ids,
		})
		if !errors.Is(gotErr, c.want) {
			t.Errorf("%s: err=%v want=%v", c.name, gotErr, c.want)
		}
	}
	snapshot.ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := store.Download(context.Background(), ProfileReportDownloadInput{Snapshot: snapshot, TenantID: 1, ActorID: 7, CurrentAccounts: []string{"acc-1"}}); !errors.Is(err, ErrProfileReportExpired) {
		t.Fatalf("过期报告应拒绝下载：err=%v", err)
	}
}

// TestProfileReportFileFormats 复用 11/12 文件格式：真 xlsx 与 CSV，含证据列且敏感字段已脱敏。
func TestProfileReportFileFormats(t *testing.T) {
	report := &ProfileReport{
		Version: "v1", GeneratedAt: time.Unix(1700000000, 0).UTC(), Count: 1,
		Entries: []ProfileReportEntry{{CustomerID: "c-1", Industry: "电子", SourceAccountID: "acc-1", Stage: "lead", Content: "客户 13812345678 的结论", Evidence: []AIEvidence{{Source: "customers", Ref: "industry"}}}},
	}
	csvBytes, err := ProfileReportCSV(report, true)
	if err != nil || !strings.Contains(string(csvBytes), "evidence") || strings.Contains(string(csvBytes), "13812345678") {
		t.Fatalf("CSV 格式/脱敏失败：err=%v content=%s", err, csvBytes)
	}
	xlsxBytes, err := ProfileReportXLSX(report, true)
	if err != nil || len(xlsxBytes) < 100 || !strings.Contains(string(xlsxBytes), "PK") {
		t.Fatalf("xlsx 不是有效 OOXML：err=%v len=%d", err, len(xlsxBytes))
	}
	if strings.Contains(string(xlsxBytes), "13812345678") {
		t.Fatal("xlsx 仍含未脱敏手机号")
	}
	_ = json.Valid(csvBytes)
}
