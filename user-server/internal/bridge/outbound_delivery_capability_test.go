package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// This adapter intentionally exposes the legacy cursor and claim ports alone.
type cursorOnlyDelivery struct {
	OutboxQuerier
	OutboundPushClaimer
}

func TestOutboundDelivery_PollHTTPReturnsCommittedClaims(t *testing.T) {
	db := setupBridgeTestDB(t, &model.MessageHub{})
	const account = "partial-http"
	ctx := localDeliveryContext(t, tenant.WithTenantID(context.Background(), 501), account, "text", "")
	rows := make([]model.MessageHub, repository.OutboundDeliveryPageSize+1)
	for i := range rows {
		rows[i] = model.MessageHub{TenantID: 501, MsgID: fmt.Sprintf("partial-http-%d", i), Platform: "douyin_shop",
			AccountID: account, Direction: "outbound", Status: "pending", MsgType: "image"}
	}
	rows[0].MsgType = "text"
	if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	pages := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:fail_later_http_delivery_page", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if tx.Statement.Table == "message_hub" && strings.Contains(sql, "FOR UPDATE") && !strings.Contains(sql, "SKIP LOCKED") {
			pages++
			if pages == 2 {
				tx.AddError(errors.New("later HTTP delivery page failed"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/bridge/outbox?channel=douyin_shop&account_id="+account+"&limit=2", nil).WithContext(ctx)
	NewBridgeIngestHandler(service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)).GetBridgeOutbox(c)
	var body struct {
		Messages []struct {
			MsgID string `json:"msg_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || pages != 2 ||
		len(body.Messages) != 1 || body.Messages[0].MsgID != rows[0].MsgID {
		t.Fatalf("expected committed claim in HTTP response after second page failed: pages=%d code=%d body=%s err=%v", pages, w.Code, w.Body, err)
	}
}

func TestOutboundDelivery_SSEMixedRowsRecoverBelowCursor(t *testing.T) {
	for _, mode := range []string{"owed", "cursor-only"} {
		t.Run(mode, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			ctx := tenant.WithTenantID(context.Background(), 501)
			bus := withBus(t)
			const account = "mixed-sse"
			rows := []model.MessageHub{
				{TenantID: 501, MsgID: "mixed-image", Platform: "douyin_shop", AccountID: account, Direction: "outbound", Status: "pending", MsgType: "image"},
				{TenantID: 501, MsgID: "mixed-text", Platform: "douyin_shop", AccountID: account, Direction: "outbound", Status: "pending", MsgType: "text"},
			}
			if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewMessageHubRepositoryWithDB(db)
			delivery := service.NewOutboundDeliveryService(repo, capregistry.AuthorizeBridgeSubmission)
			h := NewBridgeIngestHandler(nil)
			textCtx := localDeliveryContext(t, ctx, account, "text", "")
			imageCtx := localDeliveryContext(t, ctx, account, "image", "")
			imageID, textID := strconv.Itoa(int(rows[0].ID)), strconv.Itoa(int(rows[1].ID))
			if mode == "cursor-only" {
				// The adapter uses the raw cursor query, so the claim itself sees the denied gap.
				h.SetOutboxQuerier(cursorOnlyDelivery{repo, delivery})
				ch, cancel := bus.Subscribe("douyin_shop", account)
				defer cancel()
				bus.PublishCtx(textCtx, buildOutboundEvent(rows[1]))
				select {
				case ev := <-ch:
					t.Errorf("live event bypassed the cursor-only gap: %+v", ev)
				default:
				}
				events, cursor, _ := h.outboxFetcher.FetchOutboxSince(textCtx, "douyin_shop", account, "0")
				if len(events) != 0 || cursor != "0" {
					t.Errorf("cursor-only query crossed a denied row: events=%v cursor=%q", eventIDs(events), cursor)
				}
				var text model.MessageHub
				if err := db.WithContext(ctx).First(&text, rows[1].ID).Error; err != nil || text.Status != "pending" || text.PushAttempts != 0 {
					t.Errorf("cursor-only bypass must leave newer row pending: row=%+v err=%v", text, err)
				}
				return
			}
			h.SetOutboxQuerier(delivery)
			events, cursor, err := h.outboxFetcher.FetchOutboxSince(textCtx, "douyin_shop", account, "0")
			if err != nil || len(events) != 1 || events[0].ID != textID || cursor != textID {
				t.Fatalf("owed query should deliver newer authorized text: events=%v cursor=%q err=%v", eventIDs(events), cursor, err)
			}
			var image model.MessageHub
			if err := db.WithContext(ctx).First(&image, rows[0].ID).Error; err != nil || image.Status != "pending" || image.ClaimedAt != nil || image.PushAttempts != 0 || !image.SentAt.IsZero() {
				t.Fatalf("denied image changed: row=%+v err=%v", image, err)
			}
			events, _, err = h.outboxFetcher.FetchOutboxSince(imageCtx, "douyin_shop", account, cursor)
			if err != nil || len(events) != 1 || events[0].ID != imageID {
				t.Fatalf("restored authorization should deliver older image below cursor: events=%v err=%v", eventIDs(events), err)
			}
		})
	}
}

// 合成许可只用于本机队列测试；必须显式匹配真实登记的每种动作，不作为生产装配。
func localDeliveryContext(t *testing.T, ctx context.Context, account, msgType, denial string) context.Context {
	return localBridgeDeliveryContext(t, ctx, "douyin_shop", account, msgType, denial)
}

func localBridgeDeliveryContext(t *testing.T, ctx context.Context, product, account, msgType, denial string) context.Context {
	t.Helper()
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		t.Fatal("本地正例必须显式提供普通租户")
	}
	key := capregistry.CellKey{TenantID: tenantID, ShopID: "local-history-shop", Product: product, Region: "CN", Action: "message." + msgType + ".send", Carrier: capregistry.ModeBridge}
	definition, ok := capregistry.FindCellDefinition(key.Product, key.Region, key.Action, key.Carrier)
	if !ok {
		t.Fatalf("本地正例必须使用真实登记格，实际 %+v", key)
	}
	binding := capregistry.SubmissionBinding{Source: capregistry.SubmissionSourceBridge, Platform: key.Product, ResourceID: account, Key: key}
	expires := time.Now().Add(time.Hour)
	evidence := capregistry.CellEvidence{TenantID: key.TenantID, ShopID: key.ShopID, Product: key.Product, Region: key.Region, Action: key.Action, Carrier: key.Carrier, ConnectionState: capregistry.ConnectionReady, Scopes: definition.RequiredScopes, Version: definition.ContractVersion, DocumentationVersion: definition.DocumentationVersion, ExpiresAt: &expires, Evidence: []capregistry.EvidenceRecord{{Kind: capregistry.EvidenceDocumentation}, {Kind: capregistry.EvidenceImplementation}, {Kind: capregistry.EvidenceRuntime}}}
	if denial == "mapping" {
		return ctx
	}
	ctx = capregistry.WithSubmissionBinding(ctx, binding)
	if denial == "evidence" {
		return ctx
	}
	switch denial {
	case "revoked":
		evidence.Revoked = true
	case "expired":
		expired := time.Now().Add(-time.Second)
		evidence.ExpiresAt = &expired
	case "contract":
		evidence.Version = "stale"
	case "documentation":
		evidence.DocumentationVersion = "stale"
	}
	return capregistry.WithCellEvidence(ctx, evidence)
}

// 实际仓储装配后，总线和补拉都必须在认领、耗尽、回收、发布之前拒绝无当前授权的历史行。
func TestOutboundDelivery_SSEPreservesDeniedHistoricalRows(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		for _, denial := range []string{"mapping", "evidence", "revoked", "expired", "contract", "documentation"} {
			t.Run(msgType+"/"+denial, func(t *testing.T) {
				db := testutil.NewTestDB(t, &model.MessageHub{})
				ctx := tenant.WithTenantID(context.Background(), 501)
				bus := withBus(t)
				handler := NewBridgeIngestHandler(service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission))
				handler.SetOutboxQuerier(service.NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission))
				ch, cancel := bus.Subscribe("douyin_shop", "history-account")
				defer cancel()
				stale := time.Now().Add(-time.Hour)
				for i, status := range []string{"pending", "inflight", "pending"} {
					h := &model.MessageHub{TenantID: 501, MsgID: fmt.Sprintf("sse-history-%d", i), Platform: "douyin_shop", AccountID: "history-account", ConversationID: "history", Direction: "outbound", MsgType: msgType, Status: status, Content: "history", PushAttempts: i}
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
				var updates atomic.Int32
				if err := db.Callback().Update().Before("gorm:update").Register("ec01-sse-no-mutation", func(*gorm.DB) { updates.Add(1) }); err != nil {
					t.Fatal(err)
				}
				deniedCtx := localDeliveryContext(t, ctx, "history-account", msgType, denial)
				for _, row := range before {
					bus.PublishCtx(deniedCtx, buildOutboundEvent(row))
				}
				select {
				case ev := <-ch:
					t.Errorf("拒绝历史行不得发布，实际 %+v", ev)
				default:
				}
				events, _, err := handler.outboxFetcher.FetchOutboxSince(deniedCtx, "douyin_shop", "history-account", "0")
				if len(events) != 0 {
					t.Errorf("拒绝历史行不得补拉，实际 %d 条 err=%v", len(events), err)
				}
				reasons := map[string]string{"mapping": "capability_mapping_missing", "evidence": "capability_evidence_missing", "revoked": "permission_revoked", "expired": "evidence_expired", "contract": "stale_contract_version", "documentation": "stale_documentation_version"}
				if denied, ok := err.(interface{ CapabilityReasonCode() string }); !ok || denied.CapabilityReasonCode() != reasons[denial] {
					t.Errorf("补拉必须返回机器可读阻塞原因 %s，实际 %v", reasons[denial], err)
				}
				if err := db.WithContext(ctx).Order("id").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if updates.Load() != 0 || !reflect.DeepEqual(before, after) {
					t.Fatalf("SSE拒绝不得改认领租约/预算/状态，updates=%d before=%+v after=%+v", updates.Load(), before, after)
				}
			})
		}
	}
}

// SSE 以库中类型而非事件声称的类型授权；允许的本地动作仍认领一次、保持互斥并可ACK。
func TestOutboundDelivery_SSEStoredTypeAndEvidenceRechecked(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		t.Run(msgType, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			ctx := tenant.WithTenantID(context.Background(), 501)
			bus := withBus(t)
			ingress := service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)
			handler := NewBridgeIngestHandler(ingress)
			handler.SetOutboxQuerier(service.NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission))
			ch, cancel := bus.Subscribe("douyin_shop", "exact-account")
			defer cancel()
			h := model.MessageHub{TenantID: 501, MsgID: "sse-exact", Platform: "douyin_shop", AccountID: "exact-account", Direction: "outbound", Status: "pending", MsgType: msgType, Content: "history"}
			if err := db.WithContext(ctx).Create(&h).Error; err != nil {
				t.Fatal(err)
			}
			other := "image"
			if msgType == "image" {
				other = "text"
			}
			forged := buildOutboundEvent(h)
			forged.MsgType = other
			forged.Data["msg_type"] = other
			bus.PublishCtx(localDeliveryContext(t, ctx, h.AccountID, other, ""), forged)
			select {
			case ev := <-ch:
				t.Fatalf("不能相信事件类型而越过库中 %s 动作，实际 %+v", msgType, ev)
			default:
			}
			valid := localDeliveryContext(t, ctx, h.AccountID, msgType, "")
			// 即使许可匹配库中动作，事件自身也不能换类型/账号后占用这条行。
			bus.PublishCtx(valid, forged)
			select {
			case ev := <-ch:
				t.Fatalf("合法行许可不能给伪造事件载荷放行，实际 %+v", ev)
			default:
			}
			bus.PublishCtx(valid, buildOutboundEvent(h))
			select {
			case <-ch:
			default:
				t.Fatal("精确本地许可应可使用总线认领/发布机制")
			}
			bus.PublishCtx(valid, buildOutboundEvent(h))
			select {
			case <-ch:
				t.Fatal("新鲜租约不得重复推送")
			default:
			}
			stale := time.Now().Add(-time.Hour)
			if err := db.WithContext(ctx).Model(&h).Update("claimed_at", stale).Error; err != nil {
				t.Fatal(err)
			}
			var before, after model.MessageHub
			if err := db.WithContext(ctx).First(&before, h.ID).Error; err != nil {
				t.Fatal(err)
			}
			bus.PublishCtx(localDeliveryContext(t, ctx, h.AccountID, msgType, "revoked"), buildOutboundEvent(h))
			select {
			case <-ch:
				t.Fatal("撤权后不得复用旧许可发布")
			default:
			}
			if err := db.WithContext(ctx).First(&after, h.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("撤权后不得重领或改写过期租约")
			}
			events, _, err := handler.outboxFetcher.FetchOutboxSince(valid, h.Platform, h.AccountID, "999999")
			if err != nil || len(events) != 1 {
				t.Fatalf("正确许可应保留与游标无关的补拉机制，events=%+v err=%v", events, err)
			}
			if n, err := ingress.AckOutboundDelivered(valid, h.Platform, h.AccountID, []string{h.MsgID}); err != nil || n != 1 {
				t.Fatalf("原ACK应保留，n=%d err=%v", n, err)
			}
		})
	}
}

// 平台/账号错配及事件内外类型不一致均须在认领前拒绝，不能占用合法历史行的租约。
func TestOutboundDelivery_SSEEventIdentityMismatchPreservesRows(t *testing.T) {
	for _, msgType := range []string{"text", "image"} {
		t.Run(msgType, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			ctx := tenant.WithTenantID(context.Background(), 501)
			bus := withBus(t)
			repo := repository.NewMessageHubRepositoryWithDB(db)
			handler := NewBridgeIngestHandler(service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission))
			handler.SetOutboxQuerier(service.NewOutboundDeliveryService(repo, capregistry.AuthorizeBridgeSubmission))
			h := model.MessageHub{TenantID: 501, MsgID: "sse-identity", Platform: "douyin_shop", AccountID: "identity-account", Direction: "outbound", Status: "pending", MsgType: msgType, Content: "history"}
			if err := db.WithContext(ctx).Create(&h).Error; err != nil {
				t.Fatal(err)
			}
			var before model.MessageHub
			if err := db.WithContext(ctx).First(&before, h.ID).Error; err != nil {
				t.Fatal(err)
			}
			var updates atomic.Int32
			if err := db.Callback().Update().Before("gorm:update").Register("ec01-event-identity-no-mutation", func(*gorm.DB) { updates.Add(1) }); err != nil {
				t.Fatal(err)
			}
			valid := localDeliveryContext(t, ctx, h.AccountID, msgType, "")
			otherType := "image"
			if msgType == "image" {
				otherType = "text"
			}
			for _, field := range []string{"platform", "account_id", "top_msg_type", "data_msg_type"} {
				t.Run(field, func(t *testing.T) {
					ev := buildOutboundEvent(h)
					switch field {
					case "platform":
						ev.Data["platform"] = "douyin"
					case "account_id":
						ev.Data["account_id"] = "forged-account"
					case "top_msg_type":
						ev.MsgType = otherType
					case "data_msg_type":
						ev.Data["msg_type"] = otherType
					}
					channel := ev.Data["platform"].(string)
					account := ev.Data["account_id"].(string)
					ch, cancel := bus.Subscribe(channel, account)
					defer cancel()
					if field == "platform" || field == "account_id" {
						claimed, err := service.NewOutboundDeliveryService(repo, capregistry.AuthorizeBridgeSubmission).ClaimOutboundForPush(valid, uint64(h.ID), channel, account, msgType, service.InboxOutboundClaimTimeout)
						denied, ok := err.(interface{ CapabilityReasonCode() string })
						if claimed || !ok || denied.CapabilityReasonCode() != "delivery_binding_mismatch" {
							t.Fatalf("身份错配必须返回机器可读拒绝，claimed=%v err=%v", claimed, err)
						}
					}
					bus.PublishCtx(valid, ev)
					select {
					case published := <-ch:
						t.Fatalf("事件身份错配不得发布，实际 %+v", published)
					default:
					}
					var after model.MessageHub
					if err := db.WithContext(ctx).First(&after, h.ID).Error; err != nil {
						t.Fatal(err)
					}
					if updates.Load() != 0 || !reflect.DeepEqual(before, after) {
						t.Fatalf("事件身份拒绝必须保留历史队列事实，updates=%d", updates.Load())
					}
				})
			}
		})
	}
}

// 未装配任何认领/授权服务不能退回直接发布。
func TestOutboundDelivery_SSEWithoutClaimerDenies(t *testing.T) {
	bus := withBus(t)
	ch, cancel := bus.Subscribe("douyin", "acc_b11")
	defer cancel()
	bus.Publish(b11Event(1, "unconfigured"))
	select {
	case ev := <-ch:
		t.Fatalf("缺消费服务必须拒绝，实际发布 %+v", ev)
	default:
	}
}
