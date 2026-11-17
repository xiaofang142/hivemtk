package monitor

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/tracing"

	"github.com/gin-gonic/gin"
)

func TestObservation_GapDoesNotJoinDifferentDeliveryAttempts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		msg     string
		attempt int
		wantGap bool
	}{
		{name: "同一尝试可测", msg: "out-one", attempt: 1, wantGap: true},
		{name: "重领不拼等待", msg: "out-one", attempt: 2},
		{name: "另一条答复不拼等待", msg: "out-two", attempt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, ctx, _ := monitorTenantDB(t)
			at := time.Now().Add(-time.Minute)
			end := at.Add(time.Millisecond)
			next := end.Add(time.Second)
			nextEnd := next.Add(time.Millisecond)
			rows := []model.MessageTrace{
				{TraceID: "gap-trace", TurnID: "gap-turn", Channel: "douyin", AccountID: "gap-account", ConversationID: "gap-conv", MsgID: "out-one", Node: tracing.NodeDownlinkFetch, NodeOrder: 5, AnswerAttempt: 1, DeliveryAttempt: 1, DurationMeasured: true, TimingVersion: 1, StartedAt: &at, CompletedAt: &end},
				{TraceID: "gap-trace", TurnID: "gap-turn", Channel: "douyin", AccountID: "gap-account", ConversationID: "gap-conv", MsgID: tc.msg, Node: tracing.NodeDeliveredAck, NodeOrder: 6, AnswerAttempt: 1, DeliveryAttempt: tc.attempt, DurationMeasured: true, TimingVersion: 1, StartedAt: &next, CompletedAt: &nextEnd},
			}
			if err := database.WithContext(ctx).Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
			got, err := Lifecycle(ctx, "gap-conv", "gap-trace", 10)
			if err != nil || len(got) != 1 || len(got[0].Nodes) != 2 {
				t.Fatalf("测试轮次不可见：%v", err)
			}
			gap := got[0].Nodes[1].GapMs
			if (gap != nil) != tc.wantGap {
				t.Fatal("等待时间不得跨消息或跨投递尝试拼接")
			}
			if gap != nil && *gap != 1000 {
				t.Fatal("同尝试的真实操作间隙必须准确")
			}
		})
	}
}

func TestObservation_NodeHealthExcludesMissingMeasurement(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	ctx := tenant.WithTenantID(context.Background(), monTenantA)
	rows := []model.MessageTrace{
		{TenantID: monTenantA, Channel: "douyin", Node: tracing.NodeAIDispatch, Status: "ok", DurationMs: 100, DurationMeasured: true, TimingVersion: 1},
		{TenantID: monTenantA, Channel: "douyin", Node: tracing.NodeAIDispatch, Status: "ok", DurationMs: 0, DurationMeasured: true, TimingVersion: 1},
		{TenantID: monTenantA, Channel: "douyin", Node: tracing.NodeAIDispatch, Status: "ok", DurationMs: 0},
	}
	if err := database.WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got, err := NodeHealthByChannel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AvgDurationMs == nil || *got[0].AvgDurationMs != 50 || got[0].Total != 3 {
		t.Fatal("未测0不得进入均值；实测0应保留")
	}
	data, _ := json.Marshal(got[0])
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	if fields["measured_count"] != float64(2) || fields["measurement_coverage"] != float64(2)/3 {
		t.Fatal("统计必须报告实测覆盖率")
	}
}

func TestObservation_LifecyclePairsOnlySameMeasuredTurn(t *testing.T) {
	database, ctxA, ctxB := monitorTenantDB(t)
	start := time.Now().Add(-time.Minute)
	add := func(ctx context.Context, turn, account, node, status, msg string, at time.Time, measured bool, output string) {
		t.Helper()
		end := at.Add(time.Millisecond)
		row := model.MessageTrace{TraceID: "shared-session-trace", TurnID: turn, AccountID: account, ConversationID: "shared-conv", Channel: "douyin", Node: node, NodeOrder: tracing.NodeOrder(node), Status: status, MsgID: msg, StartedAt: &at, CompletedAt: &end, DurationMeasured: measured, TimingVersion: 1, Output: output}
		if err := database.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		turn            string
		offset, latency time.Duration
	}{{"turn-one", 0, time.Second}, {"turn-two", 10 * time.Second, 2 * time.Second}} {
		add(ctxA, tc.turn, "account", tracing.NodeIngest, "ok", "in-"+tc.turn, start.Add(tc.offset), true, "")
		add(ctxA, tc.turn, "account", tracing.NodeDeliveredAck, "ok", "out-"+tc.turn, start.Add(tc.offset+tc.latency-time.Millisecond), true, `{"status":"delivered","transition_applied":true}`)
		add(ctxA, tc.turn, "account", tracing.NodeDeliveredAck, "skipped", "out-"+tc.turn, start.Add(tc.offset+30*time.Second), true, `{"status":"delivered","transition_applied":false}`)
	}
	add(ctxB, "turn-one", "account", tracing.NodeDeliveredAck, "ok", "foreign", start.Add(45*time.Second), true, `{"status":"delivered","transition_applied":true}`)
	add(ctxA, "turn-one", "other-account", tracing.NodeDeliveredAck, "ok", "foreign-account", start.Add(40*time.Second), true, `{"status":"delivered","transition_applied":true}`)
	add(ctxA, "unknown-turn", "account", tracing.NodeIngest, "ok", "old", start, false, "")
	add(ctxA, "unknown-turn", "account", tracing.NodeDeliveredAck, "ok", "old-out", start.Add(35*time.Second), false, `{"status":"delivered","transition_applied":true}`)
	add(ctxA, "turn-one", "account", tracing.NodeDeliveredAck, "abnormal", "unknown", start.Add(50*time.Second), true, `{"status":"unknown","transition_applied":true}`)
	got, err := LifecycleLatencyByChannel(ctxA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SampleSize != 2 || got[0].P50Ms == nil || *got[0].P50Ms != 2000 || got[0].P95Ms == nil || *got[0].P95Ms != 2000 {
		t.Fatalf("两轮只能有两个有效样本，不能跨租户、范围、重复或缺测配对：%+v", got)
	}
	lifecycles, err := Lifecycle(ctxA, "shared-conv", "shared-session-trace", 10)
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, lc := range lifecycles {
		if lc.EndToEndMs != nil {
			valid++
			if *lc.EndToEndMs > 2000 {
				t.Fatal("重复ACK或跨范围不能改写轮次耗时")
			}
		}
	}
	if valid != 2 {
		t.Fatal("会话级trace必须分轮显示，缺测保留unknown")
	}
	tree, err := TraceTree(ctxA, "", "shared-conv", "in-turn-two")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Spans) != 3 {
		t.Fatal("消息下钻只能展示该消息同范围的一轮，不能把整会话所有轮次拼入")
	}
	for _, row := range tree.Spans {
		if row.TurnID != "turn-two" || row.AccountID != "account" {
			t.Fatal("消息下钻不得串轮或串账号")
		}
	}
	summaries, err := Traces(ctxA, 20)
	if err != nil {
		t.Fatal(err)
	}
	validSummaries := 0
	for _, summary := range summaries {
		if summary.ConversationID == "shared-conv" && summary.Channel == "douyin" && (summary.NodeCount == 3 || summary.NodeCount == 4) {
			validSummaries++
		}
	}
	if validSummaries != 2 {
		t.Fatal("总览也必须按范围与轮次分组，不能仍以共享会话trace合并")
	}
	// 较新记录属于turn-two；指定turn-one时接口不能悄悄忽略筛选。
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("GET", "/api/monitor/trace-tree?trace_id=shared-session-trace&conversation_id=shared-conv&turn_id=turn-one&account_id=account&channel=douyin", nil).WithContext(ctxA)
	traceTreeHandler(c)
	var envelope struct {
		Data TraceTreeData `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(envelope.Data.Spans) != 4 {
		t.Fatal("显式指定轮次必须只返回该轮，包含其异常/重复记录供诊断")
	}
	for _, row := range envelope.Data.Spans {
		if row.TurnID != "turn-one" || row.AccountID != "account" {
			t.Fatal("查询参数不能被忽略而跳到另一个账号或轮次")
		}
	}
}
