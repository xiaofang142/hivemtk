package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// fakeRecaller 记"出没出网"这件事。撤回的三条口径（幂等 / 分类 / 落格）
// 全都以这张计数为判据：只说"返回了 already_recalled"是不够的，
// 幂等真正的意思是**没有第二次调用平台**。
type fakeRecaller struct {
	calls atomic.Int64
	err   error
}

func (f *fakeRecaller) RecallHub(ctx context.Context, hub *model.MessageHub) error {
	f.calls.Add(1)
	return f.err
}

func setupRecallTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清空 message_hub: %v", err)
	}
	return db
}

func seedRecallRow(t *testing.T, db *gorm.DB, platform, msgID, direction string) *model.MessageHub {
	t.Helper()
	row := &model.MessageHub{
		Platform:       platform,
		AccountID:      "acc1",
		MsgID:          msgID,
		ConversationID: "conv-" + msgID,
		Direction:      direction,
		Status:         "sent",
		MsgType:        "text",
		Content:        "hello",
		SenderID:       "s1",
		ReceiverID:     "r1",
		SentAt:         time.Now(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed %s: %v", msgID, err)
	}
	return row
}

func newRecallSvc(db *gorm.DB, r ReachRecaller) *MessageHubService {
	svc := NewMessageHubService()
	svc.repo = repository.NewMessageHubRepositoryWithDB(db)
	return svc.WithReachRecaller(r)
}

// B2 验收判据（OPEN_ITEMS 第 1 条）：平台撤回一次能回查本地记录并置终态；重复撤回幂等。
func TestMessageHubServiceRecall(t *testing.T) {
	ctx := context.Background()

	t.Run("撤回成功落终态，重复调用不再出网", func(t *testing.T) {
		db := setupRecallTestDB(t)
		row := seedRecallRow(t, db, "telegram", "mh-ok", "outbound")
		rec := &fakeRecaller{}
		svc := newRecallSvc(db, rec)

		res, err := svc.Recall(ctx, row.ID)
		if err != nil {
			t.Fatalf("首次撤回: %v", err)
		}
		if !res.Recalled || res.AlreadyRecalled || res.RecalledAt == nil {
			t.Fatalf("首次撤回结论不对: %+v", res)
		}
		if got := rec.calls.Load(); got != 1 {
			t.Fatalf("首次撤回出网 %d 次, want 1", got)
		}

		// 判据"置终态"落在行上，不只是返回值
		var stored model.MessageHub
		if err := db.First(&stored, row.ID).Error; err != nil {
			t.Fatalf("回读: %v", err)
		}
		if stored.RecalledAt == nil {
			t.Fatalf("recalled_at 未落库")
		}

		// 判据"重复撤回幂等"
		res2, err := svc.Recall(ctx, row.ID)
		if err != nil {
			t.Fatalf("重复撤回: %v", err)
		}
		if !res2.Recalled || !res2.AlreadyRecalled {
			t.Fatalf("重复撤回应回 already_recalled, got %+v", res2)
		}
		if got := rec.calls.Load(); got != 1 {
			t.Fatalf("重复撤回又出网了（calls=%d）：幂等要落在本地终态上", got)
		}
	})

	t.Run("入站行不是撤回对象", func(t *testing.T) {
		db := setupRecallTestDB(t)
		row := seedRecallRow(t, db, "telegram", "mh-in", "inbound")
		rec := &fakeRecaller{}
		svc := newRecallSvc(db, rec)

		if _, err := svc.Recall(ctx, row.ID); !errors.Is(err, ErrMessageHubRecallInbound) {
			t.Fatalf("err = %v, want ErrMessageHubRecallInbound", err)
		}
		if got := rec.calls.Load(); got != 0 {
			t.Fatalf("入站行不该出网，calls=%d", got)
		}
	})

	t.Run("渠道不支持与窗口过期各回各的分类", func(t *testing.T) {
		cases := []struct {
			name string
			kind RecallFailureKind
			want error
		}{
			{"不支持的渠道", RecallKindNotSupported, ErrMessageHubRecallNotSupported},
			{"窗口过期", RecallKindWindowExpired, ErrMessageHubRecallWindowExpired},
			{"出网失败", RecallKindChannelError, ErrMessageHubRecallFailed},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				db := setupRecallTestDB(t)
				row := seedRecallRow(t, db, "telegram", "mh-kind", "outbound")
				svc := newRecallSvc(db, &fakeRecaller{err: &RecallFailure{Kind: tc.kind, Err: errors.New("platform says no")}})

				res, err := svc.Recall(ctx, row.ID)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if res == nil || res.Recalled {
					t.Fatalf("失败时不该报 recalled: %+v", res)
				}
				// 失败原因必须留痕：这是"点过但没成，为什么"的唯一出处
				var stored model.MessageHub
				if err := db.First(&stored, row.ID).Error; err != nil {
					t.Fatalf("回读: %v", err)
				}
				if stored.RecallError == "" {
					t.Fatalf("recall_error 未落库")
				}
				if stored.RecalledAt != nil {
					t.Fatalf("失败被写成了终态")
				}
			})
		}
	})

	t.Run("端口未装配与行不存在各自可区分", func(t *testing.T) {
		db := setupRecallTestDB(t)
		row := seedRecallRow(t, db, "telegram", "mh-unwired", "outbound")

		if _, err := newRecallSvc(db, nil).Recall(ctx, row.ID); !errors.Is(err, ErrMessageHubRecallUnavailable) {
			t.Fatalf("端口未装配 err = %v, want ErrMessageHubRecallUnavailable", err)
		}
		if _, err := newRecallSvc(db, &fakeRecaller{}).Recall(ctx, 99999999); !errors.Is(err, ErrMessageHubRowNotFound) {
			t.Fatalf("行不存在 err = %v, want ErrMessageHubRowNotFound", err)
		}
	})

	t.Run("失败后可以再试（失败不是终态）", func(t *testing.T) {
		db := setupRecallTestDB(t)
		row := seedRecallRow(t, db, "telegram", "mh-retry", "outbound")
		rec := &fakeRecaller{err: &RecallFailure{Kind: RecallKindChannelError, Err: errors.New("network down")}}
		svc := newRecallSvc(db, rec)

		if _, err := svc.Recall(ctx, row.ID); !errors.Is(err, ErrMessageHubRecallFailed) {
			t.Fatalf("第一次应失败: %v", err)
		}
		rec.err = nil // 网络恢复
		res, err := svc.Recall(ctx, row.ID)
		if err != nil {
			t.Fatalf("重试应成功: %v", err)
		}
		if !res.Recalled || res.RecalledAt == nil {
			t.Fatalf("重试结论不对: %+v", res)
		}
		if got := rec.calls.Load(); got != 2 {
			t.Fatalf("重试应再次出网（calls=%d, want 2）", got)
		}
		// 成功时把上次失败原因清掉，否则行上会同时挂着"已撤回"和一句失败
		var stored model.MessageHub
		if err := db.First(&stored, row.ID).Error; err != nil {
			t.Fatalf("回读: %v", err)
		}
		if stored.RecallError != "" {
			t.Fatalf("成功后 recall_error 未清空: %q", stored.RecallError)
		}
	})
}
