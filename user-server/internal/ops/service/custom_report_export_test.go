package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	sysrepo "hivemtk-user/internal/repository"
)

func TestExportReportDataCSV_HeaderAndRows(t *testing.T) {
	data := &model.ReportData{
		Dimensions: []string{"date", "status"},
		Metrics:    []string{"session_count"},
		Data: []map[string]any{
			{"date": "2026-08-01", "status": "ai_handling", "session_count": int64(12)},
			{"date": "2026-08-02", "status": "closed", "session_count": int64(7)},
		},
		Total: 2,
	}
	var buf bytes.Buffer
	if err := ExportReportDataCSV(&buf, data); err != nil {
		t.Fatalf("export: %v", err)
	}
	got := buf.String()
	want := "date,status,session_count\n2026-08-01,ai_handling,12\n2026-08-02,closed,7\n"
	if got != want {
		t.Errorf("csv = %q, want %q", got, want)
	}
}

func TestExportReportDataCSV_CellFormattingAndEscaping(t *testing.T) {
	data := &model.ReportData{
		Dimensions: []string{"k"},
		Metrics:    []string{"v"},
		Data: []map[string]any{
			{"k": "含逗号,与引号\"文本", "v": 3.5},
			{"k": nil, "v": true},
			{"k": "", "v": int(42)},
		},
		Total: 3,
	}
	var buf bytes.Buffer
	if err := ExportReportDataCSV(&buf, data); err != nil {
		t.Fatalf("export: %v", err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("rows = %d, want 4 (含表头)", len(records))
	}

	if records[1][0] != `含逗号,与引号"文本` || records[1][1] != "3.5" {
		t.Errorf("row1 = %v", records[1])
	}
	if records[2][0] != "" || records[2][1] != "true" {
		t.Errorf("nil cell 应输出空串: %v", records[2])
	}
}

func TestExportReportDataCSV_OverLimitRejected(t *testing.T) {
	var buf bytes.Buffer
	err := ExportReportDataCSV(&buf, &model.ReportData{
		Dimensions: []string{"d"}, Metrics: []string{"m"},
		Data:  []map[string]any{{"d": "x", "m": 1}},
		Total: CSVExportMaxRows + 1,
	})
	if !errors.Is(err, ErrReportTooManyRows) {
		t.Fatalf("want ErrReportTooManyRows, got %v", err)
	}
	if buf.Len() != 0 {
		t.Error("超限拒绝时不得写出任何字节")
	}

	buf.Reset()
	if err := ExportReportDataCSV(&buf, &model.ReportData{Total: CSVExportMaxRows}); err != nil {
		t.Fatalf("边界内导出失败: %v", err)
	}
}

func TestCustomReportService_ExportCSV_FullPath(t *testing.T) {
	database := testutil.NewTestDB(t, &sysmodel.UnifiedMessage{})
	db.SetTestDB(database)

	svc := NewCustomReportServiceWithRepos(
		opsrepo.NewCustomReportRepositoryWithDB(database),
		sysrepo.NewCustomerSessionRepositoryWithDB(database),
		sysrepo.NewClueRepositoryWithDB(database),
		sysrepo.NewUserRFMRepositoryWithDB(database),
	)

	report := &model.CustomReport{
		Name:       "消息类型分布",
		DataSource: "messages",
		Dimensions: `[{"field":"content_type","label":"消息类型"}]`,
		Metrics:    `[{"field":"message_count","label":"消息数"}]`,
		Filters:    "[]",
		ChartType:  "table",
		IsPublic:   true,
	}
	rows := []*sysmodel.UnifiedMessage{
		{MessageID: "m1", Platform: sysmodel.PlatformWeChat, ContentType: sysmodel.MessageTypeText},
		{MessageID: "m2", Platform: sysmodel.PlatformWeChat, ContentType: sysmodel.MessageTypeText},
		{MessageID: "m3", Platform: sysmodel.PlatformWeChat, ContentType: sysmodel.MessageTypeImage},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	var buf bytes.Buffer
	if err := svc.ExportReportCSV(context.Background(), &buf, report, map[string]any{}); err != nil {
		t.Fatalf("ExportReportCSV: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "content_type,message_count\n") {
		t.Errorf("表头错误: %q", out[:min(len(out), 80)])
	}
	if !strings.Contains(out, ",2\n") || !strings.Contains(out, ",1\n") {
		t.Errorf("聚合计数错误:\n%s", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
