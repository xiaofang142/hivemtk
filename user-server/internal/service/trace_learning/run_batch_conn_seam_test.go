// run_batch_conn_seam_test.go trace_learning RunBatch 独占连接接缝回归
// （AV-04c 批次6 刀4）。
//
// Service 侧三处 GetDB 收口（Default 构造兜底 / DBReady 探针 / 仓储同名 Connection
// 接缝）后：AV-03 钉住的根域唯一合法事务档接缝（session 级 advisory lock）语义
// 逐项不变—— 锁与锁内的待评估扫描走同一条独占连接；db 未就绪 ⇒ 「db nil」错误
// 文案不变。
package trace_learning

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 接缝等价性：Connection 回调里拿到的连接就是一条可执行查询的真连接，回调内用
// 它构造的仓储读到的数据与外部仓储一致；fn 的错误原样穿透。
func TestTraceLearningRepository_Connection_RunsOnOwnConn(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageTrace{}, &model.TraceEvalLog{})
	repo := repository.NewTraceLearningRepository(db)

	queried := false
	err := repo.Connection(context.Background(), func(conn *gorm.DB) error {
		batch := repository.NewTraceLearningRepository(conn)
		ids, e := batch.ListPendingTraceIDs(context.Background(), 24, 10)
		if e != nil {
			return e
		}
		queried = len(ids) == 0
		return nil
	})
	if err != nil {
		t.Fatalf("Connection 回调不应报错: %v", err)
	}
	if !queried {
		t.Fatal("回调内构造的仓储应能执行 ListPendingTraceIDs（同连接可查询）")
	}

	// 错误穿透：fn 返回的错误原样透出（错误路径不变）。
	sentinel := errors.New("connection seam sentinel")
	if err := repo.Connection(context.Background(), func(conn *gorm.DB) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("fn 错误应原样穿透, got %v", err)
	}
}

// 未装配（db=nil）：Connection 守卫返回 gorm.ErrInvalidDB；DBReady 探针为假。
func TestTraceLearningRepository_Connection_NilDB(t *testing.T) {
	var repo *repository.TraceLearningRepository
	if repo.DBReady() {
		t.Fatal("nil 仓储 DBReady 应为 false")
	}
	err := repo.Connection(context.Background(), func(conn *gorm.DB) error { return nil })
	if !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("nil db Connection 应返回 ErrInvalidDB, got %v", err)
	}
}

// RunBatch 错误路径：仓储未装配时报「db nil」（文案保持原样）。
func TestRunBatch_NilRepo_DBNilError(t *testing.T) {
	svc := New(nil, nil, DefaultConfig())
	_, err := svc.RunBatch(context.Background(), 24, 10, false)
	if err == nil || err.Error() != "db nil" {
		t.Fatalf("应报「db nil」, got %v", err)
	}
}

// RunBatch 主路径：空批次上跑通 advisory lock 接缝——加锁、扫描 0 条、放锁，
// 连续两轮都成功（锁在回调内释放，不残留到下一次独占连接）。
func TestRunBatch_EmptyBatch_TwoRoundsSequential(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.MessageTrace{}, &model.TraceEvalLog{})
	svc := New(nil, nil, DefaultConfig())
	for round := 1; round <= 2; round++ {
		res, err := svc.RunBatch(context.Background(), 24, 10, true)
		if err != nil {
			t.Fatalf("第 %d 轮 RunBatch 不应报错: %v", round, err)
		}
		if res == nil || res.Processed != 0 {
			t.Fatalf("空批次 Processed 应为 0: %+v", res)
		}
	}
}

// Default 构造兜底：repo==nil 时 New 兜底装配的仓储在有库环境下就绪（句柄来自
// 同一全局，Default 构造与原 service 侧取句柄拼装同连接同时刻）。
func TestNew_NilRepoFallsBackToDefault(t *testing.T) {
	_ = testutil.NewTestDB(t)
	svc := New(nil, nil, DefaultConfig())
	if svc == nil || svc.repo == nil {
		t.Fatal("兜底装配应产出非 nil 仓储")
	}
	if !svc.repo.DBReady() {
		t.Fatal("有库环境下兜底仓储应就绪")
	}
}

// Logs/TopWeights 委托仓储不变（装配收口后读路径烟测）。
func TestService_Logs_TopWeights_DelegateToRepo(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.TraceEvalLog{}, &model.KnowledgeChunk{})
	svc := New(nil, nil, DefaultConfig())
	logs, err := svc.Logs(context.Background(), 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("空表 Logs 应回空: %v %v", logs, err)
	}
	rows, err := svc.TopWeights(context.Background(), 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("空表 TopWeights 应回空: %v %v", rows, err)
	}
}
