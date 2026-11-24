package service

// FA-28 契约锁：批量导出不得静默截断或漏掉所选客户。
//
// 旧实现把三种导出格式都写成「取第一页（线索 10000 / 用户被夹到 200）后在
// 内存里按 ID 过滤」：
//   - 全量 201 客户只能导 200（user.go 把 limit 夹到 200）；
//   - 全量 10001 线索只能导 10000（第一页上限）；
//   - 选中的 ID 落在第一页之外会被内存过滤静默漏掉。
//
// 这里在真库上钉死：全量逐页遍历到总数、选中按 ID 直查、跨租户选中项遮挡、
// 空结果只有表头、读取失败返回错误（不静默产出半截文件）。

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	datamodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

func seedExportUsers(t *testing.T, db *gorm.DB, tenantID uint, n int) []*datamodel.User {
	t.Helper()
	rows := make([]*datamodel.User, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, &datamodel.User{
			TenantID: tenantID,
			Username: fmt.Sprintf("fa28_u_%04d", i),
			Email:    fmt.Sprintf("fa28_u_%04d@test.local", i),
			RealName: fmt.Sprintf("用户%04d", i),
			Status:   1,
		})
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("批量插入用户失败: %v", err)
	}
	return rows
}

func seedExportClues(t *testing.T, db *gorm.DB, tenantID uint, n int) []*datamodel.Clue {
	t.Helper()
	rows := make([]*datamodel.Clue, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, &datamodel.Clue{
			TenantID: tenantID,
			Name:     fmt.Sprintf("FA28线索%05d", i),
			Account:  fmt.Sprintf("fa28_c_%05d", i),
			Type:     1,
			City:     "北京",
		})
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("批量插入线索失败: %v", err)
	}
	return rows
}

func countCSVRows(t *testing.T, s string) int {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		t.Fatalf("CSV 解析失败: %v", err)
	}
	if len(rows) == 0 {
		return 0
	}
	return len(rows) - 1 // 去表头
}

func countJSONLRows(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func countMarkdownRows(s string) int {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) < 2 {
		return 0
	}
	return len(lines) - 2 // 去表头 + 分隔行
}

// TestBatchExportNoSilentTruncation 一次建夹具（201 用户 / 10001 线索），
// 覆盖三种格式的全量计数、页外选择、空结果、读取失败与跨租户遮挡。
func TestBatchExportNoSilentTruncation(t *testing.T) {
	db, svc, ctxA, _ := bopSvcDB(t)
	users := seedExportUsers(t, db, bopSvcTenantA, 201)
	clues := seedExportClues(t, db, bopSvcTenantA, 10001)
	// 别家租户的同名线索：混进选中列表不应被导出。
	other := &datamodel.Clue{TenantID: bopSvcTenantB, Name: "B租户的FA28线索", Account: "fa28_b_only"}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(other).Error; err != nil {
		t.Fatalf("插入别租户线索失败: %v", err)
	}

	// —— 全量：三种格式都必须导出全部 201 客户 / 10001 线索 ——
	csvUsers, err := svc.GenerateCSV(ctxA, ExportTypeUser, nil)
	if err != nil {
		t.Fatalf("CSV 全量用户: %v", err)
	}
	if n := countCSVRows(t, csvUsers.String()); n != 201 {
		t.Errorf("CSV 全量用户应 201 行，实得 %d（旧实现夹 200 会漏 1）", n)
	}

	jsonlUsers, err := svc.GenerateJSONL(ctxA, ExportTypeUser, nil)
	if err != nil {
		t.Fatalf("JSONL 全量用户: %v", err)
	}
	if n := countJSONLRows(jsonlUsers.String()); n != 201 {
		t.Errorf("JSONL 全量用户应 201 行，实得 %d", n)
	}

	mdUsers, err := svc.GenerateMarkdown(ctxA, ExportTypeUser, nil)
	if err != nil {
		t.Fatalf("Markdown 全量用户: %v", err)
	}
	if n := countMarkdownRows(mdUsers.String()); n != 201 {
		t.Errorf("Markdown 全量用户应 201 行，实得 %d", n)
	}

	csvClues, err := svc.GenerateCSV(ctxA, ExportTypeClue, nil)
	if err != nil {
		t.Fatalf("CSV 全量线索: %v", err)
	}
	if n := countCSVRows(t, csvClues.String()); n != 10001 {
		t.Errorf("CSV 全量线索应 10001 行，实得 %d（旧实现只取第一页 10000）", n)
	}

	jsonlClues, err := svc.GenerateJSONL(ctxA, ExportTypeClue, nil)
	if err != nil {
		t.Fatalf("JSONL 全量线索: %v", err)
	}
	if n := countJSONLRows(jsonlClues.String()); n != 10001 {
		t.Errorf("JSONL 全量线索应 10001 行，实得 %d", n)
	}

	mdClues, err := svc.GenerateMarkdown(ctxA, ExportTypeClue, nil)
	if err != nil {
		t.Fatalf("Markdown 全量线索: %v", err)
	}
	if n := countMarkdownRows(mdClues.String()); n != 10001 {
		t.Errorf("Markdown 全量线索应 10001 行，实得 %d", n)
	}

	// —— 页外选择：选第 5001 / 10000 条，远超第一页，必须命中 ——
	offPageIDs := []string{clues[5000].ID, clues[10000].ID}
	sel, err := svc.GenerateCSV(ctxA, ExportTypeClue, offPageIDs)
	if err != nil {
		t.Fatalf("页外选择导出: %v", err)
	}
	selOut := sel.String()
	for _, id := range offPageIDs {
		if !strings.Contains(selOut, id) {
			t.Errorf("页外选中项 %s 被静默漏掉", id)
		}
	}
	if n := countCSVRows(t, selOut); n != 2 {
		t.Errorf("页外选择应导出 2 行，实得 %d", n)
	}

	// —— 选中用户同样按 ID 直查，页外项不漏 ——
	lastUser := users[200]
	selUsers, err := svc.GenerateJSONL(ctxA, ExportTypeUser, []string{lastUser.ID})
	if err != nil {
		t.Fatalf("页外用户选择导出: %v", err)
	}
	if n := countJSONLRows(selUsers.String()); n != 1 {
		t.Errorf("页外用户选择应导出 1 行，实得 %d", n)
	}

	// —— 跨租户选中项必须遮挡（按租户直查指定 ID）——
	mixed, err := svc.GenerateCSV(ctxA, ExportTypeClue, []string{clues[0].ID, other.ID})
	if err != nil {
		t.Fatalf("混合租户选择导出: %v", err)
	}
	mixedOut := mixed.String()
	if strings.Contains(mixedOut, other.Name) {
		t.Error("别租户的选中项不应出现在导出里")
	}
	if n := countCSVRows(t, mixedOut); n != 1 {
		t.Errorf("混合租户选择应只导出本租户 1 行，实得 %d", n)
	}

	// —— 空结果：只有表头，0 数据行 ——
	empty, err := svc.GenerateCSV(ctxA, ExportTypeUser, []string{"fa28-no-such-id"})
	if err != nil {
		t.Fatalf("空结果导出: %v", err)
	}
	if n := countCSVRows(t, empty.String()); n != 0 {
		t.Errorf("空结果应 0 数据行，实得 %d", n)
	}

	// —— 读取失败：ctx 取消后查询报错，不能静默产出空文件 ——
	ctxFail, cancel := context.WithCancel(ctxA)
	cancel()
	if _, err := svc.GenerateCSV(ctxFail, ExportTypeUser, nil); err == nil {
		t.Error("读取失败（ctx 取消）应返回错误，不能静默产出半截文件")
	}
}
