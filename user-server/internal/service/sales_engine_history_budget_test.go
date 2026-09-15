package service

import (
	"strings"
	"testing"

	textutil "hivemtk-user/internal/pkg/utils/text"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/stretchr/testify/require"
)

// TestFetchHistoryWithinTokenBudget_AllKept_SmallHistory 短历史全保留
func TestFetchHistoryWithinTokenBudget_AllKept_SmallHistory(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	e := &SalesEngine{sessionMsgRepo: repository.NewSessionMessageRepositoryWithDB(db)}

	msgs := []model.SessionMessage{
		{SessionID: "s1", SenderType: "customer", Content: "你好"},
		{SessionID: "s1", SenderType: "ai", Content: "您好，请问有什么可以帮您？"},
		{SessionID: "s1", SenderType: "customer", Content: "介绍一下产品"},
	}
	for i := range msgs {
		if err := db.Create(&msgs[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got := e.fetchHistoryWithinTokenBudget("s1", "这条不在历史里")
	if len(got) != 3 {
		t.Fatalf("small history should be fully kept, got %d", len(got))
	}
	if got[0].Content != "你好" || got[2].Content != "介绍一下产品" {
		t.Fatalf("history should be in chronological order, got [%s, %s, %s]",
			got[0].Content, got[1].Content, got[2].Content)
	}
}

// TestFetchHistoryWithinTokenBudget_CurrentMsgExcluded 最新一条为当前消息时剔除
func TestFetchHistoryWithinTokenBudget_CurrentMsgExcluded(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	e := &SalesEngine{sessionMsgRepo: repository.NewSessionMessageRepositoryWithDB(db)}

	cur := model.SessionMessage{SessionID: "s2", SenderType: "customer", Content: "当前消息"}
	prev := model.SessionMessage{SessionID: "s2", SenderType: "ai", Content: "上一条回复"}
	for _, m := range []model.SessionMessage{prev, cur} {
		mm := m
		if err := db.Create(&mm).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got := e.fetchHistoryWithinTokenBudget("s2", "当前消息")
	if len(got) != 1 || got[0].Content != "上一条回复" {
		t.Fatalf("current message should be excluded, got %+v", got)
	}
}

// TestFetchHistoryWithinTokenBudget_BudgetRespected 长历史按预算截断且不超上限
func TestFetchHistoryWithinTokenBudget_BudgetRespected(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	e := &SalesEngine{sessionMsgRepo: repository.NewSessionMessageRepositoryWithDB(db)}

	long := strings.Repeat("销", 400)
	for i := 0; i < 30; i++ {
		st := "customer"
		if i%2 == 1 {
			st = "ai"
		}
		m := model.SessionMessage{SessionID: "s3", SenderType: st, Content: long}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got := e.fetchHistoryWithinTokenBudget("s3", "")
	budget := agentLoopHistoryTokenBudget * (100 - agentLoopHistoryOutputReservePct) / 100
	total := 0
	for _, m := range got {
		total += estimateHistoryTokens(m.Content)
	}
	if total > budget {
		t.Fatalf("kept history tokens %d exceed budget %d", total, budget)
	}
	if len(got) >= 20 {
		t.Fatalf("budget truncation should keep fewer than the old fixed-20, got %d", len(got))
	}

	if got[0].SenderType == "ai" || got[0].SenderType == "agent" {
		t.Fatalf("oldest kept message must start a pair (customer), got sender=%s", got[0].SenderType)
	}

	if got[len(got)-1].Content != long {
		t.Fatal("newest message must always be kept")
	}
}

// TestFetchHistoryWithinTokenBudget_PairIntegrity 截断边界的孤儿 AI 回复被丢弃
func TestFetchHistoryWithinTokenBudget_PairIntegrity(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SessionMessage{})
	e := &SalesEngine{sessionMsgRepo: repository.NewSessionMessageRepositoryWithDB(db)}

	long := strings.Repeat("测", 1600)
	seq := []string{"customer", "ai", "customer", "ai", "customer", "ai"}
	for _, sender := range seq {
		m := model.SessionMessage{SessionID: "s4", SenderType: sender, Content: long}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got := e.fetchHistoryWithinTokenBudget("s4", "")
	require.Len(t, got, 2, "孤儿 AI 回reply应被丢弃，仅保留一对")
	if got[0].SenderType != "customer" {
		t.Fatalf("oldest kept must start a pair (customer), got %s", got[0].SenderType)
	}
	if got[0].Content != long || got[1].Content != long {
		t.Fatal("kept pair should be the newest C3/A3 messages")
	}
}

func estimateHistoryTokens(content string) int {
	return textutil.EstimateTokens(content) + historyMsgTokenOverhead
}
