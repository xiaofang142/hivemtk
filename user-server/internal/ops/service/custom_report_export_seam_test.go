package service

import (
	"bytes"
	"strings"
	"testing"

	"hivemtk-user/internal/ops/model"
)

// TestCSVExportMaxRowsSeam CSV 导出行数上限的接线三格。
func TestCSVExportMaxRowsSeam(t *testing.T) {
	restore := csvExportMaxRowsProvider
	t.Cleanup(func() { csvExportMaxRowsProvider = restore })

	if got, want := CSVExportMaxRows, 30000; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.csv_export_max_rows 的 DefaultValue（%d）不一致", got, want)
	}

	SetCSVExportMaxRowsProvider(nil)
	if got := csvExportMaxRows(); got != CSVExportMaxRows {
		t.Fatalf("nil 注入时 = %v，期望回落 %d", got, CSVExportMaxRows)
	}

	SetCSVExportMaxRowsProvider(func() int { return 5 })
	if got := csvExportMaxRows(); got != 5 {
		t.Fatalf("注入后 = %v，期望 5", got)
	}
	SetCSVExportMaxRowsProvider(func() int { return 0 })
	if got := csvExportMaxRows(); got != CSVExportMaxRows {
		t.Fatalf("注入 0 时 = %v，期望回落 %d（0 会让任何一行都触发超限）", got, CSVExportMaxRows)
	}
}

// TestExportCSVRespectsSeam 真正的读取点是导出本身：上限压到 2 行时，
// 3 行的报表必须被拒，且错误文案里的上限数字要跟着变。
func TestExportCSVRespectsSeam(t *testing.T) {
	restore := csvExportMaxRowsProvider
	t.Cleanup(func() { csvExportMaxRowsProvider = restore })
	SetCSVExportMaxRowsProvider(func() int { return 2 })

	report := &model.ReportData{Total: 3, Data: []map[string]any{
		{"a": "1"}, {"a": "2"}, {"a": "3"},
	}}
	var buf bytes.Buffer
	err := ExportReportDataCSV(&buf, report)
	if err == nil {
		t.Fatal("3 行报表在上限为 2 时应被拒绝")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("错误文案应带上当前上限 2，实际：%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("被拒时不该吐出半份 CSV，实际写了 %d 字节", buf.Len())
	}

	// 上限之上的那一格：2 行在上限 2 时必须放行，证明 clamp 的是上限而不是恒定值。
	buf.Reset()
	if err := ExportReportDataCSV(&buf, &model.ReportData{Total: 2, Data: []map[string]any{{"a": "1"}, {"a": "2"}}}); err != nil {
		t.Fatalf("2 行报表在上限为 2 时应放行：%v", err)
	}
}
