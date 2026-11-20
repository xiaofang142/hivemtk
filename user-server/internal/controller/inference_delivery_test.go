package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"hivemtk-user/internal/aiagent/agent/delivery"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func TestInferenceDeliveryHTTPQueryAndRetry(t *testing.T) {
	for _, scenario := range []string{"get", "retry_blocked", "retry_rejected", "cross_tenant", "missing_tenant", "invalid_key", "missing_service", "store_failure", "not_found"} {
		t.Run(scenario, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.InferenceDelivery{})
			svc := service.NewInferenceDeliveryService(repository.NewInferenceDeliveryRepository(db), nil, nil)
			ctx := tenant.WithTenantID(context.Background(), 773)
			intent := delivery.Intent{MessageID: "message-1", Channel: "local", AccountID: "account-1", CustomerID: "customer-1", SessionID: "session-1", AgentCode: "default", Kind: "reply", Content: "本地受控回复"}
			record, err := svc.Submit(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			if record.State != delivery.Blocked {
				t.Fatal("未装配时没有持久记录阻塞")
			}
			key := record.Key
			method := "GET"
			suffix := ""
			wantStatus := 200
			switch scenario {
			case "retry_blocked":
				method, suffix, wantStatus = "POST", "/retry", 202
			case "retry_rejected":
				if err := db.Model(&model.InferenceDelivery{}).Where("tenant_id = ? AND key = ?", 773, key).Updates(map[string]any{"state": string(delivery.Rejected), "error_code": "delivery_rejected"}).Error; err != nil {
					t.Fatal(err)
				}
				method, suffix, wantStatus = "POST", "/retry", 409
			case "cross_tenant":
				ctx = tenant.WithTenantID(context.Background(), 774)
				wantStatus = 404
			case "missing_tenant":
				ctx = context.Background()
				wantStatus = 403
			case "invalid_key":
				key = "bad-key"
				wantStatus = 400
			case "missing_service":
				svc = nil
				wantStatus = 503
			case "not_found":
				key = strings.Repeat("b", 64)
				wantStatus = 404
			case "store_failure":
				if err := db.Callback().Query().Before("gorm:query").Register("delivery_http_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "inference_deliveries" {
						tx.AddError(errors.New("private store failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Callback().Query().Remove("delivery_http_failure") })
				wantStatus = 503
			}
			router := gin.New()
			NewInferenceDeliveryController(svc).RegisterRoutes(router.Group("/api"))
			req := httptest.NewRequest(method, "/api/v1/agent/inference/deliveries/"+key+suffix, nil).WithContext(ctx)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != wantStatus || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), intent.Content) {
				t.Fatalf("查询/重试状态或隔离错误：%d %s", w.Code, w.Body.String())
			}
			var result struct {
				Code json.RawMessage `json:"code"`
				Data delivery.Record `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if wantStatus < 300 {
				if string(result.Code) != "0" || result.Data.Key != record.Key || result.Data.AcceptedCount != 0 || result.Data.ConfirmedCount != 0 {
					t.Fatalf("可靠受理被当成外部确认：%s", w.Body.String())
				}
			} else if string(result.Code) == "0" {
				t.Fatalf("失败查询被记作成功：%s", w.Body.String())
			}
		})
	}
}

type deliveryControllerStoreFault struct{ record *delivery.Record }

func (s deliveryControllerStoreFault) Submit(context.Context, delivery.Intent) (*delivery.Record, error) {
	return s.record, errors.New("private store write failure")
}
func (s deliveryControllerStoreFault) Get(context.Context, string) (*delivery.Record, error) {
	return s.record, errors.New("private store write failure")
}
func (s deliveryControllerStoreFault) Retry(context.Context, string) (*delivery.Record, error) {
	return s.record, errors.New("private store write failure")
}

func TestInferenceDeliveryHTTPStoreFaultRetainsQueryEvidence(t *testing.T) {
	key := strings.Repeat("a", 64)
	record := &delivery.Record{Key: key, Kind: "reply", State: delivery.Sending, SendAttempts: 1}
	router := gin.New()
	NewInferenceDeliveryController(deliveryControllerStoreFault{record}).RegisterRoutes(router.Group("/api"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/agent/inference/deliveries/"+key+"/retry", nil).WithContext(tenant.WithTenantID(context.Background(), 773)))
	var result struct {
		Data delivery.Record `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 503 || result.Data.Key != key || result.Data.State != delivery.Sending || result.Data.ConfirmedCount != 0 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("持久状态的查询凭据丢失或被当成确认：%d %s", w.Code, w.Body.String())
	}
}
