package service

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestExcludeSystemHistoryNotices 系统通知不进模型上下文。
//
// transferToHuman 会往 session_messages 写「【系统】AI 连续回复已达上限 (10 次)，转人工跟进」
// 这类坐席可见通知；历史注入把非 ai/agent 一律映射成 user 角色，模型于是把内部通知
// 当客户话头接着推理，甚至把推理文本原样回给客户（message_hub id=432 实测外发）。
func TestExcludeSystemHistoryNotices(t *testing.T) {
	in := []model.SessionMessage{
		{SenderType: "customer", Content: "多少钱"},
		{SenderType: "system", Content: "【系统】AI 连续回复已达上限 (10 次)，转人工跟进"},
		{SenderType: "ai", Content: "给您算个优惠价"},
		{SenderType: "customer", Content: "再便宜点"},
	}
	got := excludeSystemHistoryNotices(in)
	if len(got) != 3 {
		t.Fatalf("应剔除 1 条系统通知，剩 3 条，实得 %d: %+v", len(got), got)
	}
	for _, m := range got {
		if m.SenderType == "system" {
			t.Fatalf("系统通知仍在模型上下文里: %q", m.Content)
		}
	}
	if got[0].Content != "多少钱" || got[1].Content != "给您算个优惠价" || got[2].Content != "再便宜点" {
		t.Fatalf("剔除后顺序被打乱: %+v", got)
	}
}

// TestFetchHistoryWithinTokenBudget_ExcludesSystemNotice 预算注入路径同样过滤，
// 且系统通知不再占用 token 预算。
func TestFetchHistoryWithinTokenBudget_ExcludesSystemNotice(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	e := &SalesEngine{sessionMsgRepo: repository.NewSessionMessageRepositoryWithDB(db)}

	seq := []model.SessionMessage{
		{SessionID: "s-sys", SenderType: "customer", Content: "在吗"},
		{SessionID: "s-sys", SenderType: "system", Content: "【系统】AI 连续回复已达上限 (10 次)，转人工跟进"},
		{SessionID: "s-sys", SenderType: "ai", Content: "在的，有什么可以帮您"},
		{SessionID: "s-sys", SenderType: "customer", Content: "想了解价格"},
	}
	for i := range seq {
		if err := db.Create(&seq[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got := e.fetchHistoryWithinTokenBudget("s-sys", "想了解价格")
	if len(got) == 0 {
		t.Fatal("历史不应为空")
	}
	for _, m := range got {
		if m.SenderType == "system" {
			t.Fatalf("系统通知泄漏进模型历史: %q", m.Content)
		}
	}
}
