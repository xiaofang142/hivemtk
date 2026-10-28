// contact_consent_test.go D04（可信同意与故障拒发）测试。
// 验收口径（docs/assessment/functionality-2026-10-27/开发顺序与验收.md D04）：
//   - 撤回即生效：metadata 伪造 consent=true 压不过真实撤回事实；
//   - 排队后撤回不发送（延迟队列重发走同一管线再判）；
//   - DNC 库故障：主动外发拒发（fail-closed）并留原因码；
//   - 历史无凭据不批量伪造（revocation_only 放行但标 legacy_assertion）。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupConsentService(t *testing.T) (*ContactConsentService, *repository.ContactConsentRepository) {
	database := testutil.NewTestDB(t,
		&model.ContactConsentEvent{},
		&model.ContactPreference{},
	)
	repo := repository.NewContactConsentRepository(database)
	return NewContactConsentService(repo), repo
}

// 判定状态机：无凭据 missing → grant → granted → revoke → revoked（撤回即生效）。
func TestContactConsent_StateMachine(t *testing.T) {
	svc, _ := setupConsentService(t)
	ctx := context.Background()
	const oneID = "one-consent-1"

	// 无凭据
	dec := svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
	if dec.Status != ConsentStatusMissing || dec.Trusted {
		t.Fatalf("无凭据应 missing/不可信: %+v", dec)
	}

	// grant（全渠道）→ granted
	if err := svc.Record(ctx, ConsentRecordInput{
		OneID: oneID, Purpose: model.ConsentPurposeMarketing,
		Decision: model.ConsentDecisionGrant, Source: model.ConsentSourceAPI, Actor: "op-1",
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
	if dec.Status != ConsentStatusGranted || !dec.Trusted {
		t.Fatalf("grant 后应 granted/可信: %+v", dec)
	}

	// 渠道级 revoke：wecom 撤回即生效，sms 仍 granted（渠道范围判定）
	if err := svc.Record(ctx, ConsentRecordInput{
		OneID: oneID, Purpose: model.ConsentPurposeMarketing, ChannelScope: "wecom",
		Decision: model.ConsentDecisionRevoke, Source: model.ConsentSourceSelfService, Actor: "cust-self",
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
	if dec.Status != ConsentStatusRevoked {
		t.Fatalf("渠道撤回后 wecom 应 revoked: %+v", dec)
	}
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "sms")
	if dec.Status != ConsentStatusGranted {
		t.Fatalf("未撤回渠道 sms 应保持 granted: %+v", dec)
	}

	// 同一时刻写两条：后写的事实赢（id 大者优先）
	now := time.Now()
	_ = svc.Record(ctx, ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeTransactional, Decision: model.ConsentDecisionRevoke, OccurredAt: now})
	_ = svc.Record(ctx, ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeTransactional, Decision: model.ConsentDecisionGrant, OccurredAt: now})
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeTransactional, "")
	if dec.Status != ConsentStatusGranted {
		t.Fatalf("后写事实应赢: %+v", dec)
	}
}

// 偏好暂停：渠道行优先于全渠道行；到期自动恢复。
func TestContactConsent_PreferencePause(t *testing.T) {
	svc, _ := setupConsentService(t)
	ctx := context.Background()
	const oneID = "one-consent-2"

	if err := svc.Record(ctx, ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionGrant}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	// 全渠道暂停 1 小时
	until := time.Now().Add(time.Hour)
	if err := svc.SetPreference(ctx, oneID, model.ConsentPurposeMarketing, "", true, &until, "Asia/Shanghai"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	dec := svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
	if dec.Status != ConsentStatusPaused || dec.PausedUntil == nil {
		t.Fatalf("暂停期应 paused: %+v", dec)
	}

	// 渠道行（sms 不暂停）压过全渠道暂停
	if err := svc.SetPreference(ctx, oneID, model.ConsentPurposeMarketing, "sms", false, nil, ""); err != nil {
		t.Fatalf("set channel preference: %v", err)
	}
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "sms")
	if dec.Status != ConsentStatusGranted {
		t.Fatalf("渠道行恢复应 granted: %+v", dec)
	}

	// 到期自动恢复
	past := time.Now().Add(-time.Minute)
	if err := svc.SetPreference(ctx, oneID, model.ConsentPurposeMarketing, "wecom", true, &past, ""); err != nil {
		t.Fatalf("set expired pause: %v", err)
	}
	dec = svc.CheckSendConsent(ctx, oneID, model.ConsentPurposeMarketing, "wecom")
	if dec.Status != ConsentStatusGranted {
		t.Fatalf("到期暂停应恢复 granted: %+v", dec)
	}

	// 非法入参：one_id 为空被拒
	if err := svc.SetPreference(ctx, "", model.ConsentPurposeMarketing, "", true, nil, ""); err == nil {
		t.Fatal("空 one_id 应被拒")
	}
}

// 管线闸：撤回事实压过 metadata 伪造的 consent=true。
func TestPipeline_ConsentRevocationBeatsMetadataAssertion(t *testing.T) {
	svc, _ := setupConsentService(t)
	ctx := context.Background()
	const oneID = "one-consent-3"

	if err := svc.Record(ctx, ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	cfg := DefaultSendPipelineConfig(nil)
	cfg.ConsentChecker = svc
	cfg.ConsentMode = ConsentModeRevocationOnly
	pipeline := NewSendPipeline(cfg).(*defaultSendPipeline)

	req := &ReachSendRequest{
		Channel: "wecom", CustomerID: oneID,
		Content: "营销内容",
		Metadata: map[string]string{
			"one_id":  oneID,
			"consent": "true", // 调用方伪造的自述
			"opt_in":  "true",
		},
	}
	resp := pipeline.Send(ctx, req)
	if resp.Success {
		t.Fatal("撤回后即使 metadata 自述 consent=true 也必须拒发")
	}
	if resp.Error != ErrSendConsentRevoked.Error() {
		t.Fatalf("错误应为撤回原因码，got %q", resp.Error)
	}
}

// 管线闸（D02 联动）：排队后撤回 → 延迟队列重发时被 consent 闸拦下。
func TestPipeline_DeferredThenRevokedNeverSends(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.ContactConsentEvent{},
		&model.ContactPreference{},
		&model.ReachDeferredOutbound{},
	)
	repo := repository.NewContactConsentRepository(database)
	svc := NewContactConsentService(repo)

	// quiet 窗口时钟注入：让首轮命中静默 → 进持久延迟队列
	cfg := DefaultSendPipelineConfig(&fakeConsentAdapter{})
	cfg.ConsentChecker = svc
	cfg.ConsentMode = ConsentModeRevocationOnly
	cfg.QuietHoursEnabled = true
	cfg.QuietHoursClock = func() time.Time { return time.Date(2026, 10, 28, 23, 0, 0, 0, cstZone) }
	deferralRepo := repository.NewReachDeferredOutboundRepository(database)
	pq := NewPersistentQuietHoursQueue(deferralRepo)
	cfg.QuietHoursDeferrer = pq
	pipeline := NewSendPipeline(cfg).(*defaultSendPipeline)

	const oneID = "one-consent-4"
	req := &ReachSendRequest{
		Channel: "wecom", CustomerID: oneID, Content: "安静时段的触达",
		Metadata: map[string]string{"one_id": oneID, "consent": "true"},
	}
	resp := pipeline.Send(context.Background(), req)
	if !resp.Deferred {
		t.Fatalf("quiet 窗口内应进入延迟队列: %+v", resp)
	}
	var rows []model.ReachDeferredOutbound
	if err := database.Where("status = ?", model.DeferredReachStatusPending).Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("应有一条持久 pending: %v (%d)", err, len(rows))
	}

	// 排队后客户撤回
	if err := svc.Record(context.Background(), ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeMarketing, Decision: model.ConsentDecisionRevoke}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// 到期重发：走原管线（consent 闸仍在配置里）→ 必须拒发且行不得标 sent
	pipe2 := &stubDeferredPipeline{resp: &SendResponse{Success: true}}
	pq2 := NewPersistentQuietHoursQueue(deferralRepo)
	// 与生产一致：重发管线带 consent 闸
	_ = pipeline // pipeline 即带闸的重发管线（dispatchDue 用传入 pipeline）
	pq2.dispatchDue(context.Background(), recordingConsentPipeline{inner: pipe2, cfg: cfg})

	var got model.ReachDeferredOutbound
	if err := database.First(&got, rows[0].ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status == model.DeferredReachStatusSent {
		t.Fatal("撤回后重发不得标记 sent")
	}
}

// recordingConsentPipeline 用与生产相同的管线配置重放（consent 闸生效）
type recordingConsentPipeline struct {
	inner *stubDeferredPipeline
	cfg   SendPipelineConfig
}

func (r recordingConsentPipeline) Send(ctx context.Context, req *ReachSendRequest) *SendResponse {
	return NewSendPipeline(r.cfg).Send(ctx, req)
}

// fakeConsentAdapter 管线 send 步骤的假渠道适配器
type fakeConsentAdapter struct{}

func (f *fakeConsentAdapter) Send(ctx context.Context, req *ReachSendRequest) (string, error) {
	return "mh-fake", nil
}

// revocation_only 模式：无凭据 + metadata 自述 → 放行但 step log 标 legacy_assertion。
func TestPipeline_RevocationOnlyAllowsLegacyAssertion(t *testing.T) {
	svc, _ := setupConsentService(t)
	cfg := DefaultSendPipelineConfig(&fakeConsentAdapter{})
	cfg.ConsentChecker = svc
	cfg.ConsentMode = ConsentModeRevocationOnly
	pipeline := NewSendPipeline(cfg).(*defaultSendPipeline)

	req := &ReachSendRequest{
		Channel: "wecom", CustomerID: "one-legacy",
		Content:  "营销内容",
		Metadata: map[string]string{"one_id": "one-legacy", "consent": "true"},
	}
	resp := pipeline.Send(context.Background(), req)
	if !resp.Success {
		t.Fatalf("revocation_only 下无凭据应放行（存量不批量伪造）: %v", resp.Error)
	}
	for _, step := range resp.StepResults {
		if step.Step == SendStepPermission {
			found := false
			for _, out := range step.Output {
				if m, ok := out.(map[string]any); ok && m["metadata_opt_in"] == ConsentReasonLegacyAssertion {
					found = true
				}
			}
			if !found {
				t.Fatalf("metadata 自述必须标 legacy_assertion 留痕: %+v", step.Output)
			}
		}
	}

	// enforce 模式：同样的请求 → 拒发
	cfg.ConsentMode = ConsentModeEnforce
	pipeline2 := NewSendPipeline(cfg).(*defaultSendPipeline)
	resp2 := pipeline2.Send(context.Background(), req)
	if resp2.Success || resp2.Error != ErrSendConsentRequired.Error() {
		t.Fatalf("enforce 模式无凭据应拒发: %+v", resp2)
	}
}

// DNC 故障拒发：strict 检查器报错 → fail-closed 留原因码。
func TestPipeline_DNCQueryFailureFailsClosed(t *testing.T) {
	cfg := DefaultSendPipelineConfig(&fakeConsentAdapter{})
	cfg.DoNotContact = failOpenDNC{}
	cfg.DoNotContactStrict = failingDNC{}
	pipeline := NewSendPipeline(cfg).(*defaultSendPipeline)

	req := &ReachSendRequest{
		Channel: "wecom", CustomerID: "one-dnc",
		Content:  "营销内容",
		Metadata: map[string]string{"one_id": "one-dnc"},
	}
	resp := pipeline.Send(context.Background(), req)
	if resp.Success {
		t.Fatal("DNC 状态查询失败必须拒发（fail-closed）")
	}
	if resp.Error != ErrSendDoNotContactUnavailable.Error() {
		t.Fatalf("错误应为 fail-closed 原因，got %q", resp.Error)
	}
}

type failingDNC struct{}

func (failingDNC) IsBlockedStrict(ctx context.Context, oneID, channel string) (bool, error) {
	return false, context.DeadlineExceeded
}

type failOpenDNC struct{}

func (failOpenDNC) IsBlocked(ctx context.Context, oneID, channel string) bool {
	return false // 旧 fail-open：查询失败当作未拦截（对照：不配 strict 时的行为）
}
