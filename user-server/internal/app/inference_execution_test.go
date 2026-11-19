package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
)

type inferenceHTTPGate struct {
	state agent_runtime.ExecutionAuthorization
}

func (g inferenceHTTPGate) Authorize(context.Context, agent_runtime.ExecutionRequest) (agent_runtime.ExecutionAuthorization, error) {
	return g.state, nil
}

type inferenceHTTPGenerator struct{ fail bool }

func (g inferenceHTTPGenerator) GenerateReply(_ context.Context, _ agent_runtime.CustomerMessagePayload, _ *agent_runtime.AgentContext, _ *agent_runtime.ActionPlan, results []agent_runtime.ToolExecutionResult) (string, error) {
	if g.fail {
		return "", errors.New("private generator fault")
	}
	return "实际工具结果：" + results[0].Data, nil
}

type inferenceHTTPTool struct {
	calls int
	fail  bool
}

func (t *inferenceHTTPTool) Execute(context.Context, string, map[string]any) (agent_runtime.ToolOutput, error) {
	t.calls++
	if t.fail {
		return agent_runtime.ToolOutput{}, errors.New("private tool fault")
	}
	return agent_runtime.ToolOutput{Success: true, Data: "本地订单已发货"}, nil
}

type inferenceHTTPPublisher struct{ replies []string }

func (p *inferenceHTTPPublisher) Publish(_ context.Context, _, _, reply string) error {
	p.replies = append(p.replies, reply)
	return nil
}

// 真实默认推理链和原路由验证仅规划、显式执行、审批中及失败封套。
func TestInferenceExecutionHTTPContracts(t *testing.T) {
	previous := globalInferenceOrchestrator
	t.Cleanup(func() { globalInferenceOrchestrator = previous })
	for _, scenario := range []string{"default", "planning", "execution", "pending", "denied", "unconfigured", "invalid", "tool_failed", "generation_failed"} {
		t.Run(scenario, func(t *testing.T) {
			orch := agent_runtime.NewCoreDataFlowOrchestrator(nil, nil)
			tool := &inferenceHTTPTool{fail: scenario == "tool_failed"}
			publisher := &inferenceHTTPPublisher{}
			if scenario != "unconfigured" {
				state := agent_runtime.ExecutionApproved
				if scenario == "pending" {
					state = agent_runtime.ExecutionApprovalPending
				}
				if scenario == "denied" {
					state = agent_runtime.ExecutionDenied
				}
				orch.SetExecutionGate(inferenceHTTPGate{state})
				orch.SetToolRouter(tool)
				orch.SetReplyGenerator(inferenceHTTPGenerator{fail: scenario == "generation_failed"})
				orch.SetFinalReviewer(agent_runtime.NewDefaultReviewer())
				orch.SetPublisher(publisher)
			}
			globalInferenceOrchestrator = orch
			router := gin.New()
			SetupInferenceRoutes(router.Group("/api"))
			body := map[string]string{"channel_type": "local", "customer_id": "local-customer", "content": "我的订单到哪了", "session_id": "local-session"}
			if scenario != "default" {
				mode := "execution"
				if scenario == "planning" {
					mode = "planning"
				}
				if scenario == "invalid" {
					mode = "typo"
				}
				body["mode"] = mode
			}
			blob, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/agent/inference/run", bytes.NewReader(blob))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var reply struct {
				Code json.RawMessage `json:"code"`
				Data struct {
					Mode        string                    `json:"mode"`
					Status      string                    `json:"execution_status"`
					Outcome     string                    `json:"outcome"`
					Count       int                       `json:"tool_call_count"`
					Planned     int                       `json:"planned_tool_call_count"`
					Plan        *agent_runtime.ActionPlan `json:"plan"`
					Final       string                    `json:"final_reply"`
					Publishable bool                      `json:"publishable"`
					Executed    bool                      `json:"executed"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("HTTP 暴露端口内部原因")
			}
			wantStatus, wantCount := 200, 0
			switch scenario {
			case "default", "planning":
				if reply.Data.Mode != "planning" || reply.Data.Status != "not_executed" || reply.Data.Planned != 1 || reply.Data.Plan == nil || reply.Data.Executed {
					t.Fatalf("未返回显式规划契约：%s", w.Body.String())
				}
			case "execution":
				wantCount = 1
				if reply.Data.Status != "succeeded" || !reply.Data.Executed || !reply.Data.Publishable || reply.Data.Final != "实际工具结果：本地订单已发货" || len(publisher.replies) != 1 {
					t.Fatalf("执行未经过真实结果和复核：%s", w.Body.String())
				}
			case "pending":
				wantStatus = 202
				if reply.Data.Status != "pending_approval" || reply.Data.Outcome != "pending_approval" || reply.Data.Executed {
					t.Fatal("待批被当作执行成功")
				}
			case "denied":
				wantStatus = 409
			case "unconfigured":
				wantStatus = 503
			case "invalid":
				wantStatus = 400
			case "tool_failed", "generation_failed":
				wantStatus = 500
				wantCount = 1
				if reply.Data.Status != "failed" || reply.Data.Outcome != "failed" || !reply.Data.Executed {
					t.Fatalf("错误执行状态：%s", w.Body.String())
				}
			}
			if w.Code != wantStatus || tool.calls != wantCount || reply.Data.Count != wantCount {
				t.Fatalf("HTTP 状态或实际调用数错误：status=%d want=%d actual=%d response=%s", w.Code, wantStatus, tool.calls, w.Body.String())
			}
			if wantStatus < 300 {
				if string(reply.Code) != "0" {
					t.Fatal("成功或受理封套错误")
				}
			} else if string(reply.Code) == "0" {
				t.Fatal("错误响应误报成功")
			}
			if scenario != "execution" && (reply.Data.Final != "" || reply.Data.Publishable || len(publisher.replies) != 0) {
				t.Fatalf("未成功执行仍发布：%s", w.Body.String())
			}
		})
	}
}
