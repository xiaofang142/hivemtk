package service

import "testing"

// CRM-19：业务字典校验——停用项不可用于新写入，历史值保留展示并归一，不静默丢弃。
func TestCRMExtension_DictionaryValidation(t *testing.T) {
	d := CRMBusinessDictionary{{Code: "A", Enabled: true}, {Code: "B", Enabled: false}}
	if !d.AllowsNew("A") || d.AllowsNew("B") {
		t.Fatal("新写入只允许启用字典项")
	}
	if !d.HasHistorical("B") {
		t.Fatal("停用项历史值必须保留可展示")
	}
}

// CRM-20/21：账号视图来源契约与跨账号检索——全量分页与当前页分开，来源带账号作用域。
func TestCRMExtension_SourceAndPagination(t *testing.T) {
	p := CRMSearchPage{Page: 2, PageSize: 50, Total: 120, Items: 50}
	if p.TotalAll() != 120 || p.TotalOnPage() != 50 {
		t.Fatalf("全量与当前页必须分开标注，实际 %d/%d", p.TotalAll(), p.TotalOnPage())
	}
	src := CRMSourceRef{AccountID: "acct-1", MessageID: "m-1"}
	if !src.Valid() || src.ScopeKey() != "acct-1:m-1" {
		t.Fatalf("来源必须带账号作用域，实际 %+v", src)
	}
}

// CRM-22/23：员工助手受限只读工具 + 行动草稿——默认只读、外发走审批，不自动联系客户。
func TestCRMExtension_AssistantToolGuard(t *testing.T) {
	if (CRMAssistantToolGuard{Name: "query_customers", ReadOnly: true}).Allowed() != true {
		t.Fatal("只读查询工具应允许")
	}
	if (CRMAssistantToolGuard{Name: "send_message", ReadOnly: false}).Allowed() != false {
		t.Fatal("外发工具默认必须拒绝，走审批")
	}
	d := CRMActionDraft{Approved: false}
	if d.Executable() {
		t.Fatal("未审批的行动草稿不得执行")
	}
}
