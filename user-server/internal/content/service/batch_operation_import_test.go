package service

// batch_operation_import_test.go CSV 导入解析契约。
//
// 回归背景（2026-11-21 API 全量审计实测）：Excel 导出的中文 CSV 几乎必带 UTF-8 BOM，
// csv.Reader 把 BOM 算进第一列表头（"\ufeff姓名"），strings.TrimSpace 不认 U+FEFF，
// 列名匹配全灭、整批报「缺少必要列: 姓名」。修复=公共入口剥一次首列 BOM；
// 本用例锁住「带 BOM 的模板形状 CSV 与无 BOM 行为一致」。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bopImportCSVFile(t *testing.T, bom bool) *os.File {
	t.Helper()
	header := "姓名,账户,类型,城市,地址,描述,来源ID\n"
	if bom {
		header = "\ufeff" + header
	}
	body := header + "audit20客户A,13800000001,0,北京,北京市朝阳区,audit20测试,audit20-src-1\n" +
		"audit20客户B,13800000002,1,上海,上海市浦东新区,audit20测试,audit20-src-2\n"
	path := filepath.Join(t.TempDir(), "audit20-clues.csv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写 CSV 夹具失败: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 CSV 夹具失败: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestBatchImportFromCSV_StripsBOM 带 BOM 与不带 BOM 的同一份 CSV 必须得到同样的
// 导入结果；BOM 不得吃掉第一列表头导致整批「缺少必要列」。
func TestBatchImportFromCSV_StripsBOM(t *testing.T) {
	_, svc, ctxA, _ := bopSvcDB(t)

	for _, bom := range []bool{false, true} {
		result, err := svc.ImportFromCSV(ctxA, ImportTypeClue, bopImportCSVFile(t, bom))
		if err != nil {
			t.Fatalf("bom=%v 导入失败: %v", bom, err)
		}
		if result.Success != 2 || result.Failed != 0 || result.Total != 2 {
			t.Fatalf("bom=%v 期望 success=2/failed=0/total=2，实际 success=%d/failed=%d/total=%d（errors=%v）",
				bom, result.Success, result.Failed, result.Total, result.Errors)
		}
		for _, e := range result.Errors {
			if strings.Contains(e.Reason, "缺少必要列") {
				t.Fatalf("bom=%v 不应再报缺列: %+v", bom, result.Errors)
			}
		}
	}
}
