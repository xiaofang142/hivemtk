package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
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
	Tags            string
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
	if scope.AllAccounts {
		return true
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) > 0 && !containsCRMString(scope.AccountIDs, row.SourceAccountID) {
		return false
	}
	if len(scope.Industries) > 0 && !containsCRMString(scope.Industries, row.Industry) {
		return false
	}
	if scope.OnlyOwned && row.OwnerUserID > 0 && row.OwnerUserID != scope.UserID {
		return false
	}
	return true
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
