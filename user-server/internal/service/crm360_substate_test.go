package service

import (
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// CRM-20：360 子列表的全量/当前页分开、截断标识、依赖失败显式不可用。
func TestCRM360SubState_PaginationAndFailure(t *testing.T) {
	st := crm360SubState(2, 50, 120, nil)
	if st.Total != 120 || st.Page != 2 || st.PageSize != 50 || !st.Truncated || !st.Available {
		t.Fatalf("全量与当前页必须分开且标记截断: %+v", st)
	}
	last := crm360SubState(3, 50, 120, nil)
	if last.Truncated {
		t.Fatalf("末页不应标记截断: %+v", last)
	}
	failed := crm360SubState(1, 50, 0, errors.New("db down"))
	if failed.Available || failed.Error == "" {
		t.Fatalf("依赖失败必须标不可用并保留原因: %+v", failed)
	}
}

// CRM-20：来源引用必须带账号作用域，缺失/越权有明确状态，不能伪装成"有来源"。
func TestCRM360SourceState_ScopeAndMissing(t *testing.T) {
	allow := func(CustomerDataScope, *model.Customer) bool { return true }
	deny := func(CustomerDataScope, *model.Customer) bool { return false }

	ok := crm360SourceState(CustomerDataScope{}, &model.Customer{
		SourceAccountID: "acc-1", SourceChannel: "wechat",
		SourceConversationID: "conv-1", SourceMessageID: "m-1",
	}, allow)
	if ok.Status != "available" || ok.AccountID != "acc-1" || ok.MessageID != "m-1" {
		t.Fatalf("有账号+消息的来源应可用: %+v", ok)
	}

	missing := crm360SourceState(CustomerDataScope{}, &model.Customer{SourceAccountID: "acc-1"}, allow)
	if missing.Status != "missing" {
		t.Fatalf("无消息标识的来源必须标缺失: %+v", missing)
	}

	out := crm360SourceState(CustomerDataScope{}, &model.Customer{
		SourceAccountID: "acc-2", SourceMessageID: "m-2",
	}, deny)
	if out.Status != "out_of_scope" || out.MessageID != "m-2" {
		t.Fatalf("越权来源必须标 out_of_scope: %+v", out)
	}
}
