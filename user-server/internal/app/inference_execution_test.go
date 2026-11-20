package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/aiagent/agent/delivery"
	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
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

type inferenceHTTPPublisher struct {
	mu       sync.Mutex
	replies  []string
	receipts map[string]delivery.Receipt
	lost     bool
}

func (p *inferenceHTTPPublisher) Publish(_ context.Context, _, _, reply string) error {
	p.replies = append(p.replies, reply)
	return nil
}

// 测试对端按稳定键返回明确确认；正式路径仍由真实 PostgreSQL 账本受理。
func (p *inferenceHTTPPublisher) Send(_ context.Context, key string, intent delivery.Intent) (delivery.Receipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if receipt, ok := p.receipts[key]; ok {
		return receipt, nil
	}
	if p.receipts == nil {
		p.receipts = make(map[string]delivery.Receipt)
	}
	p.replies = append(p.replies, intent.Content)
	receipt := delivery.Receipt{State: delivery.Confirmed, ID: "local:" + key}
	p.receipts[key] = receipt
	if p.lost {
		return delivery.Receipt{}, errors.New("private lost delivery receipt")
	}
	return receipt, nil
}
func (p *inferenceHTTPPublisher) Lookup(_ context.Context, key string, _ delivery.Intent) (delivery.Receipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if receipt, ok := p.receipts[key]; ok {
		return receipt, nil
	}
	return delivery.Receipt{State: delivery.Absent}, nil
}

type inferenceHTTPDeliveryPolicy struct{}

func (inferenceHTTPDeliveryPolicy) AuthorizeDelivery(context.Context, delivery.Intent) (bool, error) {
	return true, nil
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
				db := testutil.NewTestDB(t, &model.InferenceDelivery{})
				orch.SetDeliveryGateway(service.NewInferenceDeliveryService(repository.NewInferenceDeliveryRepository(db), publisher, inferenceHTTPDeliveryPolicy{}))
			}
			globalInferenceOrchestrator = orch
			router := gin.New()
			SetupInferenceRoutes(router.Group("/api"))
			body := map[string]string{"channel_type": "local", "customer_id": "local-customer", "content": "我的订单到哪了", "session_id": "local-session", "message_id": "local-message", "account_id": "local-account"}
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
			req = req.WithContext(tenant.WithTenantID(req.Context(), 773))
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

// 原推理路由到真实 PG 账本，再经查询/重试恢复丢失回执；模拟对端不代表生产第三方。
func TestInferenceDeliveryHTTPReceiptRecoveryAndRestart(t *testing.T) {
	previous := globalInferenceOrchestrator
	t.Cleanup(func() { globalInferenceOrchestrator = previous })
	db := testutil.NewTestDB(t, &model.InferenceDelivery{})
	signal := &inferenceHTTPPublisher{lost: true}
	newService := func() service.InferenceDeliveryService {
		return service.NewInferenceDeliveryService(repository.NewInferenceDeliveryRepository(db), signal, inferenceHTTPDeliveryPolicy{})
	}
	orch := agent_runtime.NewCoreDataFlowOrchestrator(nil, nil)
	orch.SetExecutionGate(inferenceHTTPGate{agent_runtime.ExecutionApproved})
	orch.SetToolRouter(&inferenceHTTPTool{})
	orch.SetReplyGenerator(inferenceHTTPGenerator{})
	orch.SetFinalReviewer(agent_runtime.NewDefaultReviewer())
	orch.SetDeliveryGateway(newService())
	globalInferenceOrchestrator = orch
	router := gin.New()
	SetupInferenceRoutes(router.Group("/api"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(tenant.WithTenantID(r.Context(), 773))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	body := `{"mode":"execution","channel_type":"local","account_id":"local-account","customer_id":"local-customer","session_id":"local-session","message_id":"recover-message","content":"我的订单到哪了"}`
	first := request("POST", "/api/agent/inference/run", body)
	var response struct {
		Data struct {
			Delivery    *delivery.Record `json:"delivery"`
			Final       string           `json:"final_reply"`
			Publishable bool             `json:"publishable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if first.Code != 202 || response.Data.Delivery == nil || response.Data.Delivery.State != delivery.Unknown || response.Data.Final != "" || response.Data.Publishable || len(signal.replies) != 1 {
		t.Fatalf("丢回执被伪装确认：%d %s", first.Code, first.Body.String())
	}
	key := response.Data.Delivery.Key
	// 新实例仅依赖真实持久记录与可查询外部回执，不保留原进程内状态。
	restarted := newService()
	controller.NewInferenceDeliveryController(restarted).RegisterRoutes(router.Group("/api"))
	path := "/api/v1/agent/inference/deliveries/" + key
	queried := request("GET", path, "")
	var record struct {
		Data delivery.Record `json:"data"`
	}
	if err := json.Unmarshal(queried.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if queried.Code != 200 || record.Data.State != delivery.Unknown || record.Data.ConfirmedCount != 0 {
		t.Fatalf("重启丢失未确认状态：%s", queried.Body.String())
	}
	retried := request("POST", path+"/retry", "")
	if err := json.Unmarshal(retried.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if retried.Code != 200 || record.Data.State != delivery.Confirmed || record.Data.AcceptedCount != 1 || record.Data.ConfirmedCount != 1 || record.Data.SendAttempts != 1 || len(signal.replies) != 1 {
		t.Fatalf("恢复重复发或未确认：%d %s", retried.Code, retried.Body.String())
	}
	orch.SetDeliveryGateway(newService())
	repeated := request("POST", "/api/agent/inference/run", body)
	if err := json.Unmarshal(repeated.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if repeated.Code != 200 || response.Data.Delivery == nil || response.Data.Delivery.Key != key || !response.Data.Publishable || len(signal.replies) != 1 {
		t.Fatalf("重复 HTTP 产生重复外发：%d %s", repeated.Code, repeated.Body.String())
	}
}
