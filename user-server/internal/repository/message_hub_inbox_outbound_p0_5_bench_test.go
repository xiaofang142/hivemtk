package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// newBenchDB 引导基准测试库。
//
// ⚠️ 2026-09-16 审计（TEST-06）：此处原为**第四套**独立的测试库引导
// （getBenchEnv + getBenchTestDSN + benchDBNameRe/benchProcDBInit/benchProcDBName，
// 外加自建库 `user_db_test_bench_<pid>`），与 internal/pkg/testutil 并存，
// 且默认值互相矛盾：
//   - 默认端口 8202（**容器内**端口），而 testutil 探测 8232/8202；
//   - 默认 user/password 都是 `postgres`，而 testutil 用 admin / POSTGRES_PASSWORD；
//   - 认的是第五个环境变量名 `TEST_DATABASE_URL`（其余代码只认 POSTGRES_TEST_DSN）；
//   - 连不上即 `tb.Skipf` → 基准测试静默跳过（exit 0），跑没跑过无人知晓。
//
// 后果：本机 PG 实际映射在 8232，这 2 个基准长期"跳过"而非执行。
//
// 现统一委托 testutil.NewTestDB（其参数已放宽为 testing.TB 正是为了支持 benchmark），
// 隔离库回归 user_db_test_<pid>，不再产生额外的 `_bench_` 库族（实测历史遗留 48 个）。
func newBenchDB(tb testing.TB, models ...any) *gorm.DB {
	tb.Helper()
	return testutil.NewTestDB(tb, models...)
}

func BenchmarkAckOutboundDeliveredBatchReturningWithStatus_500_P0_5(b *testing.B) {
	db := newBenchDB(b, &model.MessageHub{})
	repo := &MessageHubRepository{db: db}
	const (
		channel   = "douyin_web"
		accountID = "acc_bench_500"
		convID    = "conv_bench_500"
		N         = 500
	)
	ctx := context.Background()

	if err := db.Exec("DELETE FROM message_hub WHERE platform = ? AND account_id = ?", channel, accountID).Error; err != nil {
		b.Fatalf("清空失败: %v", err)
	}
	hubs := make([]*model.MessageHub, 0, N)
	for i := 0; i < N; i++ {
		hubs = append(hubs, &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: convID,
			MsgID:          fmt.Sprintf("mh:bench_500_%d", i),
			MsgType:        "text",
			Content:        fmt.Sprintf("bench content %d", i),
			Direction:      "outbound",
			Status:         "pending",
		})
	}
	if err := db.CreateInBatches(hubs, 100).Error; err != nil {
		b.Fatalf("seed 失败: %v", err)
	}
	msgIDs := make([]string, N)
	for i := 0; i < N; i++ {
		msgIDs[i] = fmt.Sprintf("mh:bench_500_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i > 0 {
			if err := db.Model(&model.MessageHub{}).
				Where("platform = ? AND account_id = ?", channel, accountID).
				Update("status", "pending").Error; err != nil {
				b.Fatalf("重置 pending 失败: %v", err)
			}
		}
		updated, affected, err := repo.AckOutboundDeliveredBatchReturningWithStatus(ctx, channel, accountID, convID, "delivered", msgIDs)
		if err != nil {
			b.Fatalf("ack 失败: %v", err)
		}
		if int(affected) != N {
			b.Fatalf("期望 affected=%d，实际 %d", N, affected)
		}
		if len(updated) != N {
			b.Fatalf("期望 updated=%d，实际 %d", N, len(updated))
		}
	}
}

func TestAckOutboundDeliveredBatchReturningWithStatus_500_P0_5_PerfThreshold(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过性能阈值测试")
	}
	db := newBenchDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: db}
	const (
		channel   = "douyin_web"
		accountID = "acc_p0_5_perf"
		convID    = "conv_p0_5_perf"
		N         = 500
	)
	ctx := context.Background()

	if err := db.Exec("DELETE FROM message_hub WHERE platform = ? AND account_id = ?", channel, accountID).Error; err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	hubs := make([]*model.MessageHub, 0, N)
	for i := 0; i < N; i++ {
		hubs = append(hubs, &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: convID,
			MsgID:          fmt.Sprintf("mh:p0_5_perf_%d", i),
			MsgType:        "text",
			Content:        fmt.Sprintf("perf content %d", i),
			Direction:      "outbound",
			Status:         "pending",
		})
	}
	if err := db.CreateInBatches(hubs, 100).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
	msgIDs := make([]string, N)
	for i := 0; i < N; i++ {
		msgIDs[i] = fmt.Sprintf("mh:p0_5_perf_%d", i)
	}

	const R = 5
	durations := make([]time.Duration, 0, R)
	for i := 0; i < R; i++ {
		if err := db.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ?", channel, accountID).
			Update("status", "pending").Error; err != nil {
			t.Fatalf("重置 pending 失败: %v", err)
		}
		start := time.Now()
		_, affected, err := repo.AckOutboundDeliveredBatchReturningWithStatus(ctx, channel, accountID, convID, "delivered", msgIDs)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("第 %d 轮 ack 失败: %v", i+1, err)
		}
		if int(affected) != N {
			t.Fatalf("第 %d 轮期望 affected=%d，实际 %d", i+1, N, affected)
		}
		durations = append(durations, elapsed)
	}

	var p95 time.Duration
	if len(durations) == R {
		sorted := make([]time.Duration, len(durations))
		copy(sorted, durations)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j] < sorted[i] {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		p95 = sorted[int(float64(len(sorted))*0.8)-1]
	}
	t.Logf("P0-5 性能测试：%d 样本耗时 = %v，P95 ≈ %v", R, durations, p95)
	const threshold = 200 * time.Millisecond
	if p95 > threshold {
		t.Errorf("P0-5 性能回归：P95=%v 超过阈值 %v", p95, threshold)
	}
}
