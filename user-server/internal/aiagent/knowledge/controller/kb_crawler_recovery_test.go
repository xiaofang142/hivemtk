package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	knowledgeRepo "hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgeService "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestCrawlerRecoveryController_AsyncKeepsTenantAfterRequestCancellation(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%t", retry), func(t *testing.T) {
			database := testutil.NewTestDB(t, &model.KBCrawlerSource{}, &model.KBCrawlBatch{})
			tenant.SetMode(tenant.ModeOn)
			t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
			type traceKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(tenant.WithTenantID(context.Background(), 73), traceKey{}, "fa12-controller-trace"))
			defer cancel()
			source := &model.KBCrawlerSource{TenantID: 73, StartURL: "https://example.com/docs", SourceType: "url",
				MaxPages: 1, SiteVerified: true, Status: model.KBCrawlerSourceStatusActive}
			if err := database.WithContext(ctx).Create(source).Error; err != nil {
				t.Fatal(err)
			}
			svc := knowledgeService.NewCrawlerServiceWithRepos(knowledgeRepo.NewKBCrawlerRepository(database), nil)
			ctrl := NewKBCrawlerController(svc, nil)
			path := fmt.Sprintf("/sources/%d/sync", source.ID)
			if retry {
				batch := &model.KBCrawlBatch{TenantID: 73, SourceID: source.ID, BatchNo: "failed-controller-fixture", Status: model.KBCrawlBatchStatusFailed}
				if err := database.WithContext(ctx).Create(batch).Error; err != nil {
					t.Fatal(err)
				}
				path = fmt.Sprintf("/batches/%d/retry", batch.ID)
			}
			type observation struct {
				tenantID uint
				err      error
				trace    any
				bounded  bool
			}
			result := make(chan observation, 1)
			release := make(chan struct{})
			injected := errors.New("合成运行状态写入失败，禁止后续网络")
			const callback = "test:async_crawler_tenant"
			if err := database.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				fields, ok := tx.Statement.Dest.(map[string]any)
				if tx.Statement.Table != "kb_crawl_batches" || !ok || fields["status"] != model.KBCrawlBatchStatusRunning {
					return
				}
				<-release
				id, _ := tenant.FromContext(tx.Statement.Context)
				deadline, bounded := tx.Statement.Context.Deadline()
				result <- observation{tenantID: id, err: tx.Statement.Context.Err(), trace: tx.Statement.Context.Value(traceKey{}), bounded: bounded && time.Until(deadline) > 0 && time.Until(deadline) <= 31*time.Minute}
				tx.AddError(injected)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Callback().Update().Remove(callback) })
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.POST("/sources/:id/sync", ctrl.SyncSource)
			router.POST("/batches/:id/retry", ctrl.RetryBatch)
			request := httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			cancel()
			close(release)
			if response.Code != http.StatusOK {
				t.Fatalf("创建后台批次失败：%d %s", response.Code, response.Body.String())
			}
			select {
			case got := <-result:
				if got.tenantID != 73 || got.err != nil || got.trace != "fa12-controller-trace" || !got.bounded {
					t.Fatalf("请求取消后后台必须保留租户且独立运行：%+v", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("手动同步或重试未实际启动后台批次")
			}
		})
	}
}
