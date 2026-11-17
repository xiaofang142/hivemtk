package tracing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestObservation_TemporaryInboundRetainsOnlyMatchingParentTrace(t *testing.T) {
	parent := &Carrier{TenantID: 18, TraceID: "confirmed-parent-trace", Channel: "douyin", AccountID: "trace-account", ConversationID: "trace-conversation", MsgID: "trace-inbound", Direction: "inbound", AnswerAttempt: 2}
	ctx := WithCarrier(tenant.WithTenantID(context.Background(), 18), parent)
	for _, tc := range []struct {
		name   string
		change func(*model.MessageHub)
		want   string
	}{
		{name: "同消息临时入站继承", want: parent.TraceID},
		{name: "持久追踪优先", change: func(h *model.MessageHub) { h.TraceID = "persisted-trace" }, want: "persisted-trace"},
		{name: "租户不同", change: func(h *model.MessageHub) { h.TenantID = 19 }},
		{name: "渠道不同", change: func(h *model.MessageHub) { h.Platform = "xiaohongshu" }},
		{name: "账号不同", change: func(h *model.MessageHub) { h.AccountID = "other-account" }},
		{name: "会话不同", change: func(h *model.MessageHub) { h.ConversationID = "other-conversation" }},
		{name: "消息不同", change: func(h *model.MessageHub) { h.MsgID = "other-inbound" }},
		{name: "消息缺失", change: func(h *model.MessageHub) { h.MsgID = "" }},
		{name: "缺账号", change: func(h *model.MessageHub) { h.AccountID = "" }},
		{name: "不是入站", change: func(h *model.MessageHub) { h.Direction = "outbound" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &model.MessageHub{TenantID: 18, Platform: parent.Channel, AccountID: parent.AccountID, ConversationID: parent.ConversationID, MsgID: parent.MsgID, Direction: "inbound"}
			if tc.change != nil {
				tc.change(h)
			}
			if got := CarrierForHub(ctx, h).TraceID; got != tc.want {
				t.Fatalf("追踪只能继承同一权威入站身份：实际 %q，期望 %q", got, tc.want)
			}
			if parent.TraceID != "confirmed-parent-trace" {
				t.Fatal("子载体不得修改父追踪")
			}
		})
	}
}

func TestObservation_ClaimBatchReadIsBoundedAndTenantSafe(t *testing.T) {
	for _, name := range []string{"批次去重至多一次读取", "错误租户不记录"} {
		t.Run(name, func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.MessageTrace{})
			ctx := tenant.WithTenantID(context.Background(), 18)
			claimed := time.Now().Add(-time.Second)
			hubs := []*model.MessageHub{}
			for i := 0; i < 3; i++ {
				hubs = append(hubs, &model.MessageHub{ID: uint(i + 1), TenantID: 18, Platform: "douyin", AccountID: "batch-account", ConversationID: "batch-conv", MsgID: string(rune('a' + i)), Direction: "outbound", PushAttempts: 1, ClaimedAt: &claimed})
			}
			if name == "错误租户不记录" {
				hubs[2].TenantID = 19
			}
			reads := 0
			callback := "observation-test-query-count"
			if err := database.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { reads++ }); err != nil {
				t.Fatal(err)
			}
			if err := database.Callback().Row().Before("gorm:row").Register(callback, func(*gorm.DB) { reads++ }); err != nil {
				t.Fatal(err)
			}
			RecordDownlinkFetchBatch(ctx, "douyin", "batch-account", hubs)
			if err := database.Callback().Query().Remove(callback); err != nil {
				t.Fatal(err)
			}
			if err := database.Callback().Row().Remove(callback); err != nil {
				t.Fatal(err)
			}
			if name == "批次去重至多一次读取" && reads != 1 {
				t.Fatalf("观测不能为每条消息同步查库增加下发延迟，读取=%d", reads)
			}
			var rows []model.MessageTrace
			if err := database.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if name == "错误租户不记录" && len(rows) != 2 {
				t.Fatal("外租户认领不得冒名写入当前租户的观测")
			}
		})
	}
}

func TestObservation_SpanDoesNotMutateParent(t *testing.T) {
	parent := NewCarrier("douyin", "observation-account", "observation-conversation").WithMsgID("inbound-1")
	originalID := parent.TraceID
	span := Start(WithCarrier(context.Background(), parent), NodeAIDispatch).TraceID("child-trace")
	if parent.TraceID != originalID {
		t.Fatal("子span覆盖追踪身份不得修改父上下文")
	}
	if span.toPending(0, StatusOk, "").msgID != "inbound-1" {
		t.Fatal("未显式覆盖时应继承权威消息身份")
	}
}

func TestObservation_OperationTimeAndMeasuredZero(t *testing.T) {
	span := Start(context.Background(), NodeAIDispatch)
	span.start = time.Date(2026, 11, 15, 23, 34, 0, 0, time.UTC)
	row := toModelFromPending(span.toPending(0, StatusOk, ""))
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["duration_measured"] != true || fields["timing_version"] != float64(1) {
		t.Fatal("真实0ms必须明确标记实测，不能等同未测")
	}
	if fields["started_at"] != span.start.Format(time.RFC3339Nano) || fields["completed_at"] == nil {
		t.Fatal("操作时间必须在投递sink之前确定，不依赖批次落库")
	}
	unknownData, _ := json.Marshal(toModelFromPending(pendingSpan{node: NodeInboxSync}))
	var unknown map[string]any
	_ = json.Unmarshal(unknownData, &unknown)
	if unknown["duration_measured"] != false || unknown["started_at"] != nil {
		t.Fatal("兼容旧调用缺测时必须保持unknown")
	}
}

func TestObservation_BusinessTurnIsScoped(t *testing.T) {
	base := BusinessTurnID(18, "douyin", "account", "conversation", "inbound")
	if base == "" || base != BusinessTurnID(18, "douyin", "account", "conversation", "inbound") {
		t.Fatal("完整同作用域身份必须稳定")
	}
	for name, value := range map[string]string{
		"租户": BusinessTurnID(19, "douyin", "account", "conversation", "inbound"),
		"渠道": BusinessTurnID(18, "xiaohongshu", "account", "conversation", "inbound"),
		"账号": BusinessTurnID(18, "douyin", "other", "conversation", "inbound"),
		"会话": BusinessTurnID(18, "douyin", "account", "other", "inbound"),
		"轮次": BusinessTurnID(18, "douyin", "account", "conversation", "other"),
	} {
		t.Run(name, func(t *testing.T) {
			if value == base {
				t.Fatal("不同作用域不得共享turn")
			}
		})
	}
	for _, value := range []string{
		BusinessTurnID(0, "douyin", "account", "conversation", "inbound"),
		BusinessTurnID(18, "", "account", "conversation", "inbound"),
		BusinessTurnID(18, "douyin", "", "conversation", "inbound"),
		BusinessTurnID(18, "douyin", "account", "", "inbound"),
		BusinessTurnID(18, "douyin", "account", "conversation", ""),
	} {
		if value != "" {
			t.Fatal("缺范围必须unknown，不得猜测")
		}
	}
}

func TestObservation_TracePayloadRemovesCredentials(t *testing.T) {
	row := toModelFromPending(pendingSpan{
		input: map[string]any{
			"content": "合法评估正文",
			"headers": map[string]any{"Authorization": "Bearer private-value", "X-Bridge-Token": "bridge-private"},
			"nested":  []any{map[string]any{"password": "secret-password", "merchant_key": "merchant-private", "completion_tokens": 12}},
		},
		output: `{"reply":"合法评估答案","access_token":"output-private"}`,
	})
	combined := row.Input + row.Output
	for _, secret := range []string{"private-value", "bridge-private", "secret-password", "merchant-private", "output-private"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("追踪载荷不得持久化凭证：%s", secret)
		}
	}
	if !strings.Contains(row.Input, "合法评估正文") || !strings.Contains(row.Output, "合法评估答案") || !strings.Contains(row.Input, "completion_tokens") {
		t.Fatal("不得误删授权评估正文或token数量元数据")
	}
}

func TestObservation_OversizedPayloadStaysValidJSON(t *testing.T) {
	row := toModelFromPending(pendingSpan{input: map[string]any{"blob": strings.Repeat("中", 10000)}})
	if len(row.Input) > 8192 || !json.Valid([]byte(row.Input)) {
		t.Fatal("巨大JSON必须有界且保持合法，不能截断成破损JSON")
	}
	if !strings.Contains(row.Input, "sha256") || !strings.Contains(row.Input, "truncated") {
		t.Fatal("巨大载荷需要不可还原的摘要及缺失标记")
	}
}

func TestObservation_CredentialPluralAndNestedJSON(t *testing.T) {
	row := toModelFromPending(pendingSpan{input: map[string]any{
		"access_tokens":      []any{"plural-private"},
		"secret_token_count": "fake-count-private",
		"prompt_tokens":      12,
		"completion_tokens":  "not-numeric-private",
		"encoded":            `{"accessToken":"encoded-private","reply":"合成授权答案"}`,
	}})
	for _, secret := range []string{"plural-private", "fake-count-private", "not-numeric-private", "encoded-private"} {
		if strings.Contains(row.Input, secret) {
			t.Fatal("凭证不能借复数、伪数量或嵌套JSON绕过脱敏")
		}
	}
	if !strings.Contains(row.Input, `"prompt_tokens":12`) || !strings.Contains(row.Input, "合成授权答案") {
		t.Fatal("合法数值元数据及授权答案应保留")
	}
}

func TestObservation_CarrierRejectsCrossTenantAndMissingScope(t *testing.T) {
	parent := &Carrier{Channel: "douyin", AccountID: "same-account", ConversationID: "same-conv", TurnID: BusinessTurnID(18, "douyin", "same-account", "same-conv", "in"), AnswerAttempt: 2}
	ctx := WithCarrier(tenant.WithTenantID(context.Background(), 18), parent)
	other := &model.MessageHub{TenantID: 19, Platform: "douyin", AccountID: "same-account", ConversationID: "same-conv", Direction: "outbound"}
	if c := CarrierForHub(ctx, other); c.TurnID != "" || c.AnswerAttempt != 0 {
		t.Fatal("不同租户的同名会话不能借用父轮次")
	}
	missing := &model.MessageHub{TenantID: 18, Platform: "douyin", Direction: "outbound", Extra: model.JSONMap{"turn_id": parent.TurnID, "answer_attempt": 2}}
	if c := CarrierForHub(ctx, missing); c.TurnID != "" || c.AnswerAttempt != 0 {
		t.Fatal("缺失账号或会话必须保留未知轮次")
	}
}

func TestObservation_DownlinkRecordsEachClaimInScope(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	ctx := tenant.WithTenantID(context.Background(), 18)
	claimed := time.Now().Add(-time.Second)
	hub := &model.MessageHub{ID: 123, TenantID: 18, Platform: "douyin", AccountID: "fetch-account", ConversationID: "fetch-conv", MsgID: "same-fetch-id", Direction: "outbound", PushAttempts: 1, ClaimedAt: &claimed, TraceID: "fetch-trace", Extra: model.JSONMap{"turn_id": BusinessTurnID(18, "douyin", "fetch-account", "fetch-conv", "in"), "answer_attempt": 1}}
	RecordDownlinkFetchBatch(ctx, hub.Platform, hub.AccountID, []*model.MessageHub{hub})
	RecordDownlinkFetchBatch(ctx, hub.Platform, hub.AccountID, []*model.MessageHub{hub})
	hub.PushAttempts = 2
	next := claimed.Add(time.Second)
	hub.ClaimedAt = &next
	RecordDownlinkFetchBatch(ctx, hub.Platform, hub.AccountID, []*model.MessageHub{hub})
	other := *hub
	other.AccountID = "other-account"
	other.ConversationID = "other-conv"
	other.PushAttempts = 1
	RecordDownlinkFetchBatch(ctx, other.Platform, other.AccountID, []*model.MessageHub{&other})
	var rows []model.MessageTrace
	if err := database.Where("node = ?", NodeDownlinkFetch).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].DeliveryAttempt != 1 || rows[1].DeliveryAttempt != 2 || rows[0].TurnID == "" || rows[2].AccountID != other.AccountID {
		t.Fatal("每次有效认领及不同范围都要可见，同次重复记录只保留一条")
	}
}
