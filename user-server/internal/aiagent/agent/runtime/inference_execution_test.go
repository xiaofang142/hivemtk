package agent_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
)

type inferenceExecutionSpy struct{ calls int }

func (s *inferenceExecutionSpy) Execute(context.Context, string, map[string]any) (ToolOutput, error) {
	s.calls++
	return ToolOutput{Success: true, Data: "订单正在运输中"}, nil
}

type executionGateFunc func(context.Context, ExecutionRequest) (ExecutionAuthorization, error)

func (f executionGateFunc) Authorize(ctx context.Context, r ExecutionRequest) (ExecutionAuthorization, error) {
	return f(ctx, r)
}

type executionToolFunc func(context.Context, string, map[string]any) (ToolOutput, error)

func (f executionToolFunc) Execute(ctx context.Context, name string, args map[string]any) (ToolOutput, error) {
	return f(ctx, name, args)
}

type executionGeneratorFunc func(context.Context, CustomerMessagePayload, *AgentContext, *ActionPlan, []ToolExecutionResult) (string, error)

func (f executionGeneratorFunc) GenerateReply(ctx context.Context, p CustomerMessagePayload, a *AgentContext, plan *ActionPlan, results []ToolExecutionResult) (string, error) {
	return f(ctx, p, a, plan, results)
}

type executionReviewFunc struct {
	*DefaultReviewer
	review func(context.Context, *InferenceContext) (*ReviewResult, error)
}

func (r executionReviewFunc) Review(ctx context.Context, ic *InferenceContext) (*ReviewResult, error) {
	return r.review(ctx, ic)
}

// 所有端口均为可计数的受控信号，复核实际将发送的裁剪稿。
func executionFixture(t *testing.T, tools int) (*CoreDataFlowOrchestrator, *inferenceExecutionPublisher, *[]string) {
	t.Helper()
	events := []string{}
	var stages []string
	cycle := outcomeCycle("", false, nil, &stages)
	cycle.PlannerStage = outcomeStage{"planner", func(_ context.Context, ic *InferenceContext) StageResult {
		ic.Plan = &ActionPlan{PlanType: "tools", ReplyHint: "内部规划指令，不可发布", Confidence: 1}
		for i := 0; i < tools; i++ {
			ic.Plan.ToolCalls = append(ic.Plan.ToolCalls, PlannedToolCall{ToolName: fmt.Sprintf("order.lookup.%d", i), Args: map[string]any{"customer_id": "checkpoint"}})
		}
		return ContinueResult()
	}}
	orch := NewCoreDataFlowOrchestrator(cycle, nil)
	pub := &inferenceExecutionPublisher{}
	orch.SetDeliveryGateway(newExecutionDeliveryFixture(func(ctx context.Context, i delivery.Intent) error {
		return pub.Publish(ctx, i.Channel, i.CustomerID, i.Content)
	}))
	orch.SetExecutionGate(executionGateFunc(func(_ context.Context, r ExecutionRequest) (ExecutionAuthorization, error) {
		if r.AgentContext == nil || r.Payload.CustomerID == "" || r.Plan == nil {
			t.Fatal("授权缺失身份或计划")
		}
		name := "reply"
		if r.ToolCall != nil {
			name = r.ToolCall.ToolName
		}
		events = append(events, "authorize:"+name)
		return ExecutionApproved, nil
	}))
	orch.SetToolRouter(executionToolFunc(func(_ context.Context, name string, args map[string]any) (ToolOutput, error) {
		events = append(events, "tool:"+name)
		if args["customer_id"] != "checkpoint" {
			t.Fatal("工具参数未传递")
		}
		return ToolOutput{Success: true, Data: "运输中:" + name}, nil
	}))
	orch.SetReplyGenerator(executionGeneratorFunc(func(_ context.Context, _ CustomerMessagePayload, _ *AgentContext, _ *ActionPlan, results []ToolExecutionResult) (string, error) {
		events = append(events, "generate")
		if len(results) != tools {
			t.Fatalf("生成器未收到实际工具结果：%+v", results)
		}
		for _, r := range results {
			if !r.Success || !strings.Contains(r.Data, "运输中:") {
				t.Fatalf("生成器收到错误结果：%+v", r)
			}
		}
		return "订单正在运输中```json\n{\"debug\":true}\n```", nil
	}))
	orch.SetFinalReviewer(executionReviewFunc{review: func(_ context.Context, ic *InferenceContext) (*ReviewResult, error) {
		events = append(events, "review:"+ic.Decision.Reply)
		if ic.Decision.Reply != "订单正在运输中" {
			t.Fatalf("未复核实际裁剪后的正文：%q", ic.Decision.Reply)
		}
		return &ReviewResult{Passed: true}, nil
	}})
	return orch, pub, &events
}

func TestInferenceExecutionUsesActualResultsBeforePublish(t *testing.T) {
	for _, tools := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(tools), func(t *testing.T) {
			orch, pub, events := executionFixture(t, tools)
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err != nil || result.Outcome != InferenceSucceeded || result.ExecutionStatus != ExecutionSucceeded || !result.Executed || !result.Publishable || result.ToolCallCount != tools || result.PlannedToolCallCount != tools || len(result.ToolResults) != tools || !reflect.DeepEqual(pub.replies, []string{"订单正在运输中"}) {
				t.Fatalf("实际执行契约错误：%+v pub=%v err=%v", result, pub.replies, err)
			}
			want := []string{}
			if tools == 0 {
				want = append(want, "authorize:reply")
			}
			for i := 0; i < tools; i++ {
				want = append(want, fmt.Sprintf("authorize:order.lookup.%d", i))
			}
			for i := 0; i < tools; i++ {
				want = append(want, fmt.Sprintf("tool:order.lookup.%d", i))
			}
			want = append(want, "generate", "review:订单正在运输中")
			if !reflect.DeepEqual(*events, want) || result.FinalReply != "订单正在运输中" || orch.GetStats().ToolCallCount != int64(tools) {
				t.Fatalf("授权/执行/复核顺序错误：%v want=%v result=%+v", *events, want, result)
			}
		})
	}
}

func TestInferenceExecutionAuthorizationPreflightsAllTools(t *testing.T) {
	for _, state := range []ExecutionAuthorization{ExecutionApprovalPending, ExecutionDenied, "unknown"} {
		t.Run(string(state), func(t *testing.T) {
			orch, pub, events := executionFixture(t, 2)
			checks := 0
			orch.SetExecutionGate(executionGateFunc(func(_ context.Context, r ExecutionRequest) (ExecutionAuthorization, error) {
				checks++
				if r.ToolCall.ToolName == "order.lookup.1" {
					return state, nil
				}
				return ExecutionApproved, nil
			}))
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err != nil || result.Executed || result.ToolCallCount != 0 || len(pub.replies) != 0 || len(*events) != 0 || checks != 2 || result.FinalReply != "" || result.Publishable {
				t.Fatalf("审批前抢跑：%+v events=%v err=%v", result, *events, err)
			}
			if state == ExecutionApprovalPending {
				if result.Outcome != InferencePendingApproval || result.ExecutionStatus != ExecutionPendingApproval {
					t.Fatal("待批被记为成功")
				}
			} else if result.Outcome != InferenceRejected || orch.GetStats().RejectedCount != 1 {
				t.Fatal("拒绝/未知授权状态放行")
			}
		})
	}
}

func TestInferenceExecutionMissingPortsFailBeforeEffects(t *testing.T) {
	for _, port := range []string{"gate", "router", "generator", "reviewer", "delivery"} {
		t.Run(port, func(t *testing.T) {
			orch, pub, events := executionFixture(t, 1)
			switch port {
			case "gate":
				orch.SetExecutionGate(nil)
			case "router":
				orch.SetToolRouter(nil)
			case "generator":
				orch.SetReplyGenerator(nil)
			case "reviewer":
				orch.SetFinalReviewer(nil)
			case "delivery":
				orch.SetDeliveryGateway(nil)
			}
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err == nil || result.ErrorCode != "execution_not_configured" || result.Executed || result.ToolCallCount != 0 || len(*events) != 0 || len(pub.replies) != 0 || result.Publishable || orch.GetStats().ErrorCount != 1 {
				t.Fatalf("缺少端口仍有副作用：%+v events=%v err=%v", result, *events, err)
			}
		})
	}
}

func TestInferenceExecutionErrorsStopPublication(t *testing.T) {
	for _, fault := range []string{"authorization", "tool_error", "tool_rejected", "generator", "empty_reply", "review_error", "review_nil", "review_rejected", "review_unstable", "publisher"} {
		t.Run(fault, func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 2)
			cause := errors.New("private execution failure")
			calls := 0
			switch fault {
			case "authorization":
				orch.SetExecutionGate(executionGateFunc(func(context.Context, ExecutionRequest) (ExecutionAuthorization, error) { return "", cause }))
			case "tool_error", "tool_rejected":
				orch.SetToolRouter(executionToolFunc(func(context.Context, string, map[string]any) (ToolOutput, error) {
					calls++
					if fault == "tool_error" {
						return ToolOutput{}, cause
					}
					return ToolOutput{Success: false, Data: "unsafe partial result", Error: cause.Error()}, nil
				}))
			case "generator", "empty_reply":
				orch.SetReplyGenerator(executionGeneratorFunc(func(context.Context, CustomerMessagePayload, *AgentContext, *ActionPlan, []ToolExecutionResult) (string, error) {
					if fault == "generator" {
						return "", cause
					}
					return " \n ", nil
				}))
			case "review_error", "review_nil", "review_rejected", "review_unstable":
				orch.SetFinalReviewer(executionReviewFunc{review: func(context.Context, *InferenceContext) (*ReviewResult, error) {
					switch fault {
					case "review_error":
						return nil, cause
					case "review_nil":
						return nil, nil
					case "review_rejected":
						return &ReviewResult{Passed: false}, nil
					}
					calls++
					return &ReviewResult{Passed: true, AdjustedReply: fmt.Sprintf("反复变化%d", calls)}, nil
				}})
			case "publisher":
				orch.SetDeliveryGateway(newExecutionDeliveryFixture(func(context.Context, delivery.Intent) error { return cause }))
			}
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if fault == "review_rejected" {
				if err != nil || result.Outcome != InferenceRejected {
					t.Fatalf("复核拒绝错误：%+v %v", result, err)
				}
			} else if err == nil || result.Outcome != InferenceFailed || result.ExecutionStatus != ExecutionFailed {
				t.Fatalf("技术错误变成成功：%+v %v", result, err)
			}
			if result.FinalReply != "" || result.Publishable || len(pub.replies) != 0 {
				t.Fatalf("失败仍发布：%+v pub=%v", result, pub.replies)
			}
			wantCalls := 2
			if fault == "authorization" {
				wantCalls = 0
			}
			if strings.HasPrefix(fault, "tool_") {
				wantCalls = 1
				if calls != 1 {
					t.Fatal("工具失败后继续后续工具")
				}
			}
			if result.ToolCallCount != wantCalls || orch.GetStats().ToolCallCount != int64(wantCalls) {
				t.Fatalf("工具实际次数错误：%+v stats=%+v", result, orch.GetStats())
			}
			blob, _ := json.Marshal(result)
			if strings.Contains(string(blob), "private execution failure") || strings.Contains(string(blob), "unsafe partial") {
				t.Fatal("失败工具输出泄漏")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("对外错误泄漏")
			}
			if fault == "tool_rejected" && (err == nil || errors.Unwrap(err) == nil || errors.Unwrap(err).Error() != cause.Error()) {
				t.Fatal("失败工具的内部原因未保留在错误链中")
			}
		})
	}
}

func TestInferenceExecutionHandoffRequiresExplicitApproval(t *testing.T) {
	for _, mode := range []string{"planning", "missing", "pending", "rejected", "approved", "failed"} {
		t.Run(mode, func(t *testing.T) {
			var calls []string
			cycle := outcomeCycle("", false, nil, &calls)
			cycle.GatekeeperStage = outcomeStage{"gatekeeper", func(_ context.Context, ic *InferenceContext) StageResult {
				ic.Decision.HandoffToHuman = true
				ic.Decision.HandoffReason = "安全交接"
				return StopResult(&ic.Decision)
			}}
			triggers := 0
			var gateway delivery.Gateway
			if mode != "missing" {
				gateway = newExecutionDeliveryFixture(func(context.Context, delivery.Intent) error {
					triggers++
					if mode == "failed" {
						return errors.New("private handoff failure")
					}
					return nil
				})
			}
			orch := NewCoreDataFlowOrchestrator(cycle, nil)
			orch.SetDeliveryGateway(gateway)
			gateCalls := 0
			orch.SetExecutionGate(executionGateFunc(func(_ context.Context, r ExecutionRequest) (ExecutionAuthorization, error) {
				gateCalls++
				if !r.Handoff || r.ToolCall != nil {
					t.Fatal("人工交接授权上下文错误")
				}
				switch mode {
				case "pending":
					return ExecutionApprovalPending, nil
				case "rejected":
					return ExecutionDenied, nil
				}
				return ExecutionApproved, nil
			}))
			executionMode := ExecutionModeExecution
			if mode == "planning" {
				executionMode = ExecutionModePlanning
			}
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, executionMode)
			if result.ToolCallCount != 0 || result.Publishable || result.FinalReply != "" || !result.HandoffToHuman {
				t.Fatalf("人工交接混用回复或工具：%+v", result)
			}
			switch mode {
			case "planning":
				if err != nil || result.Executed || triggers != 0 || gateCalls != 0 || result.ExecutionStatus != ExecutionNotExecuted {
					t.Fatalf("规划触发人工交接：%+v %v", result, err)
				}
			case "missing":
				if err == nil || result.ErrorCode != "execution_not_configured" || result.Executed || gateCalls != 0 || triggers != 0 {
					t.Fatal("未装配交接仍执行")
				}
			case "pending", "rejected":
				if err != nil || result.Executed || triggers != 0 || gateCalls != 1 {
					t.Fatalf("未授权交接：%+v %v", result, err)
				}
			case "approved":
				if err != nil || !result.Executed || result.ExecutionStatus != ExecutionHandoffRequested || triggers != 1 || orch.GetStats().FastHandoffCount != 1 {
					t.Fatalf("已批准交接未调用：%+v %v", result, err)
				}
			case "failed":
				if err == nil || result.Outcome != InferenceFailed || triggers != 1 || orch.GetStats().FastHandoffCount != 0 {
					t.Fatalf("失败交接被记作成功：%+v %v", result, err)
				}
			}
		})
	}
}

type executionPublisherFunc func(context.Context, string, string, string) error

func (f executionPublisherFunc) Publish(ctx context.Context, ch, customer, reply string) error {
	return f(ctx, ch, customer, reply)
}

func TestInferenceExecutionCancellationStopsNextEffect(t *testing.T) {
	for _, at := range []string{"authorization", "tool", "generator", "review"} {
		t.Run(at, func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 2)
			ctx, cancel := context.WithCancel(executionTestContext())
			defer cancel()
			switch at {
			case "authorization":
				orch.SetExecutionGate(executionGateFunc(func(context.Context, ExecutionRequest) (ExecutionAuthorization, error) {
					cancel()
					return ExecutionApproved, nil
				}))
			case "tool":
				orch.SetToolRouter(executionToolFunc(func(context.Context, string, map[string]any) (ToolOutput, error) {
					cancel()
					return ToolOutput{Success: true}, nil
				}))
			case "generator":
				orch.SetReplyGenerator(executionGeneratorFunc(func(context.Context, CustomerMessagePayload, *AgentContext, *ActionPlan, []ToolExecutionResult) (string, error) {
					cancel()
					return "正文", nil
				}))
			case "review":
				orch.SetFinalReviewer(executionReviewFunc{review: func(context.Context, *InferenceContext) (*ReviewResult, error) {
					cancel()
					return &ReviewResult{Passed: true}, nil
				}})
			}
			result, err := orch.ProcessWithMode(ctx, executionPayload(), nil, ExecutionModeExecution)
			if !errors.Is(err, context.Canceled) || result.Outcome != InferenceFailed || result.Publishable || len(pub.replies) != 0 {
				t.Fatalf("取消后仍发布：%+v %v", result, err)
			}
			if at == "authorization" && result.ToolCallCount != 0 || at == "tool" && result.ToolCallCount != 1 {
				t.Fatalf("取消后继续工具：%+v", result)
			}
		})
	}
}

func TestInferenceExecutionReviewedAdjustmentAndDirectReply(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 0)
			if direct {
				orch.cycle.PlannerStage = outcomeStage{"planner", func(_ context.Context, ic *InferenceContext) StageResult {
					ic.Plan = &ActionPlan{PlanType: "reply", ReplyHint: "直接回答", SkipLLM: true, Confidence: 1}
					return ContinueResult()
				}}
				orch.SetReplyGenerator(nil)
			}
			reviews := []string{}
			orch.SetFinalReviewer(executionReviewFunc{review: func(_ context.Context, ic *InferenceContext) (*ReviewResult, error) {
				reviews = append(reviews, ic.Decision.Reply)
				return &ReviewResult{Passed: true, AdjustedReply: "经复核的安全正文"}, nil
			}})
			result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
			if err != nil || result.FinalReply != "经复核的安全正文" || len(reviews) != 2 || reviews[1] != result.FinalReply || !reflect.DeepEqual(pub.replies, []string{result.FinalReply}) || orch.GetStats().DirectReplyCount != 1 {
				t.Fatalf("复核修改稿未复核或直答错误：%+v reviews=%v %v", result, reviews, err)
			}
		})
	}
}

type inferenceExecutionPublisher struct{ replies []string }

func (p *inferenceExecutionPublisher) Publish(_ context.Context, _, _, content string) error {
	p.replies = append(p.replies, content)
	return nil
}

// 真实默认链产生查单计划；默认契约只能返回规划，不能把指令当成最终回复。
func TestInferenceExecutionDefaultPlanningHasNoSideEffects(t *testing.T) {
	spy := &inferenceExecutionSpy{}
	publisher := &inferenceExecutionPublisher{}
	orchestrator := NewCoreDataFlowOrchestrator(nil, nil)
	orchestrator.SetToolRouter(spy)
	orchestrator.SetPublisher(publisher)
	result, err := orchestrator.Process(context.Background(), CustomerMessagePayload{
		ChannelType: "local", CustomerID: "local-customer", SessionID: "planning-only",
		Content: "我的订单到哪了", MessageType: "text",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolCallCount != spy.calls || spy.calls != 0 {
		t.Errorf("计划数冒充实际调用数：reported=%d actual=%d", result.ToolCallCount, spy.calls)
	}
	if result.FinalReply != "" || len(publisher.replies) != 0 || result.Publishable {
		t.Errorf("规划泄漏为最终回复或外发：reply=%q published=%v publishable=%v", result.FinalReply, publisher.replies, result.Publishable)
	}
	blob, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Mode    string      `json:"mode"`
		Status  string      `json:"execution_status"`
		Planned int         `json:"planned_tool_call_count"`
		Plan    *ActionPlan `json:"plan"`
	}
	if err := json.Unmarshal(blob, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.Mode != "planning" || contract.Status != "not_executed" || contract.Planned != 1 || contract.Plan == nil {
		t.Errorf("未显式返回规划契约：%s", blob)
	}
	stats := orchestrator.GetStats()
	if stats.ToolCallCount != 0 || stats.DirectReplyCount != 0 || stats.FastHandoffCount != 0 {
		t.Errorf("仅规划被记为执行或外发：%+v", stats)
	}
}

// 任一执行端口协作超时后，不得调用后续工具或发布候选正文。
func TestInferenceExecutionDeadlineStopsEffects(t *testing.T) {
	for _, at := range []string{"authorization", "tool", "generator", "review"} {
		t.Run(at, func(t *testing.T) {
			orch, pub, _ := executionFixture(t, 2)
			ctx, cancel := context.WithCancel(executionTestContext())
			defer cancel()
			// 从目标端口进入时才启动截止时间，避免机器负载把规划阶段误当作端口超时。
			wait := func() error {
				deadline, stop := context.WithTimeout(ctx, time.Millisecond)
				defer stop()
				<-deadline.Done()
				return deadline.Err()
			}
			switch at {
			case "authorization":
				orch.SetExecutionGate(executionGateFunc(func(context.Context, ExecutionRequest) (ExecutionAuthorization, error) { return "", wait() }))
			case "tool":
				orch.SetToolRouter(executionToolFunc(func(context.Context, string, map[string]any) (ToolOutput, error) { return ToolOutput{}, wait() }))
			case "generator":
				orch.SetReplyGenerator(executionGeneratorFunc(func(context.Context, CustomerMessagePayload, *AgentContext, *ActionPlan, []ToolExecutionResult) (string, error) {
					return "", wait()
				}))
			case "review":
				orch.SetFinalReviewer(executionReviewFunc{review: func(context.Context, *InferenceContext) (*ReviewResult, error) { return nil, wait() }})
			}
			result, err := orch.ProcessWithMode(ctx, executionPayload(), nil, ExecutionModeExecution)
			wantCalls := map[string]int{"authorization": 0, "tool": 1, "generator": 2, "review": 2}[at]
			if !errors.Is(err, context.DeadlineExceeded) || result.ExecutionStatus != ExecutionFailed || result.ToolCallCount != wantCalls || result.FinalReply != "" || result.Publishable || len(pub.replies) != 0 {
				t.Fatalf("超时继续执行或遗漏计数：result=%+v err=%v", result, err)
			}
		})
	}
}

// 第二个工具失败保留第一个真实结果，失败尝试也计数，但不得生成或发布回复。
func TestInferenceExecutionPartialToolsRetainActualCounts(t *testing.T) {
	orch, pub, events := executionFixture(t, 2)
	cause := errors.New("private second tool failure")
	calls := 0
	orch.SetToolRouter(executionToolFunc(func(context.Context, string, map[string]any) (ToolOutput, error) {
		calls++
		if calls == 2 {
			return ToolOutput{Data: "不可当成功的部分输出"}, cause
		}
		return ToolOutput{Success: true, Data: "第一个工具的真实结果"}, nil
	}))
	result, err := orch.ProcessWithMode(executionTestContext(), executionPayload(), nil, ExecutionModeExecution)
	if !errors.Is(err, cause) || result.ToolCallCount != 2 || len(result.ToolResults) != 2 || !result.ToolResults[0].Success || result.ToolResults[0].Data != "第一个工具的真实结果" || result.ToolResults[1].Success || result.ToolResults[1].Data != "" || orch.GetStats().ToolCallCount != 2 || result.Publishable || len(pub.replies) != 0 {
		t.Fatalf("部分执行被计划数或成功回复替代：%+v err=%v", result, err)
	}
	for _, event := range *events {
		if event == "generate" || strings.HasPrefix(event, "review:") {
			t.Fatalf("工具失败后继续生成或复核：%v", *events)
		}
	}
}
