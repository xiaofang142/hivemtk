package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

const (
	customerImportMaxBytes = 10 << 20 // 与现有 readCSV 的 10MB 上限一致
	customerImportMaxRows  = 10000
)

// CustomerImportRow 客户导入的一行（字段与 CSV 列顺序一致）。
type CustomerImportRow struct {
	Name            string
	Phone           string
	Email           string
	Industry        string
	SourceAccountID string
	SourceChannel   string
	OwnerUserID     uint
	Tags               string
	CompanyID          string // 公司主键（受控字段，需与公司范围校验）
	BusinessType       string // 业务类型（CRM-19 字典受控字段）
	RequirementCategory string // 客户需求分类（CRM-19 字典受控字段）
}

// CustomerImportResult 导入结果汇总。
type CustomerImportResult struct {
	Total   int      `json:"total"`
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Skipped int      `json:"skipped"`
	Denied  int      `json:"denied"`
	Errors  []string `json:"errors,omitempty"`
}

// ImportPrecheckBrief 复用导入作业预检的精简视图：映射/非法行/重复/越权原因。
type ImportPrecheckBrief struct {
	JobID         string            `json:"job_id"`
	TotalRows     int               `json:"total_rows"`
	ValidRows     int               `json:"valid_rows"`
	InvalidRows   int               `json:"invalid_rows"`
	DuplicateRows int               `json:"duplicate_rows"`
	DeniedRows    int               `json:"denied_rows"`
	Mapping       map[string]string `json:"mapping"`
	Errors        []struct {
		Row     int    `json:"row"`
		Message string `json:"message"`
	} `json:"errors"`
}

// CustomerExportRow 客户导出行。
type CustomerExportRow struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	Tags            string `json:"tags,omitempty"`
}

// CustomerExportResult 导出结果。敏感字段按权限决定是否脱敏。
type CustomerExportResult struct {
	Rows            []CustomerExportRow `json:"rows"`
	Total           int64               `json:"total"`
	SensitiveMasked bool                `json:"sensitive_masked"`
}

// CustomerImportExportService 客户导入导出（FR-004/005）。
//
// 权限：导入按账号/行业/负责人范围逐行校验，越权行计入 Denied 而不落库；
// 导出复用 CustomerPermissionService 的范围过滤，并按角色决定敏感字段是否脱敏；
// 两个动作都写 operation_logs 审计。
type CustomerImportExportService struct {
	permission *CustomerPermissionService
	customers  repository.CustomerRepository
	scopeRepo  repository.CRMCustomerRepository
	auditRepo  repository.OperationLogRepository
}

func NewCustomerImportExportService() *CustomerImportExportService {
	return &CustomerImportExportService{
		permission: NewCustomerPermissionService(),
		customers:  repository.NewCustomerRepository(),
		scopeRepo:  repository.NewCRMCustomerRepository(),
		auditRepo:  repository.NewOperationLogRepository(),
	}
}

// ParseCustomerCSV 把 CSV 流解析为导入行。表头固定顺序：
// name,phone,email,industry,source_account_id,source_channel,owner_user_id,tags
func ParseCustomerCSV(r io.Reader) ([]CustomerImportRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows := make([]CustomerImportRow, 0)
	first := true
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV 解析失败: %w", err)
		}
		if first {
			first = false
			if len(rec) > 0 && strings.TrimSpace(strings.ToLower(rec[0])) == "name" {
				continue // 跳过表头
			}
		}
		if len(rec) == 0 || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		get := func(i int) string {
			if i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		ownerID, _ := strconv.ParseUint(get(6), 10, 32)
		rows = append(rows, CustomerImportRow{
			Name: get(0), Phone: get(1), Email: get(2), Industry: get(3),
			SourceAccountID: get(4), SourceChannel: get(5),
			OwnerUserID: uint(ownerID), Tags: get(7),
		})
	}
	return rows, nil
}

// Import 按权限导入客户。
func (s *CustomerImportExportService) Import(ctx context.Context, scope CustomerDataScope, rows []CustomerImportRow, operatorName string) (*CustomerImportResult, error) {
	result := &CustomerImportResult{Total: len(rows), Errors: []string{}}
	if !scope.AllAccounts && scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		result.Denied = len(rows)
		return result, nil
	}
	for i, row := range rows {
		if !s.rowAllowed(scope, row) {
			result.Denied++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行：超出数据权限范围", i+1))
			continue
		}
		if row.Name == "" && row.Phone == "" && row.Email == "" {
			result.Skipped++
			continue
		}

		existing, err := s.customers.FindByIdentity(ctx, row.Phone, row.Email, "", "", "")
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行：查重失败 %v", i+1, err))
			continue
		}
		if existing != nil {
			existing.Name = firstNonEmptyCRMValue(row.Name, existing.Name)
			existing.Industry = firstNonEmptyCRMValue(row.Industry, existing.Industry)
			existing.SourceAccountID = firstNonEmptyCRMValue(row.SourceAccountID, existing.SourceAccountID)
			existing.SourceChannel = firstNonEmptyCRMValue(row.SourceChannel, existing.SourceChannel)
			if row.OwnerUserID > 0 {
				existing.OwnerUserID = &row.OwnerUserID
			}
			if row.Tags != "" {
				existing.Tags = row.Tags
			}
			if err := s.customers.Update(ctx, existing); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行：更新失败 %v", i+1, err))
				continue
			}
			result.Updated++
			continue
		}

		ownerID := row.OwnerUserID
		if scope.OnlyOwned {
			ownerID = scope.UserID
		} else if ownerID == 0 && scope.OwnerUserID > 0 {
			ownerID = scope.OwnerUserID
		}
		c := &model.Customer{
			Name: row.Name, Phone: row.Phone, Email: row.Email, Industry: row.Industry,
			SourceAccountID: row.SourceAccountID, SourceChannel: row.SourceChannel,
			Tags: firstNonEmptyCRMValue(row.Tags, "[]"), ChurnRisk: "low",
		}
		if ownerID > 0 {
			c.OwnerUserID = &ownerID
		}
		if err := s.customers.Create(ctx, c); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行：创建失败 %v", i+1, err))
			continue
		}
		result.Created++
	}

	s.writeAudit(ctx, "customer_import", operatorName, result)
	return result, nil
}

func (s *CustomerImportExportService) rowAllowed(scope CustomerDataScope, row CustomerImportRow) bool {
	return crmImportDenyReason(scope, row) == ""
}

// Export 按权限导出客户；非全量账号用户导出时敏感字段脱敏。
func (s *CustomerImportExportService) Export(ctx context.Context, scope CustomerDataScope, base repository.CustomerScopeFilter, operatorName string) (*CustomerExportResult, error) {
	filter := s.permission.ToFilter(scope, base)
	if !scope.AllAccounts && scope.RestrictedAccounts && len(filter.SourceAccountIDs) == 0 {
		return &CustomerExportResult{Rows: []CustomerExportRow{}, SensitiveMasked: true}, nil
	}
	masked := !scope.AllAccounts
	const pageSize = 500
	result := &CustomerExportResult{Rows: []CustomerExportRow{}, SensitiveMasked: masked}
	filter.PageSize = pageSize
	for page := 1; ; page++ {
		filter.Page = page
		rows, total, err := s.scopeRepo.ListScoped(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("导出客户失败 page=%d: %w", page, err)
		}
		if page == 1 {
			result.Total = total
		}
		for _, c := range rows {
			row := CustomerExportRow{
				ID: c.ID, Name: c.Name, Industry: c.Industry,
				SourceAccountID: c.SourceAccountID, Tags: c.Tags,
			}
			if c.OwnerUserID != nil {
				row.OwnerUserID = strconv.FormatUint(uint64(*c.OwnerUserID), 10)
			}
			if masked {
				row.Phone = maskPhone(c.Phone)
				row.Email = maskEmail(c.Email)
			} else {
				row.Phone = c.Phone
				row.Email = c.Email
			}
			result.Rows = append(result.Rows, row)
		}
		if len(rows) < pageSize || len(result.Rows) >= int(result.Total) {
			break
		}
	}
	s.writeAudit(ctx, "customer_export", operatorName, map[string]any{"exported": len(result.Rows), "total": result.Total, "masked": masked})
	return result, nil
}

// ExportByQuery 按查询条件导出（Controller 不接触 repository 层）。
func (s *CustomerImportExportService) ExportByQuery(ctx context.Context, scope CustomerDataScope, keyword, industry string, ownerUserID uint, operatorName string) (*CustomerExportResult, error) {
	filter := repository.CustomerScopeFilter{
		Keyword: keyword, ExcludeMerged: true,
	}
	if industry != "" {
		filter.Industries = []string{industry}
	}
	if ownerUserID > 0 {
		filter.OwnerUserID = ownerUserID
	}
	return s.Export(ctx, scope, filter, operatorName)
}

func (s *CustomerImportExportService) writeAudit(ctx context.Context, action, operatorName string, detail any) {
	if s.auditRepo == nil {
		return
	}
	raw, _ := json.Marshal(detail)
	_ = s.auditRepo.Create(ctx, &model.OperationLog{
		Action: action, Module: "crm", Resource: "customer",
		Username: operatorName, Detail: string(raw),
	})
}

func firstNonEmptyCRMValue(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------- CRM-09：复用导入向导的字段映射 / 真 xlsx / 预检口径 ----------

var crmImportAllowedFields = map[string]bool{
	"name": true, "phone": true, "email": true, "industry": true,
	"source_account_id": true, "source_channel": true, "owner_user_id": true,
	"tags": true, "company_id": true, "company": true,
	"wechat_open_id": true, "douyin_open_id": true, "xiaohongshu_id": true,
	"whatsapp_phone": true, "telegram_chat_id": true,
	"business_type": true, "requirement_category": true,
}

// NormalizeImportFieldMapping 把用户映射归一到白名单字段，未知目标字段一律丢弃。
func NormalizeImportFieldMapping(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" {
			continue
		}
		if crmImportAllowedFields[v] {
			if v == "company" {
				v = "company_id"
			}
			out[k] = v
		}
	}
	return out
}

// ParseCustomerFile 解析 CSV 或真正的 xlsx。
// 格式按文件 ZIP magic / 真实 OOXML 部件判定，不能靠改扩展名或加 BOM 冒充 Excel。
func ParseCustomerFile(r io.Reader, fileName string) ([]CustomerImportRow, string, error) {
	data, err := io.ReadAll(io.LimitReader(r, customerImportMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取导入文件失败: %w", err)
	}
	if len(data) > customerImportMaxBytes {
		return nil, "", fmt.Errorf("导入文件体积超限（上限 %d 字节）", customerImportMaxBytes)
	}
	var rows [][]string
	format := "csv"
	if isXLSXData(data) {
		format = "xlsx"
		rows, err = parseXLSXRows(data)
	} else {
		if _, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data))); zerr != nil && bytes.HasPrefix(data, []byte("PK\x03\x04")) {
			return nil, "", fmt.Errorf("xlsx 文件损坏：不是有效的 Excel 工作簿")
		}
		rows, err = parseCSVRows(data)
	}
	if err != nil {
		return nil, format, err
	}
	if len(rows) > customerImportMaxRows+1 {
		return nil, format, fmt.Errorf("导入文件行数超限（上限 %d 行）", customerImportMaxRows)
	}
	if len(rows) == 0 {
		return nil, format, errors.New("导入文件为空或缺少表头")
	}
	headers := normalizeCRMHeaders(rows[0])
	idx := make(map[string]int, len(headers))
	for i, h := range headers {
		idx[h] = i
	}
	aliases := map[string]string{
		"客户名称": "name", "姓名": "name", "手机号": "phone", "邮箱": "email",
		"行业": "industry", "负责人": "owner_user_id", "来源账号": "source_account_id",
		"来源渠道": "source_channel", "公司": "company_id", "公司id": "company_id",
		"标签": "tags", "业务类型": "business_type", "需求分类": "requirement_category",
	}
	out := make([]CustomerImportRow, 0, len(rows)-1)
	for i, rec := range rows[1:] {
		if crmRowEmpty(rec) {
			continue
		}
		get := func(field string) string {
			positions := []string{field}
			for alias, canonical := range aliases {
				if canonical == field {
					positions = append(positions, alias)
				}
			}
			for _, p := range positions {
				if n, ok := idx[p]; ok && n < len(rec) {
					if v := strings.TrimSpace(rec[n]); v != "" {
						return v
					}
				}
			}
			return ""
		}
		owner, _ := strconv.ParseUint(get("owner_user_id"), 10, 32)
		out = append(out, CustomerImportRow{
			Name: get("name"), Phone: get("phone"), Email: get("email"),
			Industry: get("industry"), SourceAccountID: get("source_account_id"),
			SourceChannel: get("source_channel"), OwnerUserID: uint(owner),
			Tags: get("tags"), CompanyID: get("company_id"),
			BusinessType: get("business_type"), RequirementCategory: get("requirement_category"),
		})
		_ = i
	}
	return out, format, nil
}

func isXLSXData(data []byte) bool {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return false
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "[Content_Types].xml" || strings.HasPrefix(f.Name, "xl/") {
			return true
		}
	}
	return false
}

func parseCSVRows(data []byte) ([][]string, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false // 坏 CSV 必须显式报错，不能静默吞掉不匹配的引号
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}

// parseXLSXRows 用标准库解析最小 OOXML：sharedStrings + inlineStr + 数字/布尔单元格。
func parseXLSXRows(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("xlsx  ZIP 损坏: %w", err)
	}
	var shared []string
	var sheetXML []byte
	foundSheet := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("xlsx 条目打不开: %w", err)
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("xlsx 条目读取失败: %w", readErr)
		}
		switch filepath.Base(f.Name) {
		case "sharedStrings.xml":
			shared = parseXLSXSharedStrings(content)
		case "sheet1.xml":
			sheetXML, foundSheet = content, true
		}
	}
	if !foundSheet {
		return nil, errors.New("xlsx 缺少工作表 sheet1.xml")
	}
	return parseXLSXSheet(sheetXML, shared)
}

func parseXLSXSharedStrings(data []byte) []string {
	var doc struct {
		Items []struct {
			Text string `xml:"t"`
		} `xml:"si"`
	}
	_ = xml.Unmarshal(data, &doc)
	out := make([]string, 0, len(doc.Items))
	for _, item := range doc.Items {
		out = append(out, item.Text)
	}
	return out
}

func parseXLSXSheet(data []byte, shared []string) ([][]string, error) {
	var doc struct {
		Rows []struct {
			Cells []struct {
				Type string `xml:"t,attr"`
				V    string `xml:"v"`
				Is   struct {
					Text string `xml:"t"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("xlsx 工作表 XML 损坏: %w", err)
	}
	rows := make([][]string, 0, len(doc.Rows))
	for _, r := range doc.Rows {
		cells := make([]string, 0, len(r.Cells))
		for _, c := range r.Cells {
			value := c.Is.Text
			if value == "" {
				value = c.V
			}
			if c.Type == "s" {
				if n, err := strconv.Atoi(c.V); err == nil && n >= 0 && n < len(shared) {
					value = shared[n]
				}
			}
			cells = append(cells, value)
		}
		rows = append(rows, cells)
	}
	return rows, nil
}

func normalizeCRMHeaders(headers []string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		h = strings.TrimSpace(strings.TrimPrefix(h, "\xEF\xBB\xBF"))
		h = strings.ToLower(h)
		h = strings.ReplaceAll(h, " ", "_")
		out[i] = h
	}
	return out
}

func crmRowEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// PrecheckRows 在同一套口径下给出映射/非法行/重复/越权原因（不写库）。
func PrecheckRows(scope CustomerDataScope, rows []CustomerImportRow) ImportPrecheckBrief {
	report := ImportPrecheckBrief{
		TotalRows: len(rows), Mapping: map[string]string{"来源表头": "字段白名单"},
		Errors: []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		}{},
	}
	seen := make(map[string]int)
	for i, row := range rows {
		rowNum := i + 1
		deniedReason := crmImportDenyReason(scope, row)
		if deniedReason != "" {
			report.DeniedRows++
			report.Errors = append(report.Errors, struct {
				Row     int    `json:"row"`
				Message string `json:"message"`
			}{Row: rowNum, Message: "越权: " + deniedReason})
			continue
		}
		if row.Name == "" && row.Phone == "" && row.Email == "" {
			report.InvalidRows++
			report.Errors = append(report.Errors, struct {
				Row     int    `json:"row"`
				Message string `json:"message"`
			}{Row: rowNum, Message: "非法行: 姓名/手机号/邮箱均为空"})
			continue
		}
		key := row.Phone
		if key == "" {
			key = row.Email
		}
		if key != "" {
			if prev, ok := seen[key]; ok {
				report.DuplicateRows++
				report.Errors = append(report.Errors, struct {
					Row     int    `json:"row"`
					Message string `json:"message"`
				}{Row: rowNum, Message: fmt.Sprintf("重复: 与第%d行相同(%s)", prev, key)})
			} else {
				seen[key] = rowNum
			}
		}
		report.ValidRows++
	}
	return report
}

func crmImportDenyReason(scope CustomerDataScope, row CustomerImportRow) string {
	if scope.AllAccounts {
		return ""
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return "操作者无可见账号授权"
	}
	if scope.RestrictedAccounts && !containsCRMString(scope.AccountIDs, row.SourceAccountID) {
		return "来源账号不在授权范围"
	}
	if len(scope.Industries) > 0 && !containsCRMString(scope.Industries, row.Industry) {
		return "行业不在授权范围"
	}
	if scope.OnlyOwned && row.OwnerUserID != scope.UserID {
		return "负责人非当前操作者"
	}
	if scope.OwnerUserID > 0 && row.OwnerUserID != scope.OwnerUserID {
		return "负责人不在授权范围"
	}
	return ""
}

// PrecheckWithScope 复用既有 CustomerImportJobService 的预检/作业，再叠加越权原因。
// 不新建第二套预检或报告系统：jobID 与持久化报告完全来自既有作业服务。
func PrecheckWithScope(ctx context.Context, _ *CustomerImportExportService, jobs *CustomerImportJobService, actorID uint, fileName string, reader io.Reader, scope CustomerDataScope, dicts ...crmBusinessDictReader) (*ImportPrecheckBrief, error) {
	if jobs == nil {
		jobs = NewCustomerImportJobService()
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取导入文件失败: %w", err)
	}
	rows, _, parseErr := ParseCustomerFile(bytes.NewReader(data), fileName)
	// 文件格式坏时仍交给既有作业预检给出原始错误，保持单一报告出口。
	raw, err := jobs.PrecheckScoped(ctx, actorID, fileName, bytes.NewReader(data), "customer", "skip", "{}")
	if err != nil {
		return nil, err
	}
	if parseErr != nil {
		rows = nil
	}
	brief := &ImportPrecheckBrief{
		JobID: raw.JobID, TotalRows: raw.TotalRows, ValidRows: raw.ValidRows,
		InvalidRows: raw.InvalidRows, DuplicateRows: raw.DuplicateRows,
		Mapping: raw.Mapping,
		Errors: []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		}{},
	}
	for _, r := range raw.Errors {
		brief.Errors = append(brief.Errors, struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		}{Row: r.Row, Message: r.Message})
	}
	// CRM-19：同一份预检报告里叠加业务字典校验。读字典失败显式返回，
	// 不能把读失败当成「没有停用项」静默放行。
	var indDisabled, btDisabled, rcDisabled map[string]bool
	if len(dicts) > 0 && dicts[0] != nil {
		var derr error
		indDisabled, derr = CRMDisabledDictCodes(ctx, dicts[0], CRMDictIndustry)
		if derr == nil {
			btDisabled, derr = CRMDisabledDictCodes(ctx, dicts[0], CRMDictBusinessType)
		}
		if derr == nil {
			rcDisabled, derr = CRMDisabledDictCodes(ctx, dicts[0], CRMDictRequirementCategory)
		}
		if derr != nil {
			return nil, derr
		}
	}
	for i, row := range rows {
		reason := crmImportDenyReason(scope, row)
		if reason == "" && (indDisabled != nil || btDisabled != nil || rcDisabled != nil) {
			if derr := CRMValidateRowDicts(row, indDisabled, btDisabled, rcDisabled); derr != nil {
				reason = derr.Error()
			}
		}
		if reason == "" {
			continue
		}
		brief.DeniedRows++
		brief.Errors = append(brief.Errors, struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		}{Row: i + 2, Message: "越权: " + reason})
	}
	if brief.Mapping == nil {
		brief.Mapping = map[string]string{}
	}
	brief.Mapping["格式"] = "复用导入向导"
	if len(brief.Errors) == 0 {
		brief.Errors = []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		}{}
	}
	return brief, nil
}

// ---------- CRM-10：可恢复的后台导入作业（复用既有作业持久化字段承载游标） ----------

// ImportProgress 导入进度：返回给 UI 的 jobID + 按批持久化计数。
type ImportProgress struct {
	JobID       string   `json:"job_id"`
	Status      string   `json:"status"`
	Cursor      int      `json:"cursor"`
	TotalRows   int      `json:"total_rows"`
	SuccessRows int      `json:"success_rows"`
	SkippedRows int      `json:"skipped_rows"`
	FailedRows  int      `json:"failed_rows"`
	UpdatedRows int      `json:"updated_rows"`
	Errors      []string `json:"errors,omitempty"`
}

// ImportDispatch 请求执行时只返回 jobID，不阻塞在请求内。
type ImportDispatch struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// DispatchImport 校验并标记作业 queued，返回 jobID；实际写入由 RunImportBatch 按批执行。
// 重复派发对已完成/运行中作业幂等（只返回 jobID，不重建、不重跑）。
func (s *CustomerImportExportService) DispatchImport(ctx context.Context, scope CustomerDataScope, actorID uint, jobs *CustomerImportJobService, jobID string) (*ImportDispatch, error) {
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return nil, ErrImportJobForbidden
	}
	j, err := jobs.GetScoped(ctx, scope, actorID, jobID)
	if err != nil {
		return nil, err
	}
	switch j.Status {
	case "running", "queued", "completed":
		return &ImportDispatch{JobID: jobID, Status: j.Status}, nil
	}
	if j.FilePath == "" {
		return nil, errors.New("导入任务缺少文件路径，请重新预检")
	}
	j.Status = "queued"
	if err := jobs.UpdateStatus(ctx, jobID, "queued", j.TotalRows, j.SuccessRows, j.FailedRows, j.SkippedRows, j.UpdatedRows, j.ErrorReport); err != nil {
		return nil, fmt.Errorf("标记导入排队失败: %w", err)
	}
	return &ImportDispatch{JobID: jobID, Status: "queued"}, nil
}

// RunImportBatch 执行一批：从 job.PrecheckReport/ErrorReport 持久化游标恢复。
// scopeResolver 每批前重新解析权限，撤权时停止后续处理（不写新行）。
// rows 为解析后的完整行集，cursor 之前的结果从作业计数对账，不重复写入。
func (s *CustomerImportExportService) RunImportBatch(ctx context.Context, scope CustomerDataScope, actorID uint, jobID string, rows []CustomerImportRow, batchSize int, scopeResolver func() (CustomerDataScope, error)) (*ImportProgress, error) {
	if batchSize <= 0 {
		batchSize = 50
	}
	jobs := NewCustomerImportJobService()
	j, err := jobs.GetScoped(ctx, scope, actorID, jobID)
	if err != nil {
		return nil, err
	}
	if j.Status == "completed" {
		return importProgressFromJob(j), nil
	}
	if scopeResolver != nil {
		refreshed, err := scopeResolver()
		if err != nil {
			return nil, err
		}
		if refreshed.RestrictedAccounts && len(refreshed.AccountIDs) == 0 || !sameImportScope(scope, refreshed) {
			j.Status = "failed"
			_ = jobs.UpdateStatus(ctx, jobID, "failed", j.TotalRows, j.SuccessRows, j.FailedRows, j.SkippedRows, j.UpdatedRows, cjMustJSON([]ImportPrecheckRowError{{Message: "权限已撤销或收窄，作业停止"}}))
			return nil, ErrImportJobForbidden
		}
		scope = refreshed
	}
	cursor := j.SuccessRows + j.SkippedRows + j.FailedRows
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	progress := importProgressFromJob(j)
	progress.Cursor = cursor
	end := cursor + batchSize
	if end > len(rows) {
		end = len(rows)
	}
	for i := cursor; i < end; i++ {
		row := rows[i]
		if !s.rowAllowed(scope, row) {
			progress.FailedRows++
			progress.Errors = append(progress.Errors, fmt.Sprintf("第 %d 行：超出数据权限范围", i+1))
			continue
		}
		if row.Name == "" && row.Phone == "" && row.Email == "" {
			progress.SkippedRows++
			continue
		}
		existing, err := s.customers.FindByIdentity(ctx, row.Phone, row.Email, "", "", "")
		if err != nil {
			progress.FailedRows++
			progress.Errors = append(progress.Errors, fmt.Sprintf("第 %d 行：查重失败 %v", i+1, err))
			continue
		}
		ownerID := row.OwnerUserID
		if scope.OnlyOwned {
			ownerID = scope.UserID
		}
		if !s.permission.AllowsWrite(scope, row.SourceAccountID, row.Industry, ownerID) {
			progress.FailedRows++
			progress.Errors = append(progress.Errors, fmt.Sprintf("第 %d 行：超出写入权限", i+1))
			continue
		}
		if existing != nil {
			existing.Name = firstNonEmptyCRMValue(row.Name, existing.Name)
			existing.Industry = firstNonEmptyCRMValue(row.Industry, existing.Industry)
			existing.SourceAccountID = firstNonEmptyCRMValue(row.SourceAccountID, existing.SourceAccountID)
			existing.SourceChannel = firstNonEmptyCRMValue(row.SourceChannel, existing.SourceChannel)
			if row.OwnerUserID > 0 {
				existing.OwnerUserID = &ownerID
			}
			if row.Tags != "" {
				existing.Tags = row.Tags
			}
			if err := s.customers.Update(ctx, existing); err != nil {
				progress.FailedRows++
				progress.Errors = append(progress.Errors, fmt.Sprintf("第 %d 行：更新失败 %v", i+1, err))
				continue
			}
			progress.UpdatedRows++
			progress.SuccessRows++
			continue
		}
		owner := ownerID
		c := &model.Customer{
			Name: row.Name, Phone: row.Phone, Email: row.Email, Industry: row.Industry,
			SourceAccountID: row.SourceAccountID, SourceChannel: row.SourceChannel,
			Tags: firstNonEmptyCRMValue(row.Tags, "[]"), ChurnRisk: "low",
		}
		if owner > 0 {
			c.OwnerUserID = &owner
		}
		if err := s.customers.Create(ctx, c); err != nil {
			progress.FailedRows++
			progress.Errors = append(progress.Errors, fmt.Sprintf("第 %d 行：创建失败 %v", i+1, err))
			continue
		}
		progress.SuccessRows++
	}
	progress.Cursor = end
	if end >= len(rows) {
		progress.Status = "completed"
	} else {
		progress.Status = "running"
	}
	errReport := cjMustJSON(progress.Errors)
	if err := jobs.UpdateStatus(ctx, jobID, progress.Status, len(rows), progress.SuccessRows, progress.FailedRows, progress.SkippedRows, progress.UpdatedRows, errReport); err != nil {
		// 状态写失败显式返回，不伪装成功。
		return progress, fmt.Errorf("写入导入批次结果失败: %w", err)
	}
	return progress, nil
}

// ImportProgressFromJob 供 UI 查询进度：直接读作业持久化的计数。
func (s *CustomerImportExportService) ImportProgressFromJob(ctx context.Context, scope CustomerDataScope, actorID uint, jobID string) (*ImportProgress, error) {
	jobs := NewCustomerImportJobService()
	j, err := jobs.GetScoped(ctx, scope, actorID, jobID)
	if err != nil {
		return nil, err
	}
	return importProgressFromJob(j), nil
}

func importProgressFromJob(j *model.CustomerImportJob) *ImportProgress {
	p := &ImportProgress{
		JobID: j.JobID, Status: j.Status, TotalRows: j.TotalRows,
		SuccessRows: j.SuccessRows, SkippedRows: j.SkippedRows,
		FailedRows: j.FailedRows, UpdatedRows: j.UpdatedRows,
	}
	p.Cursor = j.SuccessRows + j.SkippedRows + j.FailedRows
	return p
}

func sameImportScope(a, b CustomerDataScope) bool {
	if a.AllAccounts != b.AllAccounts || a.UserID != b.UserID || a.OnlyOwned != b.OnlyOwned || a.OwnerUserID != b.OwnerUserID {
		return false
	}
	if len(a.AccountIDs) != len(b.AccountIDs) || len(a.Industries) != len(b.Industries) {
		return false
	}
	for i := range a.AccountIDs {
		if a.AccountIDs[i] != b.AccountIDs[i] {
			return false
		}
	}
	for i := range a.Industries {
		if a.Industries[i] != b.Industries[i] {
			return false
		}
	}
	return true
}

// ---------- CRM-11：导出筛选/字段白名单/xlsx ----------

// ExportFieldSpec 一个可导出字段的契约（白名单）。
type ExportFieldSpec struct {
	Field string `json:"field"`
	Label string `json:"label"`
}

// ExportQuery 与列表对齐的筛选条件 + 字段选择；来源账号/负责人由服务端范围收敛。
type ExportQuery struct {
	EntityType      string   `json:"entity_type"`
	Keyword         string   `json:"keyword"`
	Industries      []string `json:"industries"`
	SourceAccountID string   `json:"source_account_id"`
	OwnerUserID     uint     `json:"owner_user_id"`
	Fields          []string `json:"fields"`
}

// ExportContract 可导出实体类型及其白名单字段。
var ExportContract = map[string][]ExportFieldSpec{
	"customer": {
		{Field: "id", Label: "客户ID"}, {Field: "name", Label: "客户名称"},
		{Field: "phone", Label: "手机号"}, {Field: "email", Label: "邮箱"},
		{Field: "industry", Label: "行业"}, {Field: "source_account_id", Label: "来源账号"},
		{Field: "owner_user_id", Label: "负责人"}, {Field: "tags", Label: "标签"},
	},
	"contact": {
		{Field: "id", Label: "ID"}, {Field: "name", Label: "姓名"},
		{Field: "customer_id", Label: "客户ID"}, {Field: "phone", Label: "手机号"},
		{Field: "source_account_id", Label: "来源账号"},
	},
	"company": {
		{Field: "id", Label: "ID"}, {Field: "name", Label: "公司名称"},
		{Field: "industry", Label: "行业"}, {Field: "region", Label: "区域"},
		{Field: "owner_user_id", Label: "负责人"}, {Field: "source", Label: "来源"},
	},
	"supplier": {
		{Field: "id", Label: "ID"}, {Field: "name", Label: "名称"},
		{Field: "customer_id", Label: "客户ID"}, {Field: "source", Label: "来源"},
	},
	"service_provider": {
		{Field: "id", Label: "ID"}, {Field: "name", Label: "名称"},
		{Field: "customer_id", Label: "客户ID"}, {Field: "source", Label: "来源"},
	},
}

// NormalizeExportFields 过滤恶意/未知字段，只保留白名单；空表示全部默认字段。
func NormalizeExportFields(entityType string, requested []string) ([]string, error) {
	specs, ok := ExportContract[entityType]
	if !ok {
		return nil, fmt.Errorf("暂不支持的导出实体类型: %s", entityType)
	}
	allowed := make(map[string]bool, len(specs))
	for _, s := range specs {
		allowed[s.Field] = true
	}
	if len(requested) == 0 {
		out := make([]string, 0, len(specs))
		for _, s := range specs {
			out = append(out, s.Field)
		}
		return out, nil
	}
	out := make([]string, 0, len(requested))
	seen := make(map[string]bool)
	for _, f := range requested {
		f = strings.ToLower(strings.TrimSpace(f))
		if allowed[f] && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("没有可导出的合法字段")
	}
	return out, nil
}

// ToCustomerFilter 把 ExportQuery 叠加到权限范围，得到与列表同口径的筛选。
func (s *CustomerImportExportService) ToCustomerFilter(scope CustomerDataScope, q ExportQuery) (repository.CustomerScopeFilter, error) {
	filter := repository.CustomerScopeFilter{
		Keyword: q.Keyword, Industries: append([]string{}, q.Industries...),
		ExcludeMerged: true,
	}
	if q.SourceAccountID != "" {
		filter.SourceAccountIDs = []string{q.SourceAccountID}
	}
	if q.OwnerUserID > 0 {
		filter.OwnerUserID = q.OwnerUserID
	}
	return s.permission.ToFilter(scope, filter), nil
}

// ExportRowsXLSX 把导出结果按字段白名单写成真正的 xlsx（OOXML），并做服务端脱敏。
func ExportRowsXLSX(rows []CustomerExportRow, fields []string, masked bool) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err == nil {
			_, _ = w.Write([]byte(content))
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`)
	write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	write("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="export" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	write("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`)

	all := []string{"id", "name", "phone", "email", "industry", "source_account_id", "owner_user_id", "tags"}
	chosen := make([]string, 0, len(fields))
	for _, f := range fields {
		for _, a := range all {
			if f == a {
				chosen = append(chosen, f)
			}
		}
	}
	if len(chosen) == 0 {
		chosen = all
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	writeRow := func(r int, values []string) {
		sheet.WriteString(`<row r="` + strconv.Itoa(r) + `">`)
		for i, v := range values {
			ref := string(rune('A'+i)) + strconv.Itoa(r)
			sheet.WriteString(`<c r="` + ref + `" t="inlineStr"><is><t>` + xmlEscapeCRM(v) + `</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
	}
	writeRow(1, chosen)
	for i, row := range rows {
		values := make([]string, 0, len(chosen))
		for _, f := range chosen {
			v := ""
			switch f {
			case "id":
				v = row.ID
			case "name":
				v = row.Name
			case "phone":
				v = row.Phone
				if masked {
					v = maskPhone(v)
				}
			case "email":
				v = row.Email
				if masked {
					v = maskEmail(v)
				}
			case "industry":
				v = row.Industry
			case "source_account_id":
				v = row.SourceAccountID
			case "owner_user_id":
				v = row.OwnerUserID
			case "tags":
				v = row.Tags
			}
			values = append(values, v)
		}
		writeRow(i+2, values)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	write("xl/worksheets/sheet1.xml", sheet.String())
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("生成 xlsx 失败: %w", err)
	}
	return buf.Bytes(), nil
}

func xmlEscapeCRM(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// ExportByContract 按白名单筛选 + 服务端脱敏导出；返回字段顺序与 fields 一致。
func (s *CustomerImportExportService) ExportByContract(ctx context.Context, scope CustomerDataScope, q ExportQuery, operatorName string) (*CustomerExportResult, []string, error) {
	fields, err := NormalizeExportFields(q.EntityType, q.Fields)
	if err != nil {
		return nil, nil, err
	}
	filter, err := s.ToCustomerFilter(scope, q)
	if err != nil {
		return nil, nil, err
	}
	result, err := s.Export(ctx, scope, filter, operatorName)
	if err != nil {
		return nil, nil, err
	}
	return result, fields, nil
}

// ---------- CRM-12：大批量导出作业 + 受限下载 ----------

// ExportJobSnapshot 保存筛选/字段/脱敏策略/操作者，下载时重新验证当前权限。
type ExportJobSnapshot struct {
	JobID       string            `json:"job_id"`
	TenantID    uint              `json:"tenant_id"`
	Query       ExportQuery       `json:"query"`
	Fields      []string          `json:"fields"`
	Masked      bool              `json:"masked"`
	Scope       CustomerDataScope `json:"scope"` // 生成时的权限范围快照
	OperatorID  uint              `json:"operator_id"`
	Operator    string            `json:"operator"`
	Status      string            `json:"status"`
	Exported    int               `json:"exported"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	FileContent []byte            `json:"-"`
	Format      string            `json:"format"`
}

// ExportJobStore 导出作业的内存持久层（不新增 DB schema；进程重启后余项见 assessment）。
type ExportJobStore struct {
	jobs map[string]*ExportJobSnapshot
}

func NewExportJobStore() *ExportJobStore {
	return &ExportJobStore{jobs: make(map[string]*ExportJobSnapshot)}
}

func (st *ExportJobStore) Save(j *ExportJobSnapshot) { st.jobs[j.JobID] = j }
func (st *ExportJobStore) Get(jobID string) (*ExportJobSnapshot, bool) {
	j, ok := st.jobs[jobID]
	return j, ok
}

// CleanupExpired 清理过期导出文件（过期只删文件，不删作业状态）。
func (st *ExportJobStore) CleanupExpired(now time.Time) int {
	n := 0
	for id, j := range st.jobs {
		if !now.Before(j.ExpiresAt) {
			j.FileContent = nil
			j.Status = "expired"
			n++
			_ = id
		}
	}
	return n
}

var ErrExportJobForbidden = errors.New("无权访问该导出作业")
var ErrExportJobExpired = errors.New("导出文件已过期，请重新生成")

// CreateExportJob 按批流式生成导出文件，并保存策略快照与操作者。
func (s *CustomerImportExportService) CreateExportJob(ctx context.Context, scope CustomerDataScope, actorID uint, operatorName string, q ExportQuery, format string, store *ExportJobStore, pageSize int) (*ExportJobSnapshot, error) {
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return nil, ErrExportJobForbidden
	}
	fields, err := NormalizeExportFields(q.EntityType, q.Fields)
	if err != nil {
		return nil, err
	}
	filter, err := s.ToCustomerFilter(scope, q)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 500
	}
	masked := !scope.AllAccounts
	all := []CustomerExportRow{}
	filter.PageSize = pageSize
	for page := 1; ; page++ {
		filter.Page = page
		rows, total, err := s.scopeRepo.ListScoped(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("导出作业第 %d 页失败: %w", page, err)
		}
		for _, c := range rows {
			row := CustomerExportRow{ID: c.ID, Name: c.Name, Industry: c.Industry, SourceAccountID: c.SourceAccountID, Tags: c.Tags}
			if c.OwnerUserID != nil {
				row.OwnerUserID = strconv.FormatUint(uint64(*c.OwnerUserID), 10)
			}
			row.Phone = c.Phone
			row.Email = c.Email
			if masked {
				row.Phone = maskPhone(row.Phone)
				row.Email = maskEmail(row.Email)
			}
			all = append(all, row)
		}
		if len(rows) < pageSize || len(all) >= int(total) || len(rows) == 0 {
			break
		}
	}
	jobID := fmt.Sprintf("exp_%d", time.Now().UnixNano())
	var content []byte
	if format == "xlsx" {
		content, err = ExportRowsXLSX(all, fields, masked)
		if err != nil {
			return nil, fmt.Errorf("生成 XLSX 导出失败: %w", err)
		}
	} else {
		var sb strings.Builder
		sb.WriteString("\xEF\xBB\xBF")
		cr := csv.NewWriter(&sb)
		_ = cr.Write(fields)
		for _, row := range all {
			values := make([]string, 0, len(fields))
			for _, f := range fields {
				switch f {
				case "id":
					values = append(values, row.ID)
				case "name":
					values = append(values, row.Name)
				case "phone":
					values = append(values, row.Phone)
				case "email":
					values = append(values, row.Email)
				case "industry":
					values = append(values, row.Industry)
				case "source_account_id":
					values = append(values, row.SourceAccountID)
				case "owner_user_id":
					values = append(values, row.OwnerUserID)
				default:
					values = append(values, "")
				}
			}
			_ = cr.Write(values)
		}
		cr.Flush()
		content = []byte(sb.String())
		format = "csv"
	}
	job := &ExportJobSnapshot{
		JobID: jobID, TenantID: tenant.WriteTenantID(ctx), Query: q, Fields: fields,
		Masked: masked, Scope: scope, OperatorID: actorID, Operator: operatorName,
		Status: "completed", Exported: len(all), CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour), FileContent: content, Format: format,
	}
	if store != nil {
		store.Save(job)
	}
	s.writeAudit(ctx, "customer_export_job", operatorName, map[string]any{"job_id": jobID, "exported": len(all), "masked": masked, "format": format})
	return job, nil
}

// DownloadExportJob 重新解析当前权限；范围收窄/非创建者/跨租户/过期一律拒绝旧文件。
func (s *CustomerImportExportService) DownloadExportJob(ctx context.Context, scope CustomerDataScope, actorID uint, store *ExportJobStore, jobID string) (*ExportJobSnapshot, []byte, error) {
	if store == nil {
		return nil, nil, ErrExportJobForbidden
	}
	j, ok := store.Get(jobID)
	if !ok || j == nil {
		return nil, nil, ErrExportJobForbidden
	}
	if j.TenantID != tenant.WriteTenantID(ctx) {
		return nil, nil, ErrExportJobForbidden
	}
	if !scope.AllAccounts && j.OperatorID != actorID {
		return nil, nil, ErrExportJobForbidden
	}
	if !time.Now().Before(j.ExpiresAt) || len(j.FileContent) == 0 {
		return nil, nil, ErrExportJobExpired
	}
	// 重新验证当前范围是否仍覆盖快照：用当前权限构造筛选，比较账号/行业/负责人维度。
	filter, err := s.ToCustomerFilter(scope, j.Query)
	if err != nil {
		return nil, nil, err
	}
	if !scope.AllAccounts && (len(filter.SourceAccountIDs) == 0 || filter.DenyAll) {
		return nil, nil, ErrExportJobForbidden
	}
	// 范围收窄拒绝：当前账号/行业集合必须是生成时快照的超集。
	if !scope.AllAccounts {
		if j.Scope.AllAccounts && !scope.AllAccounts {
			return nil, nil, ErrExportJobForbidden
		}
		for _, acc := range j.Scope.AccountIDs {
			if !containsCRMString(scope.AccountIDs, acc) {
				return nil, nil, ErrExportJobForbidden
			}
		}
		for _, ind := range j.Scope.Industries {
			if !containsCRMString(scope.Industries, ind) {
				return nil, nil, ErrExportJobForbidden
			}
		}
	}
	return j, j.FileContent, nil
}
