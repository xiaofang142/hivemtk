package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
)

// CRM-19：行业/业务类型/客户需求分类字典的读取与校验接缝。
//
// 字典条目走既有 DictionaryService（DB 优先，DB 无该域则回落种子），
// 不新建平行字典表。语义与 CRMBusinessDictionary 契约一致：
//   - 命中明确 Enabled=false 的码：新写入/导入预检拒绝；
//   - 字典里没有的历史自由文本：兼容保留，不静默丢弃；
//   - 域没有配置任何项：跳过校验（未配置字典的站点行为与既有自由输入一致）。

const (
	CRMDictIndustry            = "crm_industry"
	CRMDictBusinessType        = "crm_business_type"
	CRMDictRequirementCategory = "crm_requirement_category"
)

// crmBusinessDictReader 只依赖 DictionaryService.GetDictionary，便于测试替身注入。
type crmBusinessDictReader interface {
	GetDictionary(ctx context.Context, parent string) ([]model.Dictionary, []model.DictionaryTransition, error)
}

// CRMDisabledDictCodes 取一个 CRM 字典域里被明确停用的码集合。
// 读取失败时返回错误，调用方须显式处理，不能把读失败当成「没有停用项」放行。
func CRMDisabledDictCodes(ctx context.Context, reader crmBusinessDictReader, parent string) (map[string]bool, error) {
	if reader == nil {
		return nil, errors.New("业务字典服务未装配")
	}
	items, _, err := reader.GetDictionary(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("读取业务字典 %s 失败: %w", parent, err)
	}
	disabled := make(map[string]bool)
	seen := make(map[string]bool)
	for _, it := range items {
		if it.Code == "" || seen[it.Code] {
			continue
		}
		seen[it.Code] = true
		if !it.Enabled {
			disabled[it.Code] = true
		}
	}
	return disabled, nil
}

// CRMValidateDictValue 校验单个字段值：只有明确停用的码才拒绝。
// 未知值（历史自由文本）保留，避免把存量数据一次性判为非法。
func CRMValidateDictValue(field, code string, disabled map[string]bool) error {
	if disabled[code] {
		return fmt.Errorf("%s 使用了已停用的字典值 %q", field, code)
	}
	return nil
}

// CRMValidateRowDicts 校验导入行中走字典的三个受控字段。
func CRMValidateRowDicts(row CustomerImportRow, industryDisabled, businessTypeDisabled, requirementDisabled map[string]bool) error {
	if err := CRMValidateDictValue("行业", row.Industry, industryDisabled); err != nil {
		return err
	}
	if err := CRMValidateDictValue("业务类型", row.BusinessType, businessTypeDisabled); err != nil {
		return err
	}
	return CRMValidateDictValue("需求分类", row.RequirementCategory, requirementDisabled)
}
