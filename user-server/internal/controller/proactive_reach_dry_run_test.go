package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// validate 强制预检，send 尊重 dry_run；三类目标均不得调用任何发送器。
func TestProactiveReachAPI_DryRunNeverSends(t *testing.T) {
	for _, path := range []string{"/api/reach/proactive/validate", "/api/reach/proactive/send"} {
		for _, target := range []string{"phone", "email", "customer"} {
			t.Run(path+"/"+target, func(t *testing.T) {
				db := newGateAPIDB(t)
				policy := service.NewConfigCompliancePolicy()
				policy.SetPolicySourceForTest(func(context.Context) string { return "" })
				service.SetGlobalCompliancePolicyForTest(policy)
				t.Cleanup(func() { service.SetGlobalCompliancePolicyForTest(nil) })
				svc := service.NewProactiveReachService(repository.NewProactiveReachRepository(db), repository.NewCustomerDoNotContactRepository(db), nil)
				var sends, registryCalls int
				svc.SetSMSRegistry(func() (func(context.Context, string, string, string, map[string]string) (string, error), error) {
					registryCalls++
					return func(context.Context, string, string, string, map[string]string) (string, error) {
						sends++
						return "sms_spy", nil
					}, nil
				})
				svc.SetEmailRegistry(func(context.Context, uint, string, string, string, []string) (string, error) {
					sends++
					return "email_spy", nil
				})
				gate := &reachGateStub{allowed: false, reason: "preview_does_not_authorize_sending"}
				svc.SetPreSendApprovalChecker(gate)
				ctrl := NewProactiveReachController(svc)
				gin.SetMode(gin.TestMode)
				router := gin.New()
				router.POST("/api/reach/proactive/validate", ctrl.ValidateReach)
				router.POST("/api/reach/proactive/send", ctrl.ProactiveSend)
				body := map[string]any{"content": "只预检", "dry_run": path == "/api/reach/proactive/send"}
				channel, recipient := "sms", "13900002109"
				switch target {
				case "phone":
					body["phone"] = recipient
				case "email":
					channel, recipient = "email", "Preview@Example.com"
					body["email"] = recipient
				case "customer":
					id := fmt.Sprintf("fa21-api-%d", time.Now().UnixNano())
					if err := db.Create(&model.Customer{ID: id, UnifiedID: id, Phone: recipient}).Error; err != nil {
						t.Fatalf("创建客户失败: %v", err)
					}
					body["customer_id"] = id
				}
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatalf("序列化请求失败: %v", err)
				}
				w := postReachJSON(t, router, path, string(raw))
				if w.Code != http.StatusOK {
					t.Fatalf("预检应返回200: code=%d body=%s", w.Code, w.Body.String())
				}
				var out struct {
					Code int                            `json:"code"`
					Data service.ProactiveReachResponse `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatalf("解析响应失败: %v", err)
				}
				if out.Code != 0 || out.Data.Status != "dry_run" || out.Data.MessageID != "dry_run" || out.Data.Channel != channel || out.Data.RecipientID != recipient {
					t.Errorf("HTTP响应必须明确预检及真实目标: %+v", out)
				}
				if sends != 0 || registryCalls != 0 || len(gate.asks) != 0 {
					t.Errorf("预检必须零外发、零发送器解析、零审批副作用: sends=%d registry=%d gate=%d", sends, registryCalls, len(gate.asks))
				}
			})
		}
	}
}
