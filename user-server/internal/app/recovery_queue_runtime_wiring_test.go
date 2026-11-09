// recovery_queue_runtime_wiring_test.go MD-06：挽回队列 HTTP 读写面装配层实跑。
//
// 判据两条：db=nil 不装配（且不回落实全局句柄 —— 本包测试不设全局 DB，
// 若误用 NewRecoveryQueueService 的全局回落，这条往返会在 nil 句柄上直接红）；
// db 非 nil 时显式句柄版构造的服务能对真库完成入队→读回往返。
package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func TestInitRecoveryQueueRuntime_NilDBReturnsNil(t *testing.T) {
	if got := InitRecoveryQueueRuntime(nil); got != nil {
		t.Fatalf("db=nil 不应装配，got %v", got)
	}
}

func TestInitRecoveryQueueRuntime_ExplicitHandleRoundTrip(t *testing.T) {
	db := testutil.NewTestDB(t, &model.RecoveryQueue{})
	svc := InitRecoveryQueueRuntime(db)
	if svc == nil {
		t.Fatal("db 非 nil 时应返回服务实例")
	}
	item, err := svc.Enqueue(context.Background(), &service.RecoveryEnqueueInput{
		CustomerID: "md06-c1",
		UnifiedID:  "md06-u1",
		Account:    "md06-acc",
		Reason:     "churn",
		Strategy:   "sms_coupon",
		Priority:   5,
	})
	if err != nil {
		t.Fatalf("Enqueue 失败（显式句柄未生效）：%v", err)
	}
	if item.ID == 0 || item.Stage != model.RecoveryStageQueued {
		t.Fatalf("入队结果异常：id=%d stage=%s", item.ID, item.Stage)
	}
	rows, total, err := svc.ListByStage(context.Background(), model.RecoveryStageQueued, 1, 10)
	if err != nil {
		t.Fatalf("ListByStage 失败：%v", err)
	}
	if total < 1 {
		t.Fatalf("显式句柄构造的服务读回 0 条（total=%d），装配句柄与读写面不是同一份", total)
	}
	found := false
	for _, r := range rows {
		if r.ID == item.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("入队行 id=%d 不在 ListByStage 读回集里", item.ID)
	}
}
