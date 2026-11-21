package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 持久任务采用真实 PostgreSQL 夹具，验证内部截止后仍能保存失败终态。
func TestFA12Followup_InternalDeadlinePersistsFailure(t *testing.T) {
	f := newFA12CrawlerFixture(t)
	src := f.source(7, false)
	svc, _ := f.crawler()
	svc.batchTimeout = 20 * time.Millisecond
	svc.http = &http.Client{Transport: fa12RoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/robots.txt" {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("User-agent: *\nAllow: /\n")), Request: req}, nil
	})}
	ctx := tenant.WithTenantID(context.Background(), 7)
	batch, err := svc.CreateBatch(ctx, src.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.RunBatch(ctx, batch.ID)
	var stored model.KBCrawlBatch
	if readErr := f.db.WithContext(ctx).First(&stored, batch.ID).Error; readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) || stored.Status != model.KBCrawlBatchStatusFailed || stored.FinishedAt == nil || stored.LeaseToken != "" {
		t.Fatalf("内部超时必须持久失败并释放租约: err=%v batch=%+v", err, stored)
	}
}

func TestFA12Followup_ProcessingKeepsBatchDeadlineAndTrace(t *testing.T) {
	ctx, cancel := context.WithCancel(logger.WithTraceID(tenant.WithTenantID(context.Background(), 7), "fa12-batch"))
	ctx = context.WithValue(ctx, crawlerBatchContextKey{}, true)
	proc := crawlerProcessingContext(ctx, 7)
	cancel()
	if proc.Err() != context.Canceled || logger.TraceIDFromContext(proc) != "fa12-batch" || tenant.IsSystemScope(proc) {
		t.Fatalf("批次处理丢取消/trace，err=%v trace=%q", proc.Err(), logger.TraceIDFromContext(proc))
	}
}

// crawlerProcessingContext 为处理任务恢复源租户；批次内必须遵守批次的取消/截止。
// 爬虫成功/失败判定和后续文档读写必须共享这一上下文，不能退回无租户后台 ctx。
func crawlerProcessingContext(ctx context.Context, tenantID uint) context.Context {
	if tenantID == 0 && !tenant.IsMultiTenant() {
		tenantID = tenant.WriteTenantID(ctx)
	}
	if inBatch, _ := ctx.Value(crawlerBatchContextKey{}).(bool); !inBatch {
		ctx = context.WithoutCancel(ctx)
	}
	return crawlerTenantContext(ctx, tenantID)
}
