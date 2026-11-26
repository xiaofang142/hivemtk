package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

type crmDictReaderStub struct {
	items map[string][]model.Dictionary
	err   error
}

func (s *crmDictReaderStub) GetDictionary(_ context.Context, parent string) ([]model.Dictionary, []model.DictionaryTransition, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.items[parent], nil, nil
}

// CRM-19：三类业务字典有真实种子（含明确停用项），不是只有纯函数契约。
func TestCRMBusinessDictionary_SeedsRegistered(t *testing.T) {
	for _, parent := range []string{CRMDictIndustry, CRMDictBusinessType, CRMDictRequirementCategory} {
		items, _, ok := lookupDictionarySeed(parent)
		if !ok || len(items) == 0 {
			t.Fatalf("字典域 %s 没有真实种子", parent)
		}
		enabled, disabled := 0, 0
		for _, it := range items {
			if it.Enabled {
				enabled++
			} else {
				disabled++
			}
		}
		if enabled == 0 {
			t.Fatalf("字典域 %s 无启用项", parent)
		}
		if parent == CRMDictIndustry && disabled == 0 {
			t.Fatalf("行业字典应有明确停用项供导入预检拒绝，实际 0")
		}
	}
}

func TestCRMBusinessDictionary_DisabledRejectedUnknownKept(t *testing.T) {
	reader := &crmDictReaderStub{items: map[string][]model.Dictionary{
		CRMDictIndustry: {{Code: "good", Enabled: true}, {Code: "old", Enabled: false}},
	}}
	disabled, err := CRMDisabledDictCodes(context.Background(), reader, CRMDictIndustry)
	if err != nil {
		t.Fatalf("读取停用码失败: %v", err)
	}
	if !disabled["old"] || disabled["good"] {
		t.Fatalf("停用码识别错误: %+v", disabled)
	}
	if err := CRMValidateDictValue("行业", "old", disabled); err == nil {
		t.Fatal("停用码必须被拒绝")
	}
	// 历史自由文本不在字典里：兼容保留，不静默丢弃、也不误拒。
	if err := CRMValidateDictValue("行业", "legacy-free-text", disabled); err != nil {
		t.Fatalf("历史自由文本应保留: %v", err)
	}
	if err := CRMValidateRowDicts(CustomerImportRow{Industry: "old"}, disabled, nil, nil); err == nil {
		t.Fatal("导入行的行业命中停用码必须被拒")
	}
}

// 字典读失败必须显式暴露，不能回退成「无停用项」放行。
func TestCRMBusinessDictionary_ReadFailureFailsClosed(t *testing.T) {
	reader := &crmDictReaderStub{err: errors.New("db down")}
	if _, err := CRMDisabledDictCodes(context.Background(), reader, CRMDictIndustry); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("字典读失败必须显式返回，实际 err=%v", err)
	}
}
