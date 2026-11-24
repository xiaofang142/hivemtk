package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type CustomerImportJobService struct {
	repo          repository.CustomerImportJobRepository
	custRepo      repository.CustomerRepository
	readCSVFileFn func(path string) ([][]string, error) // nil 用生产实现；测试注入故障出口
}

func NewCustomerImportJobService() *CustomerImportJobService {
	return &CustomerImportJobService{
		repo:     repository.NewCustomerImportJobRepository(),
		custRepo: repository.NewCustomerRepository(),
	}
}

type ImportTemplateColumn struct {
	Field       string `json:"field"`
	Label       string `json:"label"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type ImportTemplate struct {
	EntityType string                 `json:"entity_type"`
	Columns    []ImportTemplateColumn `json:"columns"`
	SampleRow  map[string]string      `json:"sample_row"`
}

type ImportPrecheckRowError struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type ImportPrecheckReport struct {
	JobID         string                   `json:"job_id"`
	EntityType    string                   `json:"entity_type"`
	FileName      string                   `json:"file_name"`
	TotalRows     int                      `json:"total_rows"`
	ValidRows     int                      `json:"valid_rows"`
	InvalidRows   int                      `json:"invalid_rows"`
	DuplicateRows int                      `json:"duplicate_rows"`
	DedupStrategy string                   `json:"dedup_strategy"`
	Mapping       map[string]string        `json:"mapping"`
	Errors        []ImportPrecheckRowError `json:"errors,omitempty"`
	GeneratedAt   time.Time                `json:"generated_at"`
}

type ImportExecuteReport struct {
	JobID         string                   `json:"job_id"`
	Status        string                   `json:"status"`
	TotalRows     int                      `json:"total_rows"`
	SuccessRows   int                      `json:"success_rows"`
	FailedRows    int                      `json:"failed_rows"`
	SkippedRows   int                      `json:"skipped_rows"`
	UpdatedRows   int                      `json:"updated_rows"`
	DedupStrategy string                   `json:"dedup_strategy"`
	Errors        []ImportPrecheckRowError `json:"errors,omitempty"`
	CompletedAt   time.Time                `json:"completed_at"`
}

var customerImportColumns = []ImportTemplateColumn{
	{Field: "name", Label: "客户名称", Required: false},
	{Field: "phone", Label: "手机号", Required: false, Description: "手机号/邮箱/微信/抖音/小红书至少一个"},
	{Field: "email", Label: "邮箱", Required: false},
	{Field: "wechat_open_id", Label: "微信OpenID", Required: false},
	{Field: "douyin_open_id", Label: "抖音OpenID", Required: false},
	{Field: "xiaohongshu_id", Label: "小红书ID", Required: false},
	{Field: "whatsapp_phone", Label: "WhatsApp", Required: false},
	{Field: "telegram_chat_id", Label: "Telegram ChatID", Required: false},
	{Field: "tags", Label: "标签(逗号分隔)", Required: false},
	{Field: "source_channel", Label: "来源渠道", Required: false},
	{Field: "source_account_id", Label: "来源账号ID", Required: false},
	{Field: "company_id", Label: "公司ID", Required: false},
}

func (s *CustomerImportJobService) Create(ctx context.Context, j *model.CustomerImportJob) error {
	if j.JobID == "" {
		j.JobID = fmt.Sprintf("imp_%d", time.Now().UnixNano())
	}
	if j.Status == "" {
		j.Status = "pending"
	}
	j.DedupStrategy = normalizeDedupStrategy(j.DedupStrategy)
	return s.repo.Create(ctx, j)
}

func (s *CustomerImportJobService) Get(ctx context.Context, jobID string) (*model.CustomerImportJob, error) {
	return s.repo.GetByJobID(ctx, jobID)
}

func (s *CustomerImportJobService) List(ctx context.Context, page, pageSize int) ([]model.CustomerImportJob, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

func (s *CustomerImportJobService) UpdateStatus(ctx context.Context, jobID, status string, total, success, failed, skipped, updated int, errReport string) error {
	return s.repo.UpdateStatus(ctx, jobID, status, total, success, failed, skipped, updated, errReport)
}

func (s *CustomerImportJobService) Template(entityType string) (*ImportTemplate, error) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	if entityType == "" {
		entityType = "customer"
	}
	switch entityType {
	case "customer":
		return &ImportTemplate{
			EntityType: "customer",
			Columns:    customerImportColumns,
			SampleRow: map[string]string{
				"name": "张三", "phone": "13800138000", "email": "zhangsan@example.com",
				"wechat_open_id": "", "douyin_open_id": "", "xiaohongshu_id": "",
				"whatsapp_phone": "", "telegram_chat_id": "", "tags": "跨境,重点客户",
				"source_channel": "wechat", "source_account_id": "acc_001", "company_id": "",
			},
		}, nil
	default:
		return nil, fmt.Errorf("暂不支持的导入实体类型: %s", entityType)
	}
}

func (s *CustomerImportJobService) TemplateCSV(entityType string) ([]byte, string, error) {
	tpl, err := s.Template(entityType)
	if err != nil {
		return nil, "", err
	}
	headers := make([]string, 0, len(tpl.Columns))
	for _, c := range tpl.Columns {
		headers = append(headers, c.Field)
	}
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF") // UTF-8 BOM for Excel
	w := csv.NewWriter(&b)
	_ = w.Write(headers)
	sample := make([]string, 0, len(headers))
	for _, h := range headers {
		sample = append(sample, tpl.SampleRow[h])
	}
	_ = w.Write(sample)
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", err
	}
	return []byte(b.String()), fmt.Sprintf("%s_import_template.csv", tpl.EntityType), nil
}

func normalizeDedupStrategy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "update":
		return "update"
	case "error":
		return "error"
	case "skip", "":
		return "skip"
	default:
		return "skip"
	}
}

func (s *CustomerImportJobService) Precheck(ctx context.Context, fileName string, reader io.Reader, entityType, dedupStrategy, mappingJSON string) (*ImportPrecheckReport, error) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	if entityType == "" {
		entityType = "customer"
	}
	if entityType != "customer" {
		return nil, fmt.Errorf("暂不支持的导入实体类型: %s", entityType)
	}
	dedupStrategy = normalizeDedupStrategy(dedupStrategy)
	mapping, err := parseMapping(mappingJSON)
	if err != nil {
		return nil, err
	}

	rows, err := readCSV(reader)
	if err != nil {
		return nil, fmt.Errorf("解析CSV失败: %w", err)
	}
	if len(rows) < 1 {
		return nil, errors.New("CSV文件为空或缺少表头")
	}
	headers := normalizeHeaders(rows[0])
	headerIndex := make(map[string]int, len(headers))
	for i, h := range headers {
		headerIndex[h] = i
	}

	jobID := fmt.Sprintf("imp_%d", time.Now().UnixNano())
	filePath, err := s.saveImportFile(jobID, fileName, rows)
	if err != nil {
		return nil, err
	}

	report := &ImportPrecheckReport{
		JobID:         jobID,
		EntityType:    entityType,
		FileName:      fileName,
		DedupStrategy: dedupStrategy,
		Mapping:       mapping,
		GeneratedAt:   time.Now(),
	}

	seenKeys := map[string]int{}
	for i, row := range rows[1:] {
		rowNum := i + 2 // 1-based, header is row 1
		if isEmptyRow(row) {
			continue
		}
		report.TotalRows++
		rec := mapRow(headers, headerIndex, mapping, row)
		rowErrs := validateCustomerRow(rec)
		if len(rowErrs) > 0 {
			report.InvalidRows++
			for _, msg := range rowErrs {
				report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: msg})
			}
			continue
		}
		dedupKey := customerDedupKey(rec)
		if dedupKey != "" {
			if prev, ok := seenKeys[dedupKey]; ok {
				report.DuplicateRows++
				report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: fmt.Sprintf("与第%d行重复(%s)", prev, dedupKey)})
				if dedupStrategy == "error" {
					report.InvalidRows++
				} else {
					report.ValidRows++
				}
				continue
			}
			seenKeys[dedupKey] = rowNum
		}
		exists, err := s.customerExists(ctx, rec)
		if err != nil {
			report.InvalidRows++
			report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "重复检查失败: " + err.Error()})
			continue
		}
		if exists {
			report.DuplicateRows++
			report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "与系统已有客户重复"})
			if dedupStrategy == "error" {
				report.InvalidRows++
				continue
			}
		}
		report.ValidRows++
	}

	precheckJSON, _ := json.Marshal(report)
	job := &model.CustomerImportJob{
		TenantID:       tenant.WriteTenantID(ctx),
		JobID:          jobID,
		EntityType:     entityType,
		FileName:       fileName,
		FilePath:       filePath,
		Status:         "prechecked",
		TotalRows:      report.TotalRows,
		SuccessRows:    0,
		FailedRows:     report.InvalidRows,
		DedupStrategy:  dedupStrategy,
		MappingJSON:    cjMustJSON(mapping),
		PrecheckReport: string(precheckJSON),
		ErrorReport:    cjMustJSON(report.Errors),
	}
	if err := s.repo.Create(ctx, job); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *CustomerImportJobService) Execute(ctx context.Context, jobID string) (*ImportExecuteReport, error) {
	job, err := s.repo.GetByJobID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == "running" {
		return nil, errors.New("导入任务正在执行中")
	}
	if job.Status == "completed" {
		return nil, errors.New("导入任务已完成，请勿重复执行")
	}
	if job.FilePath == "" {
		return nil, errors.New("导入任务缺少文件路径，请重新预检")
	}

	job.Status = "running"
	job.ErrorReport = "[]"
	if err := s.repo.Update(ctx, job); err != nil {
		// 权威状态写失败即中止：否则作业停在旧状态，用户既拿不到结果也因旧状态无法重试。
		return nil, fmt.Errorf("标记导入任务执行中失败: %w", err)
	}

	readCSV := readCSVFile
	if s.readCSVFileFn != nil {
		readCSV = s.readCSVFileFn
	}
	rows, err := readCSV(job.FilePath)
	if err != nil {
		if statusErr := s.repo.UpdateStatus(ctx, jobID, "failed", job.TotalRows, 0, job.TotalRows, 0, 0, cjMustJSON([]ImportPrecheckRowError{{Row: 0, Message: "读取导入文件失败: " + err.Error()}})); statusErr != nil {
			logger.Errorf("[ImportJob] job=%s 读取失败后标记 failed 失败: %v（原始错误: %v）", jobID, statusErr, err)
		}
		return nil, fmt.Errorf("读取导入文件失败: %w", err)
	}
	if len(rows) < 1 {
		if statusErr := s.repo.UpdateStatus(ctx, jobID, "failed", 0, 0, 0, 0, 0, cjMustJSON([]ImportPrecheckRowError{{Row: 0, Message: "CSV文件为空"}})); statusErr != nil {
			logger.Errorf("[ImportJob] job=%s 空 CSV 后标记 failed 失败: %v", jobID, statusErr)
		}
		return nil, errors.New("CSV文件为空")
	}
	headers := normalizeHeaders(rows[0])
	headerIndex := make(map[string]int, len(headers))
	for i, h := range headers {
		headerIndex[h] = i
	}
	mapping, _ := parseMapping(job.MappingJSON)
	strategy := normalizeDedupStrategy(job.DedupStrategy)

	report := &ImportExecuteReport{
		JobID: jobID, Status: "running", DedupStrategy: strategy,
	}

	for i, row := range rows[1:] {
		rowNum := i + 2
		if isEmptyRow(row) {
			continue
		}
		report.TotalRows++
		rec := mapRow(headers, headerIndex, mapping, row)
		rowErrs := validateCustomerRow(rec)
		if len(rowErrs) > 0 {
			report.FailedRows++
			for _, msg := range rowErrs {
				report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: msg})
			}
			continue
		}
		existing, err := s.findExistingCustomer(ctx, rec)
		if err != nil {
			report.FailedRows++
			report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "重复检查失败: " + err.Error()})
			continue
		}
		if existing != nil {
			switch strategy {
			case "skip":
				report.SkippedRows++
				report.SuccessRows++
				continue
			case "error":
				report.FailedRows++
				report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "与系统已有客户重复"})
				continue
			case "update":
				applyCustomerRecord(existing, rec)
				if err := s.custRepo.Update(ctx, existing); err != nil {
					report.FailedRows++
					report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "更新客户失败: " + err.Error()})
					continue
				}
				report.UpdatedRows++
				report.SuccessRows++
				continue
			}
		}
		customer := buildCustomerFromRecord(rec, tenant.WriteTenantID(ctx))
		if err := s.custRepo.Create(ctx, customer); err != nil {
			report.FailedRows++
			report.Errors = append(report.Errors, ImportPrecheckRowError{Row: rowNum, Message: "创建客户失败: " + err.Error()})
			continue
		}
		report.SuccessRows++
	}

	report.Status = "completed"
	if report.FailedRows > 0 && report.SuccessRows == 0 {
		report.Status = "failed"
	}
	report.CompletedAt = time.Now()
	errJSON := cjMustJSON(report.Errors)
	if err := s.repo.UpdateStatus(ctx, jobID, report.Status, report.TotalRows, report.SuccessRows, report.FailedRows, report.SkippedRows, report.UpdatedRows, errJSON); err != nil {
		// 终态写失败不能伪装成成功：report 仍回给调用方参考，但错误必须显式返回。
		logger.Errorf("[ImportJob] job=%s 写入执行结果失败: %v", jobID, err)
		return report, fmt.Errorf("写入导入结果失败: %w", err)
	}
	return report, nil
}

func (s *CustomerImportJobService) ErrorReport(ctx context.Context, jobID string) ([]ImportPrecheckRowError, error) {
	job, err := s.repo.GetByJobID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(job.ErrorReport) == "" {
		return []ImportPrecheckRowError{}, nil
	}
	var errs []ImportPrecheckRowError
	if err := json.Unmarshal([]byte(job.ErrorReport), &errs); err != nil {
		// 兼容旧纯文本
		return []ImportPrecheckRowError{{Row: 0, Message: job.ErrorReport}}, nil
	}
	return errs, nil
}

func (s *CustomerImportJobService) ErrorReportCSV(ctx context.Context, jobID string) ([]byte, string, error) {
	errs, err := s.ErrorReport(ctx, jobID)
	if err != nil {
		return nil, "", err
	}
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"row", "field", "message"})
	for _, e := range errs {
		_ = w.Write([]string{fmt.Sprintf("%d", e.Row), e.Field, e.Message})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", err
	}
	filename := fmt.Sprintf("customer_import_%s_errors.csv", strings.ReplaceAll(jobID, "/", "_"))
	return []byte(b.String()), filename, nil
}

func (s *CustomerImportJobService) saveImportFile(jobID, fileName string, rows [][]string) (string, error) {
	dir := os.Getenv("IMPORT_JOB_DIR")
	if dir == "" {
		dir = filepath.Join(".", "data", "customer_imports")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(fileName)
	if base == "" || base == "." {
		base = "import.csv"
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%s", jobID, base))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return path, nil
}

func readCSV(r io.Reader) ([][]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 10<<20)) // 10MB MVP limit
	if err != nil {
		return nil, err
	}
	// strip UTF-8 BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	cr := csv.NewReader(strings.NewReader(string(data)))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}

func readCSVFile(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readCSV(f)
}

func normalizeHeaders(headers []string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		h = strings.TrimSpace(strings.TrimPrefix(h, "\xEF\xBB\xBF"))
		h = strings.ToLower(h)
		h = strings.ReplaceAll(h, " ", "_")
		out[i] = h
	}
	return out
}

func parseMapping(mappingJSON string) (map[string]string, error) {
	mapping := map[string]string{}
	mappingJSON = strings.TrimSpace(mappingJSON)
	if mappingJSON == "" || mappingJSON == "{}" {
		return mapping, nil
	}
	if err := json.Unmarshal([]byte(mappingJSON), &mapping); err != nil {
		return nil, fmt.Errorf("mapping_json 格式错误: %w", err)
	}
	out := make(map[string]string, len(mapping))
	for k, v := range mapping {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out, nil
}

func mapRow(headers []string, headerIndex map[string]int, mapping map[string]string, row []string) map[string]string {
	rec := map[string]string{}
	get := func(name string) string {
		idx, ok := headerIndex[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}
	for _, col := range customerImportColumns {
		field := col.Field
		// mapping: source header -> target field
		src := field
		for k, v := range mapping {
			if v == field {
				src = k
				break
			}
		}
		val := get(src)
		if val == "" {
			val = get(field)
		}
		rec[field] = val
	}
	// also include any mapped extra fields directly present
	for src, target := range mapping {
		if _, ok := rec[target]; ok && rec[target] != "" {
			continue
		}
		if v := get(src); v != "" {
			rec[target] = v
		}
	}
	_ = headerIndex
	return rec
}

func isEmptyRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func validateCustomerRow(rec map[string]string) []string {
	var errs []string
	hasID := rec["phone"] != "" || rec["email"] != "" || rec["wechat_open_id"] != "" || rec["douyin_open_id"] != "" || rec["xiaohongshu_id"] != "" || rec["whatsapp_phone"] != "" || rec["telegram_chat_id"] != ""
	if !hasID {
		errs = append(errs, "至少需要一个身份标识(phone/email/wechat_open_id/douyin_open_id/xiaohongshu_id/whatsapp_phone/telegram_chat_id)")
	}
	if rec["email"] != "" && !strings.Contains(rec["email"], "@") {
		errs = append(errs, "邮箱格式不合法")
	}
	if rec["company_id"] != "" {
		if _, err := cjParseInt64(rec["company_id"]); err != nil {
			errs = append(errs, "company_id 必须是整数")
		}
	}
	if rec["telegram_chat_id"] != "" {
		if _, err := cjParseInt64(rec["telegram_chat_id"]); err != nil {
			errs = append(errs, "telegram_chat_id 必须是整数")
		}
	}
	return errs
}

func customerDedupKey(rec map[string]string) string {
	switch {
	case rec["phone"] != "":
		return "phone:" + rec["phone"]
	case rec["email"] != "":
		return "email:" + strings.ToLower(rec["email"])
	case rec["wechat_open_id"] != "":
		return "wechat:" + rec["wechat_open_id"]
	case rec["douyin_open_id"] != "":
		return "douyin:" + rec["douyin_open_id"]
	case rec["xiaohongshu_id"] != "":
		return "xhs:" + rec["xiaohongshu_id"]
	case rec["whatsapp_phone"] != "":
		return "wa:" + rec["whatsapp_phone"]
	case rec["telegram_chat_id"] != "":
		return "tg:" + rec["telegram_chat_id"]
	default:
		return ""
	}
}

func (s *CustomerImportJobService) customerExists(ctx context.Context, rec map[string]string) (bool, error) {
	existing, err := s.findExistingCustomer(ctx, rec)
	if err != nil {
		return false, err
	}
	return existing != nil, nil
}

func (s *CustomerImportJobService) findExistingCustomer(ctx context.Context, rec map[string]string) (*model.Customer, error) {
	// 优先用强标识精确查
	if rec["phone"] != "" {
		if c, err := s.custRepo.GetByPhone(ctx, rec["phone"]); err == nil && c != nil {
			return c, nil
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if rec["email"] != "" {
		if c, err := s.custRepo.GetByEmail(ctx, rec["email"]); err == nil && c != nil {
			return c, nil
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if rec["wechat_open_id"] != "" {
		if c, err := s.custRepo.GetByWechatOpenID(ctx, rec["wechat_open_id"]); err == nil && c != nil {
			return c, nil
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if rec["douyin_open_id"] != "" {
		if c, err := s.custRepo.GetByDouyinOpenID(ctx, rec["douyin_open_id"]); err == nil && c != nil {
			return c, nil
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if rec["xiaohongshu_id"] != "" {
		if c, err := s.custRepo.GetByXiaohongshuID(ctx, rec["xiaohongshu_id"]); err == nil && c != nil {
			return c, nil
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

func buildCustomerFromRecord(rec map[string]string, tenantID uint) *model.Customer {
	c := &model.Customer{TenantID: tenantID}
	applyCustomerRecord(c, rec)
	return c
}

func applyCustomerRecord(c *model.Customer, rec map[string]string) {
	if v := rec["name"]; v != "" {
		c.Name = v
	}
	if v := rec["phone"]; v != "" {
		c.Phone = v
	}
	if v := rec["email"]; v != "" {
		c.Email = v
	}
	if v := rec["wechat_open_id"]; v != "" {
		c.WechatOpenID = v
	}
	if v := rec["douyin_open_id"]; v != "" {
		c.DouyinOpenID = v
	}
	if v := rec["xiaohongshu_id"]; v != "" {
		c.XiaohongshuID = v
	}
	if v := rec["whatsapp_phone"]; v != "" {
		c.WhatsAppPhone = v
	}
	if v := rec["telegram_chat_id"]; v != "" {
		if n, err := cjParseInt64(v); err == nil {
			c.TelegramChatID = n
		}
	}
	if v := rec["tags"]; v != "" {
		c.Tags = v
	}
	if v := rec["source_channel"]; v != "" {
		c.SourceChannel = v
	}
	if v := rec["source_account_id"]; v != "" {
		c.SourceAccountID = v
	}
	if v := rec["company_id"]; v != "" {
		if n, err := cjParseInt64(v); err == nil {
			c.CompanyID = &n
		}
	}
	if c.FirstTouchAt == nil {
		now := time.Now()
		c.FirstTouchAt = &now
	}
	now := time.Now()
	c.LastTouchAt = &now
}

func cjParseInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func cjMustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
