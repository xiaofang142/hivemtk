package service

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 历史队列直接写库，刻意不经过新生产者 gate；消费拒绝不得回收/耗尽/认领这些事实。
func TestOutboundDelivery_PollDenialPreservesHistoricalRows(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		for _, denial := range submitDenialCases() {
			t.Run(msgType+"/"+denial.name, func(t *testing.T) {
				db := testutil.NewTestDB(t, &model.MessageHub{})
				svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
				ctx := tenant.WithTenantID(context.Background(), 501)
				stale := time.Now().Add(-time.Hour)
				for i, status := range []string{"pending", "inflight", "pending"} {
					h := &model.MessageHub{TenantID: 501, MsgID: fmt.Sprintf("historical-%d", i), Platform: "douyin_shop", AccountID: "historical-account", ConversationID: "history", Direction: "outbound", MsgType: msgType, Status: status, Content: "historical", PushAttempts: i}
					if status == "inflight" {
						h.ClaimedAt = &stale
					}
					if i == 2 {
						h.PushAttempts = repository.MaxOutboundPushAttempts
					}
					if err := db.WithContext(ctx).Create(h).Error; err != nil {
						t.Fatal(err)
					}
				}
				var before, after []model.MessageHub
				if err := db.WithContext(ctx).Order("id").Find(&before).Error; err != nil {
					t.Fatal(err)
				}
				var mutations atomic.Int32
				if err := db.Callback().Update().Before("gorm:update").Register("ec01-no-claim", func(*gorm.DB) { mutations.Add(1) }); err != nil {
					t.Fatal(err)
				}
				action := "message." + msgType + ".send"
				binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "historical-account", action, capregistry.ModeBridge)
				rows, err := svc.ClaimPendingOutbound(denial.context(binding, evidence), "douyin_shop", "historical-account", 10)
				if len(rows) != 0 {
					t.Errorf("拒绝不能领取历史行，实际 %d 条", len(rows))
				}
				if reason, ok := capabilityGateReason(err); !ok || reason != denial.reason {
					t.Errorf("拒绝原因期望 %s，实际 %v", denial.reason, err)
				}
				delivery := NewOutboundDeliveryService(svc.hubRepo, capregistry.AuthorizeBridgeSubmission)
				for _, row := range before {
					claimed, err := delivery.ClaimOutboundForPush(denial.context(binding, evidence), uint64(row.ID), row.Platform, row.AccountID, row.MsgType, InboxOutboundClaimTimeout)
					if reason, ok := capabilityGateReason(err); claimed || !ok || reason != denial.reason {
						t.Errorf("单行推送必须先返回 %s，claimed=%v err=%v", denial.reason, claimed, err)
					}
				}
				if err := db.WithContext(ctx).Order("id").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if mutations.Load() != 0 || !reflect.DeepEqual(before, after) {
					t.Fatalf("拒绝不得认领/回收/耗尽/修改历史行，updates=%d before=%+v after=%+v", mutations.Load(), before, after)
				}
			})
		}
	}
}

// 混合队列不能以文本授权消费图片；正确的单动作队列保留认领与 ACK 机制。
func TestOutboundDelivery_PollUsesStoredMessageTypeAndCurrentEvidence(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		t.Run(msgType, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
			ctx := tenant.WithTenantID(context.Background(), 501)
			h := &model.MessageHub{TenantID: 501, MsgID: "exact-history", Platform: "douyin_shop", AccountID: "exact-account", Direction: "outbound", Status: "pending", MsgType: msgType, Content: "history"}
			if err := db.WithContext(ctx).Create(h).Error; err != nil {
				t.Fatal(err)
			}
			otherType := "image"
			if msgType == "image" {
				otherType = "text"
			}
			wrong := localBridgeSubmitContext(t, ctx, h.Platform, h.AccountID, otherType)
			rows, err := svc.ClaimPendingOutbound(wrong, h.Platform, h.AccountID, 10)
			if reason, ok := capabilityGateReason(err); len(rows) != 0 || !ok || reason != "request_action_mismatch" {
				t.Fatalf("不能用 %s 授权消费 %s，rows=%d err=%v", otherType, msgType, len(rows), err)
			}
			valid := localBridgeSubmitContext(t, ctx, h.Platform, h.AccountID, msgType)
			rows, err = svc.ClaimPendingOutbound(valid, h.Platform, h.AccountID, 10)
			if err != nil || len(rows) != 1 || rows[0].Status != "inflight" || rows[0].PushAttempts != 1 || rows[0].ClaimedAt == nil {
				t.Fatalf("匹配的本地动作应走原认领机制，rows=%+v err=%v", rows, err)
			}
			stale := time.Now().Add(-time.Hour)
			if err := db.WithContext(ctx).Model(h).Update("claimed_at", stale).Error; err != nil {
				t.Fatal(err)
			}
			var before, after model.MessageHub
			if err := db.WithContext(ctx).First(&before, h.ID).Error; err != nil {
				t.Fatal(err)
			}
			binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, h.Platform, h.AccountID, "message."+msgType+".send", capregistry.ModeBridge)
			evidence.Revoked = true
			rows, err = svc.ClaimPendingOutbound(localSubmitContext(binding, evidence), h.Platform, h.AccountID, 10)
			requireSubmitDenial(t, err, "permission_revoked")
			if err := db.WithContext(ctx).First(&after, h.ID).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 || !reflect.DeepEqual(before, after) {
				t.Fatal("撤权后不得复用之前认领许可或回收旧租约")
			}
			rows, err = svc.ClaimPendingOutbound(valid, h.Platform, h.AccountID, 10)
			if err != nil || len(rows) != 1 || rows[0].PushAttempts != 2 {
				t.Fatalf("恢复显式本地证据后仍须沿用重领预算，rows=%+v err=%v", rows, err)
			}
			if n, err := svc.AckOutboundDelivered(valid, h.Platform, h.AccountID, []string{h.MsgID}); err != nil || n != 1 {
				t.Fatalf("原ACK机制应保留，n=%d err=%v", n, err)
			}
		})
	}
}

// 原始公开方法的 outbound/default 分支也须在改输入或创建行之前裁决，inbound 分支不受影响。
func TestOutboundDelivery_RawProducerGateDoesNotAffectInbound(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
	ctx := tenant.WithTenantID(context.Background(), 501)
	previous := GlobalSSEPublisher
	GlobalSSEPublisher = nil
	t.Cleanup(func() { GlobalSSEPublisher = previous })
	for _, direction := range []string{"", "outbound"} {
		h := &model.MessageHub{TenantID: 501, MsgID: "raw-" + direction, Platform: "douyin_shop", AccountID: "raw-account", Direction: direction, MsgType: "image", Content: "raw"}
		before := *h
		err := svc.DeliverOutbound(ctx, h)
		if reason, ok := capabilityGateReason(err); !ok || reason != "capability_mapping_missing" {
			t.Errorf("原始出站入口缺映射必须拒绝，err=%v", err)
		}
		if !reflect.DeepEqual(before, *h) {
			t.Error("拒绝前不得给入参伪造状态/成功时间/行ID")
		}
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.MessageHub{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("拒绝须零落库，count=%d err=%v", count, err)
	}
	h := &model.MessageHub{TenantID: 501, MsgID: "raw-inbound", Platform: "douyin_shop", AccountID: "raw-account", Direction: "inbound", Content: "inbound"}
	if err := svc.DeliverOutbound(ctx, h); err != nil || h.ID == 0 {
		t.Fatalf("不应改变入站分支，id=%d err=%v", h.ID, err)
	}
	for _, msgType := range []string{"text", "image"} {
		h := &model.MessageHub{TenantID: 501, MsgID: "raw-valid-" + msgType, Platform: "douyin_shop", AccountID: "raw-account", MsgType: msgType, Content: "raw"}
		valid := localBridgeSubmitContext(t, ctx, h.Platform, h.AccountID, msgType)
		if err := svc.DeliverOutbound(valid, h); err != nil || h.ID == 0 || h.Direction != "outbound" || h.Status != "pending" {
			t.Fatalf("精确本地许可应保留原始入队行为，hub=%+v err=%v", h, err)
		}
	}
}

// 审核锁定集合后并发入队的图片不能借文本许可混入本次认领；下一轮须重新裁决。
func TestOutboundDelivery_ApprovedSnapshotExcludesNewRows(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
	ctx := tenant.WithTenantID(context.Background(), 501)
	text := &model.MessageHub{TenantID: 501, Platform: "douyin_shop", AccountID: "snapshot-account", Direction: "outbound", Status: "pending", MsgID: "snapshot-text", MsgType: "text"}
	if err := db.WithContext(ctx).Create(text).Error; err != nil {
		t.Fatal(err)
	}
	image := *text
	image.ID = 0
	image.MsgID = "snapshot-image"
	image.MsgType = "image"
	var inserted atomic.Bool
	if err := db.Callback().Query().After("gorm:query").Register("ec01-concurrent-enqueue", func(tx *gorm.DB) {
		if tx.Statement.Table == "message_hub" && strings.Contains(tx.Statement.SQL.String(), "FOR UPDATE") && inserted.CompareAndSwap(false, true) {
			if err := db.WithContext(ctx).Create(&image).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	valid := localBridgeSubmitContext(t, ctx, text.Platform, text.AccountID, "text")
	rows, err := svc.ClaimPendingOutbound(valid, text.Platform, text.AccountID, 10)
	if err != nil || len(rows) != 1 || rows[0].ID != text.ID {
		t.Fatalf("本次只准认领已审核文本，rows=%+v err=%v", rows, err)
	}
	var before, after model.MessageHub
	if err := db.WithContext(ctx).First(&before, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if before.Status != "pending" || before.PushAttempts != 0 || before.ClaimedAt != nil {
		t.Fatalf("新图片不能借文本授权被认领，实际 %+v", before)
	}
	rows, err = svc.ClaimPendingOutbound(valid, text.Platform, text.AccountID, 10)
	requireSubmitDenial(t, err, "request_action_mismatch")
	if err := db.WithContext(ctx).First(&after, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 || !reflect.DeepEqual(before, after) {
		t.Fatal("下一轮混合动作拒绝应保留新行")
	}
}

// 普通租户以外的范围不能访问消费事实；另一个租户的同名账号行不能被认领。
func TestOutboundDelivery_ScopeAndTenantIsolation(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
	delivery := NewOutboundDeliveryService(svc.hubRepo, capregistry.AuthorizeBridgeSubmission)
	ctx := tenant.WithTenantID(context.Background(), 501)
	h := &model.MessageHub{TenantID: 501, Platform: "douyin_shop", AccountID: "scope-account", Direction: "outbound", Status: "pending", MsgID: "scope-text", MsgType: "text"}
	if err := db.WithContext(ctx).Create(h).Error; err != nil {
		t.Fatal(err)
	}
	valid := localBridgeSubmitContext(t, ctx, h.Platform, h.AccountID, "text")
	for _, denied := range []context.Context{context.Background(), tenant.WithSystemScope(valid)} {
		rows, err := svc.ClaimPendingOutbound(denied, h.Platform, h.AccountID, 10)
		requireSubmitDenial(t, err, "tenant_scope_invalid")
		claimed, err := delivery.ClaimOutboundForPush(denied, uint64(h.ID), h.Platform, h.AccountID, "text", InboxOutboundClaimTimeout)
		requireSubmitDenial(t, err, "tenant_scope_invalid")
		if len(rows) != 0 || claimed {
			t.Fatal("无租户/系统范围不得认领")
		}
	}
	other := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 502), h.Platform, h.AccountID, "text")
	if claimed, err := delivery.ClaimOutboundForPush(other, uint64(h.ID), h.Platform, h.AccountID, "text", InboxOutboundClaimTimeout); claimed || err != nil {
		t.Fatalf("异租户行应不可见，claimed=%v err=%v", claimed, err)
	}
	var after model.MessageHub
	if err := db.WithContext(ctx).First(&after, h.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != "pending" || after.PushAttempts != 0 || after.ClaimedAt != nil {
		t.Fatalf("范围拒绝不得修改队列，实际 %+v", after)
	}
}

// 后台巡检不能把被拒绝的待发事实改成 pending/failed/expired，系统范围也不能替代店铺授权。
func TestOutboundDelivery_ReclaimPreservesDeniedHistoricalRows(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		for _, denial := range submitDenialCases() {
			t.Run(msgType+"/"+denial.name, func(t *testing.T) {
				db := testutil.NewTestDB(t, &model.MessageHub{})
				task := NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission)
				ctx := tenant.WithTenantID(context.Background(), 501)
				stale := time.Now().Add(-2 * OutboundStaleAfterDefault)
				for i, status := range []string{"inflight", "pending", "pending"} {
					h := model.MessageHub{TenantID: 501, Platform: "douyin_shop", AccountID: "reclaim-account", Direction: "outbound", Status: status, MsgID: fmt.Sprintf("reclaim-%d", i), MsgType: msgType, CreatedAt: stale, PushAttempts: i}
					if i == 0 {
						h.ClaimedAt = &stale
					}
					if i == 1 {
						h.PushAttempts = repository.MaxOutboundPushAttempts
					}
					if err := db.WithContext(ctx).Create(&h).Error; err != nil {
						t.Fatal(err)
					}
				}
				var before, after []model.MessageHub
				if err := db.WithContext(ctx).Order("id").Find(&before).Error; err != nil {
					t.Fatal(err)
				}
				var updates atomic.Int32
				if err := db.Callback().Update().Before("gorm:update").Register("ec01-reclaim-no-mutation", func(*gorm.DB) { updates.Add(1) }); err != nil {
					t.Fatal(err)
				}
				binding, evidence := localSubmitFixture(t, capregistry.SubmissionSourceBridge, "douyin_shop", "reclaim-account", "message."+msgType+".send", capregistry.ModeBridge)
				r, e, a, err := task.ReclaimOutbound(denial.context(binding, evidence), InboxOutboundClaimTimeout, OutboundStaleAfterDefault)
				if reason, ok := capabilityGateReason(err); !ok || reason != denial.reason {
					t.Errorf("巡检拒绝原因期望 %s，实际 %v", denial.reason, err)
				}
				if err := db.WithContext(ctx).Order("id").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if r != 0 || e != 0 || a != 0 || updates.Load() != 0 || !reflect.DeepEqual(before, after) {
					t.Fatalf("巡检拒绝必须保留队列事实，reclaimed=%d exhausted=%d aged=%d updates=%d", r, e, a, updates.Load())
				}
			})
		}
	}
}

// 显式本地精确许可下保留原有三步巡检语义；后台空范围/系统范围不能访问队列。
func TestOutboundDelivery_ReclaimRequiresTenantAndPreservesAuthorizedMechanics(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	task := NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission)
	ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "reclaim-account", "text")
	stale := time.Now().Add(-2 * OutboundStaleAfterDefault)
	for i, status := range []string{"inflight", "pending", "pending"} {
		h := model.MessageHub{TenantID: 501, Platform: "douyin_shop", AccountID: "reclaim-account", Direction: "outbound", Status: status, MsgID: fmt.Sprintf("reclaim-valid-%d", i), MsgType: "text"}
		if i == 0 {
			h.ClaimedAt = &stale
		}
		if i == 1 {
			h.PushAttempts = repository.MaxOutboundPushAttempts
		}
		if i == 2 {
			h.CreatedAt = stale
		}
		if err := db.WithContext(ctx).Create(&h).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, denied := range []context.Context{context.Background(), tenant.WithSystemScope(ctx)} {
		r, e, a, err := task.ReclaimOutbound(denied, InboxOutboundClaimTimeout, OutboundStaleAfterDefault)
		requireSubmitDenial(t, err, "tenant_scope_invalid")
		if r != 0 || e != 0 || a != 0 {
			t.Fatalf("无租户/系统范围不能巡检队列，实际 %d/%d/%d", r, e, a)
		}
	}
	r, e, a, err := task.ReclaimOutbound(ctx, InboxOutboundClaimTimeout, OutboundStaleAfterDefault)
	if err != nil || r != 1 || e != 1 || a != 1 {
		t.Fatalf("本地许可应保留三步巡检，实际 %d/%d/%d err=%v", r, e, a, err)
	}
}
