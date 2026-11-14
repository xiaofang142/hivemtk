package service

// 自回声判据边界回归钉（架构师二审①，2026-11-14）。
//
// 背景：charCoverage 频率差集修复（b896358f）后二审发现残留误拦面——
// 「引用追问的追问字符恰好全部 ⊆ 出站字符集」会被频率闸 stray==0 拦掉；
// 以及修复放行与 tail 判据的**执行顺序**风险（放行若先于 tail，线程拼接
// 以我方发言收尾的形态会被误放行触发重复回复）。三个用例钉住边界。
//
// 运行前提：真库（testutil），TENANT_MODE 无关（ingress 判据不依赖租户模式）。
// 独立成文件：原 recent_echo 测试文件被外部编辑器 buffer 还原污染，新文件隔离。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 二审①钉1：引用追问的追问字符**全部 ⊆ 出站字符集**（stray==0 形态）必须放行。
// 出站「这个款式还有货」客户引用后问「还有货吗」——有/货/吗 全在出站内，
// 频率闸 stray==0 会拦；剥离层终结放行（先于 multiset/频率闸）必须生效。
func TestEchoBoundary_QuoteQuestionAllCharsSubset_NotBlocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "echo-bd-1"
		conv     = "conv-echo-bd-1"
	)
	outContent := "这个款式还有货"
	if err := db.Create(&model.MessageHub{
		MsgID: "mh:ob-bd-1", Platform: platform, AccountID: account,
		Direction: "outbound", Status: "delivered", MsgType: "text",
		ConversationID: conv, SenderID: account, Content: outContent,
		SentAt: time.Now().Add(-20 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel: platform, SenderID: conv, SenderName: "抖音客户", SenderType: "customer",
		Content: outContent + " 还有货吗", EventID: "mh:bd-subset-1",
		ConversationID: conv, Extra: map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked && decision.IsSelfEcho {
		t.Fatalf("追问字符全在出站字符集内的引用追问必须放行, got: %+v", decision)
	}
}

// 二审①钉2：线程拼接以我方发言**收尾**（客户旧话+我方话）必须拦截——
// 剥离层会剩下客户旧话 ≥4 字，引用追问放行必须让位于 tail 判据（tail 在前）。
func TestEchoBoundary_ThreadSelfTail_Blocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "echo-bd-2"
		conv     = "conv-echo-bd-2"
	)
	outContent := "好的，这边已经帮您登记好了"
	if err := db.Create(&model.MessageHub{
		MsgID: "mh:ob-bd-2", Platform: platform, AccountID: account,
		Direction: "outbound", Status: "delivered", MsgType: "text",
		ConversationID: conv, SenderID: account, Content: outContent,
		SentAt: time.Now().Add(-30 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	customerMsg := "什么时候能发货" + "\n" + outContent
	event := &model.MessageEvent{
		Channel: platform, SenderID: conv, SenderName: "抖音客户", SenderType: "customer",
		Content: customerMsg, EventID: "mh:bd-thread-1",
		ConversationID: conv, Extra: map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision == nil || !decision.Blocked || !decision.IsSelfEcho {
		t.Fatalf("以我方发言收尾的线程拼接必须拦截（tail 判据先于引用追问放行）, got: %+v", decision)
	}
}

// 二审①钉3：AI 拆多气泡发送、客户把两条气泡全文引用再加追问——剥离循环必须
// 把每条出站都剥净（不能把另一条出站原文误当「客户新话」提前放行/误拦）。
func TestEchoBoundary_MultiBubbleQuoteWithQuestion_NotBlocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "echo-bd-3"
		conv     = "conv-echo-bd-3"
	)
	outA := "全包的话大概每平一千二到一千五"
	outB := "具体要量房之后才能给您出准确报价单"
	for i, c := range []string{outA, outB} {
		if err := db.Create(&model.MessageHub{
			MsgID: fmt.Sprintf("mh:ob-bd-3-%d", i), Platform: platform, AccountID: account,
			Direction: "outbound", Status: "delivered", MsgType: "text",
			ConversationID: conv, SenderID: account, Content: c,
			SentAt: time.Now().Add(-30 * time.Second),
		}).Error; err != nil {
			t.Fatalf("插入 outbound %d 失败: %v", i, err)
		}
	}

	customerMsg := outA + "\n" + outB + "\n那半包呢？能便宜多少？"
	event := &model.MessageEvent{
		Channel: platform, SenderID: conv, SenderName: "抖音客户", SenderType: "customer",
		Content: customerMsg, EventID: "mh:bd-multi-1",
		ConversationID: conv, Extra: map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked && decision.IsSelfEcho {
		t.Fatalf("多气泡全引用+追问必须放行, got: %+v", decision)
	}
}
