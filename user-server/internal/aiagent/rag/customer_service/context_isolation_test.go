package ragcustomerservice

import (
	"context"
	"testing"
)

// UpdateContext 返回的是值拷贝，若沿用调用方的 map 底层存储，会话历史会在无人赋值的情况下被改写。
func TestUpdateContextDoesNotMutateCallerEntities(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	const content = "订单号 9527 什么时候发货"
	intent, err := svc.AnalyzeIntent(ctx, content, nil)
	if err != nil {
		t.Fatalf("AnalyzeIntent: %v", err)
	}
	if len(intent.Parameters) == 0 {
		t.Fatalf("夹具前置不成立：intent 未带出任何参数，本用例将空跑, intent=%+v", intent)
	}

	caller := Context{
		Topic:    "order_inquiry",
		Entities: map[string][]string{"existing": {"keep-me"}},
	}
	if _, err := svc.UpdateContext(ctx, caller, Message{Content: content}, intent); err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}

	if len(caller.Entities) != 1 || caller.Entities["existing"][0] != "keep-me" {
		t.Errorf("调用方的 Entities 被写穿: %v", caller.Entities)
	}
}

func TestUpdateContextDoesNotSharePreviousTopicsBackingArray(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	caller := Context{
		Topic:          "product_inquiry",
		PreviousTopics: make([]string, 0, 4),
	}
	if cap(caller.PreviousTopics) < 2 {
		t.Fatalf("夹具前置不成立：底层数组需留富余容量才测得出共用, cap=%d", cap(caller.PreviousTopics))
	}

	msg := Message{Content: "你好"} // greeting 与 product_inquiry 跨域 → 应记一条历史话题
	intent, err := svc.AnalyzeIntent(ctx, msg.Content, nil)
	if err != nil {
		t.Fatalf("AnalyzeIntent: %v", err)
	}
	updated, err := svc.UpdateContext(ctx, caller, msg, intent)
	if err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}
	if len(updated.PreviousTopics) != 1 {
		t.Fatalf("夹具前置不成立：本轮未记录历史话题, prev=%v", updated.PreviousTopics)
	}

	// 共用底层数组时，调用方自己往后追加一格就会盖掉服务已返回的那条历史。
	caller.PreviousTopics = append(caller.PreviousTopics, "caller-side")
	if len(updated.PreviousTopics) != 1 || updated.PreviousTopics[0] != "product_inquiry" {
		t.Errorf("调用方随后的追加改写了服务返回的历史（两者共用底层数组）: %v", updated.PreviousTopics)
	}
}
