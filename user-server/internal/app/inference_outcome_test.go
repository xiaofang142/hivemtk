package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"

	"github.com/gin-gonic/gin"
)

type inferenceOutcomeHTTPStage struct{ name, fault, mode string }

func (s inferenceOutcomeHTTPStage) Name() string { return s.name }
func (s inferenceOutcomeHTTPStage) Execute(_ context.Context, ic *agent_runtime.InferenceContext) agent_runtime.StageResult {
	if s.name == s.fault {
		ic.Decision.Reply = "未经复核的旧回复"
		return agent_runtime.FailResult(errors.New("private provider secret"))
	}
	if s.name == "planner" {
		ic.Plan = &agent_runtime.ActionPlan{ReplyHint: "已复核回复", SkipLLM: true, Confidence: 1}
	}
	if s.name == "reviewer" {
		ic.Decision.Review = &agent_runtime.ReviewResult{Passed: s.mode != "rejected"}
	}
	return agent_runtime.ContinueResult()
}

// 使用原路由和真实编排器验证封套，技术错误不得泄漏端口原文。
func TestInferenceOutcomeHTTP(t *testing.T) {
	previous := globalInferenceOrchestrator
	t.Cleanup(func() { globalInferenceOrchestrator = previous })
	for _, mode := range []string{"perception", "alignment", "gatekeeper", "planner", "reviewer", "rejected", "succeeded"} {
		t.Run(mode, func(t *testing.T) {
			mk := func(name string) agent_runtime.InferenceStage { return inferenceOutcomeHTTPStage{name, mode, mode} }
			cycle := agent_runtime.NewInferenceCycleWithStages(mk("perception"), mk("alignment"), mk("gatekeeper"), mk("planner"))
			cycle.SetReviewerStage(mk("reviewer"))
			globalInferenceOrchestrator = agent_runtime.NewCoreDataFlowOrchestrator(cycle, nil)
			router := gin.New()
			SetupInferenceRoutes(router.Group("/api"))
			request := httptest.NewRequest("POST", "/api/agent/inference/run", bytes.NewBufferString(`{"channel_type":"test","customer_id":"local","session_id":"local-run","content":"本地验收"}`))
			request.Header.Set("Content-Type", "application/json")
			writer := httptest.NewRecorder()
			router.ServeHTTP(writer, request)
			var envelope struct {
				Code json.RawMessage `json:"code"`
				Data struct {
					Outcome     string                        `json:"outcome"`
					Publishable bool                          `json:"publishable"`
					FailedStage string                        `json:"failed_stage"`
					Stages      []agent_runtime.StageDecision `json:"stages"`
					FinalReply  string                        `json:"final_reply"`
				} `json:"data"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(writer.Body.String(), "private provider secret") {
				t.Fatal("HTTP 泄漏内部错误")
			}
			switch mode {
			case "succeeded":
				if writer.Code != 200 || string(envelope.Code) != "0" || envelope.Data.Outcome != mode || !envelope.Data.Publishable || envelope.Data.FinalReply != "已复核回复" {
					t.Fatalf("成功响应异常：%d %s", writer.Code, writer.Body.String())
				}
			case "rejected":
				if writer.Code != 409 || len(envelope.Code) == 0 || string(envelope.Code) == "0" || envelope.Data.Outcome != mode || envelope.Data.Publishable || envelope.Data.FinalReply != "" {
					t.Fatalf("拒绝返回成功封套：%d %s", writer.Code, writer.Body.String())
				}
			default:
				if writer.Code != 500 || len(envelope.Code) == 0 || string(envelope.Code) == "0" || envelope.Data.Outcome != "failed" || envelope.Data.FailedStage != mode || len(envelope.Data.Stages) == 0 || envelope.Data.Publishable || envelope.Data.FinalReply != "" {
					t.Fatalf("故障响应缺少失败诊断：%d %s", writer.Code, writer.Body.String())
				}
			}
		})
	}
}
