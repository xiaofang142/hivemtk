package service

// intent_feedback_test.go TB-09：纠错反馈闭环 E2E（真库）。
//
// 判据：一条错误识别日志被纠错后，同类话术下次直接命中正确意图（规则层）。
// 负例：纠正目标不在词典→4000；同意图→4009；空操作者→4000。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestTB09_FeedbackLoop_E2E(t *testing.T) {
	db := testutil.NewTestDB(t, &model.IntentLog{}, &model.CustomIntent{}, &model.SystemConfigKV{})
	ctx := tenant.ContextWithDefault(context.Background())

	// 1) 预置一条错判日志（客服话术被误判为 price_inquiry）
	logRepo := repository.NewIntentLogRepository()
	rec := &model.IntentLog{
		TenantID: 1, Message: "我的包裹咋还没到啊", IntentMajor: IntentPriceInquiry, Confidence: 0.4,
	}
	if err := logRepo.Create(ctx, rec); err != nil {
		t.Fatalf("预置日志失败: %v", err)
	}

	svc := NewIntentFeedbackService()
	// 自定义意图服务注入（纠正目标可为自定义词条）
	ciSvc := NewCustomIntentService(repository.NewCustomIntentRepositoryWithDB(db))
	SetCustomIntentService(ciSvc)
	svc.SetCustomIntentService(ciSvc)
	t.Cleanup(func() { SetCustomIntentService(nil) })

	// 2) 真识别器：纠错前 "我的包裹咋还没到啊" 不命中 ask_service（词面无覆盖）
	rec2 := NewIntentRecognizer(nil, nil)
	if r := rec2.recognizeByRule(ctx, "我的包裹咋还没到啊"); r != nil && r.IntentType == IntentAskService {
		t.Fatal("纠错前不应命中 ask_service")
	}

	// 3) 纠错：日志改判 ask_service（原文作词面）
	out, err := svc.CorrectIntent(ctx, IntentFeedbackCorrectInput{LogID: rec.ID, CorrectedType: IntentAskService, Operator: "admin"})
	if err != nil {
		t.Fatalf("纠错失败: %v", err)
	}
	if out["keyword"] != "我的包裹咋还没到啊" {
		t.Fatalf("词面应取日志原文, got %v", out)
	}

	// 4) 热生效：同话术下次直接规则命中正确意图（闭环判据）
	r3 := rec2.recognizeByRule(ctx, "我的包裹咋还没到啊")
	if r3 == nil || r3.IntentType != IntentAskService || r3.Method != "rule" {
		t.Fatalf("纠错后应直接规则命中, got %+v", r3)
	}

	// 5) 负例矩阵
	if _, err := svc.CorrectIntent(ctx, IntentFeedbackCorrectInput{LogID: rec.ID, CorrectedType: "made_up", Operator: "admin"}); err == nil {
		t.Fatal("纠正目标不在词典应拒")
	}
	if _, err := svc.CorrectIntent(ctx, IntentFeedbackCorrectInput{LogID: rec.ID, CorrectedType: IntentPriceInquiry, Operator: "admin"}); err == nil {
		t.Fatal("纠正目标=日志原意图（无纠错必要）应 4009")
	}
	// 幂等：同一日志重复纠错同目标→去重后仍成功（不重复写词面）
	if _, err := svc.CorrectIntent(ctx, IntentFeedbackCorrectInput{LogID: rec.ID, CorrectedType: IntentAskService, Operator: "admin"}); err != nil {
		t.Fatalf("重复纠错应幂等成功: %v", err)
	}
	if _, err := svc.CorrectIntent(ctx, IntentFeedbackCorrectInput{LogID: rec.ID, CorrectedType: IntentPriceInquiry, Operator: ""}); err == nil {
		t.Fatal("空操作者应拒")
	}
}
