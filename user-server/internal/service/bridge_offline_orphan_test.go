package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 桥接出站孤儿结算的用例。
//
// 渠道名与账号名每次现取（带时间戳后缀）：测试库是常驻的，写死名字会让上一轮遗留的
// 同账号行混进本轮的分组计数里，断言就再也不是"这一批夹具"的数量。

type orphanFixture struct {
	svc    *BridgeOfflineReplayService
	db     *gorm.DB
	ch     string // 白名单内渠道
	chOut  string // 白名单外渠道（验证不误伤非桥接行）
	unreg  string // bridge_accounts 里没有这行
	stale  string // 注册过、但 last_sync_at 早于阈值
	fresh  string // 注册过且刚同步
	nonce  string
	ttl    time.Duration
	cutoff time.Time
}

func newOrphanFixture(t *testing.T) *orphanFixture {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.BridgeAccount{}, &model.ConfigParam{})
	if db == nil {
		t.Fatal("孤儿结算判的是候选集合与写库结果，无测试库等于没测")
	}
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	f := &orphanFixture{
		svc:    NewBridgeOfflineReplayService().WithDB(db),
		db:     db,
		ch:     "orph-" + nonce,
		chOut:  "orphx-" + nonce,
		unreg:  "unreg-" + nonce,
		stale:  "stale-" + nonce,
		fresh:  "fresh-" + nonce,
		nonce:  nonce,
		ttl:    24 * time.Hour,
		cutoff: time.Now(),
	}
	prevChannels := bridgeChannels
	SetBridgeChannels([]string{f.ch})
	t.Cleanup(func() { bridgeChannels = prevChannels })

	f.upsertAccount(t, f.ch, f.stale, f.cutoff.Add(-48*time.Hour))
	f.upsertAccount(t, f.ch, f.fresh, f.cutoff)
	return f
}

func (f *orphanFixture) upsertAccount(t *testing.T, channel, accountID string, lastSync time.Time) {
	t.Helper()
	sync := lastSync
	acc := model.BridgeAccount{
		Channel:    channel,
		AccountID:  accountID,
		Status:     "offline",
		LastSyncAt: &sync,
	}
	if err := f.db.Create(&acc).Error; err != nil {
		t.Fatalf("建桥接账号 %s/%s: %v", channel, accountID, err)
	}
}

// putRow 造一行出站消息；age 相对"现在"，status 落库时的状态。
func (f *orphanFixture) putRow(t *testing.T, platform, accountID, status string, age time.Duration) uint {
	t.Helper()
	created := time.Now().Add(-age)
	row := model.MessageHub{
		Platform:       platform,
		AccountID:      accountID,
		MsgID:          fmt.Sprintf("orphmsg-%s-%d", f.nonce, time.Now().UnixNano()),
		Direction:      "outbound",
		Status:         status,
		MsgType:        "text",
		Content:        "孤儿结算夹具",
		ConversationID: "conv-" + f.nonce,
		SentAt:         created,
		CreatedAt:      created,
	}
	if status == "inflight" {
		old := created
		row.ClaimedAt = &old
	}
	if err := f.db.Create(&row).Error; err != nil {
		t.Fatalf("建造试行: %v", err)
	}
	return row.ID
}

func (f *orphanFixture) statusOf(t *testing.T, id uint) model.MessageHub {
	t.Helper()
	// 读回必须用独立零值 struct：复用已填充的 struct 再 First() 会把旧字段并入 WHERE。
	// Unscoped 是必需的：夹具里有一行"已软删、结算不该碰"的消息，走默认作用域直接
	// record not found，判据就退化成"读不到"而不是"没被改成 failed"。
	var got model.MessageHub
	if err := f.db.Unscoped().Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatalf("读回行 %d: %v", id, err)
	}
	return got
}

func (f *orphanFixture) softDelete(t *testing.T, id uint) {
	t.Helper()
	if err := f.db.Model(&model.MessageHub{}).Where("id = ?", id).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("软删行 %d: %v", id, err)
	}
}

// seedSix 铺一遍完整形状：3 行应入候选、5 行不该动。
//
//	候选：未注册账号的 48h 旧 pending、旧账号已停同步的 48h pending、未注册账号的 48h inflight
//	不该动：账号刚同步过的旧行、行本身还新的、终态 delivered/failed、白名单外渠道、已软删的行
func (f *orphanFixture) seedSix(t *testing.T) (wantIDs, keepIDs []uint) {
	t.Helper()
	wantIDs = append(wantIDs, f.putRow(t, f.ch, f.unreg, "pending", 48*time.Hour))
	wantIDs = append(wantIDs, f.putRow(t, f.ch, f.stale, "pending", 48*time.Hour))
	wantIDs = append(wantIDs, f.putRow(t, f.ch, f.unreg, "inflight", 48*time.Hour))
	keepIDs = append(keepIDs, f.putRow(t, f.ch, f.fresh, "pending", 48*time.Hour))
	keepIDs = append(keepIDs, f.putRow(t, f.ch, f.unreg, "pending", time.Hour))
	keepIDs = append(keepIDs, f.putRow(t, f.ch, f.unreg, "delivered", 48*time.Hour))
	keepIDs = append(keepIDs, f.putRow(t, f.ch, f.unreg, "failed", 48*time.Hour))
	keepIDs = append(keepIDs, f.putRow(t, f.chOut, f.unreg, "pending", 48*time.Hour))
	id := f.putRow(t, f.ch, f.unreg, "pending", 48*time.Hour)
	f.softDelete(t, id)
	keepIDs = append(keepIDs, id)
	return wantIDs, keepIDs
}

func withOrphanProbe(t *testing.T, fn func(ctx context.Context, channel, accountID string) bool) {
	t.Helper()
	prev := loadBridgeChannelOnlineProbe()
	storeBridgeChannelOnlineProbe(fn)
	t.Cleanup(func() { storeBridgeChannelOnlineProbe(prev) })
}

func assertUnsettled(t *testing.T, f *orphanFixture, ids []uint, why string) {
	t.Helper()
	for _, id := range ids {
		got := f.statusOf(t, id)
		if got.Status == "failed" && got.PushError == bridgeOutboundOrphanReason {
			t.Errorf("%s：行 %d 不该被结算，实得 status=%s push_error=%q", why, id, got.Status, got.PushError)
		}
	}
}

func TestSettleOrphanBridgeOutbound_DryRunReportsWithoutWriting(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, keepIDs := f.seedSix(t)
	withOrphanProbe(t, func(context.Context, string, string) bool { return false })

	got, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), f.ttl, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !got.DryRun {
		t.Error("DryRun 标记未回传")
	}
	if got.Groups != 2 || got.CandidateRows != 3 {
		t.Errorf("dry-run 报数期望 groups=2 rows=3，实得 groups=%d rows=%d", got.Groups, got.CandidateRows)
	}
	if got.SettledRows != 0 {
		t.Errorf("dry-run 不该写库，实得 settled=%d", got.SettledRows)
	}
	assertUnsettled(t, f, append(wantIDs, keepIDs...), "dry-run 之后")
}

func TestSettleOrphanBridgeOutbound_SettlesOnlyOrphans(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, keepIDs := f.seedSix(t)
	withOrphanProbe(t, func(context.Context, string, string) bool { return false })

	got, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), f.ttl, false)
	if err != nil {
		t.Fatalf("结算: %v", err)
	}
	if got.SettledRows != 3 {
		t.Errorf("期望结算 3 行，实得 %d", got.SettledRows)
	}
	for _, id := range wantIDs {
		row := f.statusOf(t, id)
		if row.Status != "failed" || row.PushError != bridgeOutboundOrphanReason {
			t.Errorf("行 %d 期望 failed/%s，实得 %s/%q", id, bridgeOutboundOrphanReason, row.Status, row.PushError)
		}
		// 与 exhaustOutbound 同规格：租约清零、sent_at 保持建行时刻不被伪造成投递时刻。
		if row.ClaimedAt != nil {
			t.Errorf("行 %d 结算后 claimed_at 应清空，实得 %v", id, row.ClaimedAt)
		}
		if row.SentAt.After(f.cutoff) {
			t.Errorf("行 %d 结算不该写 sent_at，实得 %v", id, row.SentAt)
		}
	}
	assertUnsettled(t, f, keepIDs, "结算之后")
}

func TestSettleOrphanBridgeOutbound_ProbeSkipsReachableGroups(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, keepIDs := f.seedSix(t)
	// 未注册账号那组此刻挂着 SSE：探针说收得到，就不该烧。
	// wantIDs = [unreg-pending, stale-pending, unreg-inflight]，被放行的是第 1、3 行。
	withOrphanProbe(t, func(_ context.Context, _, accountID string) bool {
		return accountID == f.unreg
	})

	got, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), f.ttl, false)
	if err != nil {
		t.Fatalf("结算: %v", err)
	}
	// SkippedReachable 计的是行（字段名 skipped_reachable_rows），未注册那组 2 行都在内。
	if got.SkippedReachable != 2 || got.SettledRows != 1 {
		t.Errorf("期望 skip_reachable=2 settled=1，实得 skip=%d settled=%d", got.SkippedReachable, got.SettledRows)
	}
	if got.Groups != 2 {
		t.Errorf("候选组期望 2（未注册 + 停同步），实得 %d", got.Groups)
	}
	assertUnsettled(t, f, []uint{wantIDs[0], wantIDs[2]}, "探针放行未注册组之后")
	last := f.statusOf(t, wantIDs[1])
	if last.Status != "failed" || last.PushError != bridgeOutboundOrphanReason {
		t.Errorf("探针未放行的停同步组应被结算，实得 %s/%q", last.Status, last.PushError)
	}
	assertUnsettled(t, f, keepIDs, "探针放行后")
}

// 探针未注册时整轮不结算：这是不可逆写入，判据未知按"全部可达"处理。
func TestSettleOrphanBridgeOutbound_MissingProbeSettlesNothing(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, keepIDs := f.seedSix(t)
	withOrphanProbe(t, nil)

	got, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), f.ttl, false)
	if err != nil {
		t.Fatalf("结算: %v", err)
	}
	if !got.ProbeMissing || got.SettledRows != 0 {
		t.Errorf("期望 ProbeMissing 且零结算，实得 %+v", got)
	}
	if got.CandidateRows != 3 {
		t.Errorf("缺探针时也要报出候选行数供运维判断，实得 %d", got.CandidateRows)
	}
	assertUnsettled(t, f, append(wantIDs, keepIDs...), "缺探针轮次之后")
}

func TestSettleOrphanBridgeOutbound_NonPositiveTTLDisables(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, keepIDs := f.seedSix(t)
	withOrphanProbe(t, func(context.Context, string, string) bool { return false })

	for _, ttl := range []time.Duration{0, -time.Hour} {
		got, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), ttl, false)
		if err != nil {
			t.Fatalf("ttl=%v: %v", ttl, err)
		}
		if !got.SkippedDisabled || got.Groups != 0 {
			t.Errorf("ttl=%v 应整条停用，实得 %+v", ttl, got)
		}
	}
	assertUnsettled(t, f, append(wantIDs, keepIDs...), "停用配置之后")
}

// 报数与写入之间账号重新同步（真实扩展回来的形状）：写的那条 SQL 必须重算判定。
func TestSettleOrphanOutbound_SettlementRechecksLastSync(t *testing.T) {
	f := newOrphanFixture(t)
	ids := []uint{f.putRow(t, f.ch, f.stale, "pending", 48*time.Hour)}
	if _, err := f.svc.SettleOrphanBridgeOutbound(context.Background(), 0, true); err != nil {
		t.Fatalf("预热: %v", err)
	}
	// 假装这一轮里 f.stale 重新同步了，然后直接按"上一轮的分组快照"调仓储。
	resync := time.Now()
	if err := f.db.Model(&model.BridgeAccount{}).
		Where("channel = ? AND account_id = ?", f.ch, f.stale).
		Update("last_sync_at", resync).Error; err != nil {
		t.Fatalf("回写同步时间: %v", err)
	}
	settled, err := f.svc.repo.SettleOrphanOutbound(context.Background(),
		[]repository.OrphanOutboundGroup{{Platform: f.ch, AccountID: f.stale, Rows: 1}}, f.ttl,
		bridgeOutboundOrphanReason, 100)
	if err != nil {
		t.Fatalf("结算: %v", err)
	}
	if settled != 0 {
		t.Errorf("刚同步的账号一行都不该被结算，实得 %d", settled)
	}
	assertUnsettled(t, f, ids, "重新同步之后")
}

// withOrphanConfig 把全局配置实例重绑到本用例的库，并写一个 bridge 组的配置项。
//
// RunOnce 的阈值与"只报数"开关都只从全局单例读（那是运维唯一能改的旋钮），不绑定就等于
// 断言"上一个用例留在全局里的值"——本包里 TestSeedConfigParams 会把默认值 604800 写进同一槽库，
// 届时 48h 的夹具行根本不是候选，用例会以"结算没跑"的红因失败。
// 传空 value 表示"这一项不存在"，走代码缺省值那条分支。
func withOrphanConfig(t *testing.T, db *gorm.DB, key, value, valueType string) {
	t.Helper()
	// 先查后写，和 seed 路径同形状。不用 upsert：库里 idx_group_key 实际是
	// UNIQUE (key) 单列（见 DefaultParamDefs 的 key 全局唯一守卫），
	// ON CONFLICT (param_group, key) 找不到匹配约束，PG 直接 42P10。
	var existing model.ConfigParam
	err := db.Where("param_group = ? AND key = ?", "bridge", key).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if value == "" {
			break // 本就没有，正好测"配置缺项走代码缺省值"
		}
		row := model.ConfigParam{
			Group: "bridge", Key: key, Name: "孤儿结算用例配置", ValueType: valueType,
			Value: value, DefaultValue: value,
		}
		if cerr := db.Create(&row).Error; cerr != nil {
			t.Fatalf("写入 bridge.%s: %v", key, cerr)
		}
	case err != nil:
		t.Fatalf("读取 bridge.%s: %v", key, err)
	default:
		if value == "" {
			if derr := db.Delete(&model.ConfigParam{}, existing.ID).Error; derr != nil {
				t.Fatalf("删除 bridge.%s: %v", key, derr)
			}
			break
		}
		if uerr := db.Model(&model.ConfigParam{}).Where("id = ?", existing.ID).
			Update("param_value", value).Error; uerr != nil {
			t.Fatalf("改写 bridge.%s: %v", key, uerr)
		}
	}
	globalConfigParamMu.RLock()
	prev := globalConfigParam
	globalConfigParamMu.RUnlock()
	SetGlobalForTest(NewConfigParamService(db))
	t.Cleanup(func() {
		globalConfigParamMu.Lock()
		globalConfigParam = prev
		globalConfigParamMu.Unlock()
	})
}

func withOrphanTTLConfig(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	withOrphanConfig(t, db, bridgeOutboundOrphanParam, value, "duration")
}

func withOrphanDryRunConfig(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	withOrphanConfig(t, db, bridgeOutboundOrphanDryRunParam, value, "bool")
}

// RunOnce 必须真的把这道结算挂上：单独测方法、cron 里没调，等于白写。
// 断言同时钉住"阈值取自配置"这条：写 86400 而不是缺省的 7 天，夹具的 48h 行才是候选。
func TestRunOnce_CallsOrphanSettlement(t *testing.T) {
	f := newOrphanFixture(t)
	wantIDs, _ := f.seedSix(t)
	withOrphanProbe(t, func(context.Context, string, string) bool { return false })
	withOrphanTTLConfig(t, f.db, "86400")
	withOrphanDryRunConfig(t, f.db, "false")

	stats := f.svc.RunOnce(context.Background())
	if stats.OrphanCandidateRows != 3 {
		t.Errorf("RunOnce 里孤儿候选行期望 3，实得 %d（说明这道结算没被挂上或判定口径不一致）", stats.OrphanCandidateRows)
	}
	if stats.OrphanDryRun {
		t.Error("配置已置 dry_run=false，统计里却仍报 dry-run（说明开关没接到写入上）")
	}
	if stats.OrphanSettledRows != 3 {
		t.Errorf("RunOnce 里期望结算 3 行，实得 %d", stats.OrphanSettledRows)
	}
	for _, id := range wantIDs {
		if got := f.statusOf(t, id); got.Status != "failed" {
			t.Errorf("行 %d 应在 RunOnce 里落 failed，实得 %s", id, got.Status)
		}
	}
}

// 缺省必须是"只报数"：这道 UPDATE 不可逆，而回扫是自动跑的后台任务
// （开发态存盘即热重载进真实例、连真库，缺省会写＝上线即误烧）。
func TestRunOnce_OrphanSettlementDefaultsToDryRun(t *testing.T) {
	for _, cfg := range []struct{ name, value string }{
		{"配置缺项", ""},
		{"配置写 true", "true"},
	} {
		t.Run(cfg.name, func(t *testing.T) {
			f := newOrphanFixture(t)
			wantIDs, keepIDs := f.seedSix(t)
			withOrphanProbe(t, func(context.Context, string, string) bool { return false })
			withOrphanTTLConfig(t, f.db, "86400")
			withOrphanDryRunConfig(t, f.db, cfg.value)

			stats := f.svc.RunOnce(context.Background())
			if !stats.OrphanDryRun {
				t.Errorf("OrphanDryRun 期望 true，实得 false")
			}
			if stats.OrphanCandidateRows != 3 {
				t.Errorf("dry-run 也要报出候选行，期望 3，实得 %d", stats.OrphanCandidateRows)
			}
			if stats.OrphanSettledRows != 0 {
				t.Errorf("dry-run 不该写库，实得 settled=%d", stats.OrphanSettledRows)
			}
			assertUnsettled(t, f, append(wantIDs, keepIDs...), "dry-run 轮次之后")
		})
	}
}

// 配置读的是"秒"语义的 duration：缺省值 604800 必须能原样读回 7 天。
func TestBridgeOrphanOutboundTTL_ReadsConfiguredSeconds(t *testing.T) {
	f := newOrphanFixture(t)
	withOrphanTTLConfig(t, f.db, "86400")
	if got := bridgeOrphanOutboundTTL(context.Background()); got != 24*time.Hour {
		t.Errorf("配置写 86400 秒，读回期望 24h，实得 %s", got)
	}
	withOrphanTTLConfig(t, f.db, "0")
	if got := bridgeOrphanOutboundTTL(context.Background()); got != 0 {
		t.Errorf("配置写 0（运维口径＝停用），期望 0，实得 %s", got)
	}
}
