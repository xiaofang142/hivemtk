package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 构造一个最小但结构完整的 xlsx（OOXML），不依赖第三方库。
func buildTestXLSX(t *testing.T, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建 xlsx 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写 xlsx 条目失败: %v", err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/></Types>`)
	write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	write("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="客户" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	write("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`)

	var ss strings.Builder
	ss.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="0" uniqueCount="0">`)
	cellRef := func(r, c int) string {
		// 只需 A..Z
		return string(rune('A'+c)) + crmItoa(r+1)
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		sheet.WriteString(`<row r="` + crmItoa(ri+1) + `">`)
		for ci, cell := range row {
			ref := cellRef(ri, ci)
			// 全部用 inlineStr，避免 sharedStrings 索引维护；解析器必须支持 inlineStr。
			sheet.WriteString(`<c r="` + ref + `" t="inlineStr"><is><t>` + xmlEscape(cell) + `</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	ss.WriteString(`</sst>`)
	write("xl/sharedStrings.xml", ss.String())
	write("xl/worksheets/sheet1.xml", sheet.String())
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 xlsx 失败: %v", err)
	}
	return buf.Bytes()
}

func crmItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// CSV：BOM + 引号包裹的逗号 + 字段内换行，必须按标准 CSV 解析成一行。
func TestParseCustomerFile_CSVQuotesNewlinesBOM(t *testing.T) {
	data := "\xEF\xBB\xBFname,phone,industry,source_account_id\n\"张,三\n贸易\",13800138000,制造,acct-a\n"
	rows, format, err := ParseCustomerFile(strings.NewReader(data), "customers.csv")
	if err != nil {
		t.Fatalf("解析 BOM/引号/换行 CSV 失败: %v", err)
	}
	if format != "csv" {
		t.Fatalf("格式应为 csv，实际 %s", format)
	}
	if len(rows) != 1 || rows[0].Name != "张,三\n贸易" || rows[0].Industry != "制造" || rows[0].SourceAccountID != "acct-a" {
		t.Fatalf("CSV 解析结果不符: %+v", rows)
	}
}

// 真正的 xlsx（中文表头 + inlineStr）必须被解析；不能靠改扩展名或加 BOM 冒充。
func TestParseCustomerFile_RealXLSXChinese(t *testing.T) {
	data := buildTestXLSX(t, [][]string{
		{"客户名称", "手机号", "行业", "负责人", "来源账号", "来源渠道", "公司ID"},
		{"张三", "13800138000", "制造", "7", "acct-a", "wechat", "1001"},
	})
	rows, format, err := ParseCustomerFile(bytes.NewReader(data), "customers.xlsx")
	if err != nil {
		t.Fatalf("解析真实 xlsx 失败: %v", err)
	}
	if format != "xlsx" {
		t.Fatalf("格式应为 xlsx，实际 %s", format)
	}
	if len(rows) != 1 || rows[0].Name != "张三" || rows[0].Industry != "制造" || rows[0].OwnerUserID != 7 || rows[0].CompanyID != "1001" {
		t.Fatalf("xlsx 中文映射结果不符: %+v", rows)
	}
}

// 坏格式必须显式失败；改扩展名的 CSV 仍按 CSV 报告，不能声称是 Excel 支持。
func TestParseCustomerFile_BadFormatAndRenamedCSV(t *testing.T) {
	if _, _, err := ParseCustomerFile(bytes.NewReader([]byte("PK\x03\x04broken")), "bad.xlsx"); err == nil {
		t.Fatal("损坏的 xlsx 应返回错误，实际成功")
	}
	_, format, err := ParseCustomerFile(strings.NewReader("name,phone\n张三,13800138000\n"), "fake.xlsx")
	if err != nil {
		t.Fatalf("改扩展名的 CSV 仍应能按 CSV 读取: %v", err)
	}
	if format != "csv" {
		t.Fatalf("改扩展名不能冒充 Excel，格式应为 csv，实际 %s", format)
	}
}

// 体积与行数上限必须生效，阻止超大文件拖垮请求。
func TestParseCustomerFile_SizeAndRowLimits(t *testing.T) {
	big := strings.Repeat("x", customerImportMaxBytes+1)
	if _, _, err := ParseCustomerFile(strings.NewReader(big), "big.csv"); err == nil || !strings.Contains(err.Error(), "体积") {
		t.Fatalf("超大文件应报体积错误，实际 err=%v", err)
	}
	var sb strings.Builder
	sb.WriteString("name,phone\n")
	for i := 0; i <= customerImportMaxRows; i++ {
		sb.WriteString("张三,13800138000\n")
	}
	if _, _, err := ParseCustomerFile(strings.NewReader(sb.String()), "many.csv"); err == nil || !strings.Contains(err.Error(), "行数") {
		t.Fatalf("超行数应报行数错误，实际 err=%v", err)
	}
}

// 字段映射白名单：中文别名映射到受控字段，未知字段不能注入。
func TestNormalizeImportFieldMapping_Whitelist(t *testing.T) {
	mapping := NormalizeImportFieldMapping(map[string]string{
		"客户名称": "name", "行业": "industry", "负责人": "owner_user_id",
		"来源账号": "source_account_id", "公司": "company_id", "恶意字段": "DROP TABLE",
	})
	want := map[string]string{
		"客户名称": "name", "行业": "industry", "负责人": "owner_user_id",
		"来源账号": "source_account_id", "公司": "company_id",
	}
	for k, v := range want {
		if mapping[k] != v {
			t.Fatalf("映射 %s 期望 %s，实际 %s", k, v, mapping[k])
		}
	}
	if _, ok := mapping["恶意字段"]; ok {
		t.Fatal("未知字段必须被白名单拒绝")
	}
}

// 预检逐行显示越权原因与重复原因，且计数与原因一一对应。
func TestPrecheckRows_DeniedAndDuplicateReasons(t *testing.T) {
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}, Industries: []string{"制造"}}
	rows := []CustomerImportRow{
		{Name: "A", Phone: "13000000001", Industry: "制造", SourceAccountID: "acct-a"},
		{Name: "B", Phone: "13000000002", Industry: "金融", SourceAccountID: "acct-a"},
		{Name: "C", Phone: "13000000003", Industry: "制造", SourceAccountID: "acct-b"},
		{Name: "D", Phone: "13000000001", Industry: "制造", SourceAccountID: "acct-a"},
	}
	report := PrecheckRows(scope, rows)
	// skip 策略下重复行仍算可导入行，只是提示重复原因
	if report.DeniedRows != 2 || report.DuplicateRows != 1 || report.ValidRows != 2 {
		t.Fatalf("预检计数不符: %+v", report)
	}
	joined := ""
	for _, e := range report.Errors {
		joined += e.Message
	}
	if !strings.Contains(joined, "越权") || !strings.Contains(joined, "重复") {
		t.Fatalf("预检必须显示越权与重复原因，实际 %s", joined)
	}
	if len(report.Mapping) == 0 {
		t.Fatal("预检必须显示字段映射")
	}
}

// 真实 PG：复用导入作业向导的预检报告，并叠加越权原因后保持同一 jobID。
func TestPrecheckWithScope_ReusesJobReport(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerImportJob{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 6601)
	svc := NewCustomerImportExportService()
	jobSvc := NewCustomerImportJobService()
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	data := "name,phone,source_account_id\nA,13000000011,acct-a\nB,13000000012,acct-b\n"
	report, err := PrecheckWithScope(ctx, svc, jobSvc, 7, "scope.csv", strings.NewReader(data), scope)
	if err != nil {
		t.Fatalf("带权限预检失败: %v", err)
	}
	if report.JobID == "" || report.DeniedRows != 1 {
		t.Fatalf("应复用作业 jobID 并统计 1 条越权，实际 jobID=%q denied=%d", report.JobID, report.DeniedRows)
	}
	found := false
	for _, e := range report.Errors {
		if strings.Contains(e.Message, "越权") {
			found = true
		}
	}
	if !found {
		t.Fatalf("预检错误必须含越权原因，实际 %+v", report.Errors)
	}
	var job model.CustomerImportJob
	if err := db.WithContext(ctx).Where("job_id = ?", report.JobID).First(&job).Error; err != nil {
		t.Fatalf("预检作业未落库: %v", err)
	}
	if job.TenantID != 6601 {
		t.Fatalf("作业租户错误: %d", job.TenantID)
	}
	_ = gorm.ErrRecordNotFound // 保持 import 稳定
}

// CRM-11：字段白名单拒绝恶意注入，空字段返回默认全量。
func TestNormalizeExportFields_Whitelist(t *testing.T) {
	got, err := NormalizeExportFields("customer", []string{"name", "phone", "DROP TABLE customers", "id", "name"})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if len(got) != 3 || got[0] != "name" || got[1] != "phone" || got[2] != "id" {
		t.Fatalf("白名单应过滤恶意/重复字段: %+v", got)
	}
	if _, err := NormalizeExportFields("evil_entity", nil); err == nil {
		t.Fatal("未知实体类型必须拒绝")
	}
	if _, err := NormalizeExportFields("customer", []string{"evil"}); err == nil {
		t.Fatal("全非法字段必须拒绝")
	}
}

// CRM-11：真 xlsx 输出含工作表 XML 与 inlineStr，不能是 BOM CSV 或改扩展名。
func TestExportRowsXLSX_RealOOXML(t *testing.T) {
	rows := []CustomerExportRow{{ID: "1", Name: "张三", Phone: "13800138000", Email: "a@b.com", Industry: "制造"}}
	fields := []string{"id", "name", "phone"}
	data, err := ExportRowsXLSX(rows, fields, true)
	if err != nil {
		t.Fatalf("生成 xlsx 失败: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		t.Fatal("xlsx 必须是 ZIP 容器")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("生成的 xlsx 不是可解压的 ZIP: %v", err)
	}
	text := ""
	foundSheet := false
	for _, f := range zr.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			foundSheet = true
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 xlsx 条目失败: %v", err)
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		text += string(content)
	}
	if !foundSheet || !strings.Contains(text, "inlineStr") {
		t.Fatal("xlsx 缺少工作表/inlineStr 单元格")
	}
	if strings.Contains(text, "13800138000") {
		t.Fatal("受限用户手机号必须在服务端脱敏")
	}
}

// CRM-12：导出作业保存快照；撤权/他员工/过期/跨租户下载均拒绝。
func TestExportJobDownload_ScopeChanges(t *testing.T) {
	store := NewExportJobStore()
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}, Industries: []string{"制造"}}
	job := &ExportJobSnapshot{
		JobID: "exp_1", TenantID: 6602, Query: ExportQuery{EntityType: "customer"},
		Fields: []string{"id", "name"}, Scope: scope, OperatorID: 7,
		Status: "completed", ExpiresAt: time.Now().Add(time.Hour),
		FileContent: []byte("data"), Format: "csv",
	}
	store.Save(job)
	ctx := tenant.WithTenantID(context.Background(), 6602)
	svc := NewCustomerImportExportService()
	if _, _, err := svc.DownloadExportJob(ctx, scope, 7, store, "exp_1"); err != nil {
		t.Fatalf("创建者下载应成功: %v", err)
	}
	narrow := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-b"}, Industries: []string{"制造"}}
	if _, _, err := svc.DownloadExportJob(ctx, narrow, 7, store, "exp_1"); !errors.Is(err, ErrExportJobForbidden) {
		t.Fatalf("范围收窄应拒绝，实际 %v", err)
	}
	if _, _, err := svc.DownloadExportJob(ctx, scope, 8, store, "exp_1"); !errors.Is(err, ErrExportJobForbidden) {
		t.Fatalf("他员工下载应拒绝，实际 %v", err)
	}
	otherTenant := tenant.WithTenantID(context.Background(), 6603)
	if _, _, err := svc.DownloadExportJob(otherTenant, scope, 7, store, "exp_1"); !errors.Is(err, ErrExportJobForbidden) {
		t.Fatalf("跨租户下载应拒绝，实际 %v", err)
	}
	job.ExpiresAt = time.Now().Add(-time.Hour)
	if _, _, err := svc.DownloadExportJob(ctx, scope, 7, store, "exp_1"); !errors.Is(err, ErrExportJobExpired) {
		t.Fatalf("过期应拒绝，实际 %v", err)
	}
}

// CRM-12：过期清理只删文件内容、保留作业状态与审计快照。
func TestExportJobStore_CleanupExpired(t *testing.T) {
	store := NewExportJobStore()
	store.Save(&ExportJobSnapshot{JobID: "a", ExpiresAt: time.Now().Add(-time.Hour), Status: "completed", FileContent: []byte("x")})
	store.Save(&ExportJobSnapshot{JobID: "b", ExpiresAt: time.Now().Add(time.Hour), Status: "completed", FileContent: []byte("y")})
	if n := store.CleanupExpired(time.Now()); n != 1 {
		t.Fatalf("应清理 1 个过期作业，实际 %d", n)
	}
	if len(store.jobs["a"].FileContent) != 0 || store.jobs["a"].Status != "expired" {
		t.Fatal("过期作业应清空文件并标记 expired")
	}
	if len(store.jobs["b"].FileContent) == 0 {
		t.Fatal("未过期作业不应被清理")
	}
}

// CRM-10：重复派发已运行/完成作业幂等，不重建更新。
func TestDispatchImport_Idempotent(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerImportJob{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 6603)
	svc := NewCustomerImportExportService()
	jobs := NewCustomerImportJobService()
	jobID := "imp_dispatch_1"
	creator := uint(7)
	if err := jobs.Create(ctx, &model.CustomerImportJob{
		TenantID: 6603, JobID: jobID, Status: "prechecked", FilePath: "some.csv",
		CreatedBy: &creator, DedupStrategy: "skip",
	}); err != nil {
		t.Fatalf("准备作业失败: %v", err)
	}
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	first, err := svc.DispatchImport(ctx, scope, 7, jobs, jobID)
	if err != nil || first.Status != "queued" {
		t.Fatalf("首次派发应 queued，实际 %+v err=%v", first, err)
	}
	second, err := svc.DispatchImport(ctx, scope, 7, jobs, jobID)
	if err != nil || second.Status != "queued" {
		t.Fatalf("重复派发应幂等，实际 %+v err=%v", second, err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.CustomerImportJob{}).Where("job_id = ?", jobID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("重复派发不应新建作业，count=%d err=%v", count, err)
	}
}

// CRM-10：按批恢复计数可对账；第二批不重复写第一批行。
func TestRunImportBatch_ResumeCounts(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerImportJob{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 6604)
	svc := NewCustomerImportExportService()
	jobs := NewCustomerImportJobService()
	creator := uint(7)
	jobID := "imp_batch_resume"
	if err := jobs.Create(ctx, &model.CustomerImportJob{
		TenantID: 6604, JobID: jobID, Status: "running", FilePath: "batch.csv",
		CreatedBy: &creator, TotalRows: 4, SuccessRows: 2, DedupStrategy: "skip",
	}); err != nil {
		t.Fatalf("准备作业失败: %v", err)
	}
	rows := []CustomerImportRow{
		{Name: "A", Phone: "13000000021", Industry: "制造", SourceAccountID: "acct-a"},
		{Name: "B", Phone: "13000000022", Industry: "制造", SourceAccountID: "acct-a"},
		{Name: "C", Phone: "13000000023", Industry: "制造", SourceAccountID: "acct-a"},
		{Name: "D", Phone: "13000000024", Industry: "制造", SourceAccountID: "acct-a"},
	}
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}, Industries: []string{"制造"}}
	p, err := svc.RunImportBatch(ctx, scope, 7, jobID, rows, 2, nil)
	if err != nil {
		t.Fatalf("第二批执行失败: %v", err)
	}
	if p.Cursor != 4 || p.SuccessRows != 4 || p.Status != "completed" {
		t.Fatalf("恢复计数应对账: %+v", p)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Customer{}).Where("phone IN ?", []string{"13000000021", "13000000022", "13000000023", "13000000024"}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("只应新增后两行，实际 count=%d err=%v", count, err)
	}
}

// CRM-10：每批前重新解析权限，撤权（空授权）时停止后续处理并标记失败。
func TestRunImportBatch_RevokedStops(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerImportJob{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达")
	}
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 6605)
	svc := NewCustomerImportExportService()
	jobs := NewCustomerImportJobService()
	creator := uint(7)
	jobID := "imp_batch_revoke"
	if err := jobs.Create(ctx, &model.CustomerImportJob{
		TenantID: 6605, JobID: jobID, Status: "queued", FilePath: "revoke.csv",
		CreatedBy: &creator, TotalRows: 2, DedupStrategy: "skip",
	}); err != nil {
		t.Fatalf("准备作业失败: %v", err)
	}
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	_, err := svc.RunImportBatch(ctx, scope, 7, jobID, []CustomerImportRow{{Name: "A", Phone: "13000000025"}}, 1, func() (CustomerDataScope, error) {
		return CustomerDataScope{UserID: 7, RestrictedAccounts: true}, nil
	})
	if !errors.Is(err, ErrImportJobForbidden) {
		t.Fatalf("撤权应停止作业，实际 err=%v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.Customer{}).Where("phone = ?", "13000000025").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("撤权后不得创建客户，count=%d err=%v", count, err)
	}
}
