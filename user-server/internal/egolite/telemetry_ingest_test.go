package egolite

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// S-9 上报入口与聚合查询：真库实跑钉三件事——
//  1. 入库前白名单裁决：页面内容类键进不来，缺 kind 的样本整条拒收；
//  2. 批次截断如实上报（accepted/dropped/truncated 三个读数都能对上）；
//  3. 管理端 summary 只回聚合桶，窗口过滤生效（窗口外的样本不进读数）。

func newTelemetryEnv(t *testing.T) (EgoTelemetryRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteTelemetry{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	return NewEgoTelemetryRepositoryWithDB(db), db
}

func TestIngestTelemetryDropsPageContentKeys(t *testing.T) {
	ctx := context.Background()
	repo, _ := newTelemetryEnv(t)

	accepted, dropped, truncated, err := IngestTelemetry(ctx, 7, repo, []map[string]any{
		// 良性样本
		{"kind": "run", "ok": true, "duration_ms": 120, "skill_id": "my-flow", "runner_id": "dev-1"},
		// 页面内容类键：白名单外全丢，样本其余部分照收
		{"kind": "sync", "ok": false, "page_html": "<div>x</div>", "url": "https://x.example.com/a", "text": "正文"},
		// 缺 kind：整条拒收
		{"ok": true, "duration_ms": 5},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if accepted != 2 {
		t.Fatalf("应入库 2 条（缺 kind 拒收），实得 %d", accepted)
	}
	if dropped != 3 {
		t.Fatalf("页面内容类键应丢 3 个（page_html/url/text），实得 %d", dropped)
	}
	if truncated != 0 {
		t.Fatalf("小批量不应截断，实得 %d", truncated)
	}

	buckets, total, _, err := TelemetrySummary(ctx, repo, 24)
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("窗口内应 2 条，实得 %d", total)
	}
	byKind := map[string]TelemetryBucket{}
	for _, b := range buckets {
		byKind[b.Kind] = b
	}
	if b := byKind["run"]; b.Count != 1 || b.OKCount != 1 || b.SuccessPct != 100 {
		t.Fatalf("run 桶应 1 成 100%%，实得 %+v", b)
	}
	if b := byKind["sync"]; b.Count != 1 || b.FailCount != 1 {
		t.Fatalf("sync 桶应 1 败，实得 %+v", b)
	}
}

func TestIngestTelemetryBatchCapTruncates(t *testing.T) {
	ctx := context.Background()
	repo, _ := newTelemetryEnv(t)
	raws := make([]map[string]any, 0, telemetryMaxBatch+3)
	for i := 0; i < telemetryMaxBatch+3; i++ {
		raws = append(raws, map[string]any{"kind": "sync", "ok": true})
	}
	accepted, _, truncated, err := IngestTelemetry(ctx, 7, repo, raws)
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if accepted != telemetryMaxBatch {
		t.Fatalf("超限应截到 %d 条，实得 %d", telemetryMaxBatch, accepted)
	}
	if truncated != 3 {
		t.Fatalf("截断数应=3，实得 %d", truncated)
	}
}

func TestTelemetrySummaryWindowFilter(t *testing.T) {
	ctx := context.Background()
	repo, db := newTelemetryEnv(t)
	// 窗口内一条（入口按"当下"打 at）+ 窗口外一条（直插 48h 前，构造旧样本）
	_, _, _, err := IngestTelemetry(ctx, 7, repo, []map[string]any{{"kind": "run", "ok": true}})
	if err != nil {
		t.Fatal(err)
	}
	old := &model.EgoliteTelemetry{TenantID: 1, UserID: 7, Kind: "sync", OK: false,
		At: time.Now().UTC().Add(-48 * time.Hour)}
	if err := db.Create(old).Error; err != nil {
		t.Fatalf("插入窗口外样本失败: %v", err)
	}
	buckets, total, _, err := TelemetrySummary(ctx, repo, 24)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("24h 窗口应只见 1 条（48h 前的 sync 不进读数），实得 %d", total)
	}
	if len(buckets) != 1 || buckets[0].Kind != "run" {
		t.Fatalf("窗口聚合应只有 run 桶，实得 %+v", buckets)
	}
}
