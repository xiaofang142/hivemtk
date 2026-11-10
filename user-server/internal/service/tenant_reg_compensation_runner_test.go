package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/platform"
)

// 开户登记自动补偿扫描测试（PLT-03）。
//
// 覆盖四条判据面：
//  1. 真库 E2E：先造 failed/pending 行 → 跑一轮 → 假平台收到重报且平台 Upsert
//     幂等不重复建行（含 ack 丢失型 pending：平台已有行、重报后仍一行）→ 状态 synced；
//  2. off 模式一轮扫描零出站请求（zero_egress 同款断言，行状态也不被改写）；
//  3. 连续失败指数退避：计数推进、退避未到点跳过、错误文本持续落 platform_sync_err；
//  4. 手动 RetryRegistrationSync 路径回归绿（与自动扫描并存不互斥，语义不变）。
//
// DB 按 AV-02 口径走 testutil.NewTestDB（专用快测实例，仅显式 POSTGRES_TEST_* 环境
// 变量可入）；平台端用 httptest 假件，无任何真实出域。

// fakeRegRow 假平台账本行（登记上报链路关心的字段子集）
type fakeRegRow struct {
	MerchantKey      string
	InstallID        string
	TenantCode       string
	CompanyName      string
	LegalPerson      string
	Phone            string
	Email            string
	LicenseNo        string
	Address          string
	RegMode          string
	AgreementVersion string
	InitIP           string
}

// fakeRegPlatform 假平台：POST /merchant-api/tenant/register 按
// hivemtk-platform/platform-server 仓储 Upsert 的真实语义记账 ——
// 幂等键 (merchant_key, tenant_code)，命中即整行覆盖，仅保留首次登记的协议版本
// （见 platform-server internal/repository/tenant_registration.go Upsert）。
type fakeRegPlatform struct {
	mu      sync.Mutex
	srv     *httptest.Server
	submits int                    // 收到的 POST 总数
	failAll bool                   // 平台持续故障开关（退避格与手动路径负例用）
	rows    map[string]*fakeRegRow // 键 = merchant_key + "\x00" + tenant_code
}

func newFakeRegPlatform(t *testing.T) *fakeRegPlatform {
	t.Helper()
	f := &fakeRegPlatform{rows: map[string]*fakeRegRow{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/merchant-api/tenant/register" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var p platform.TenantRegistrationPayload
		if err := json.Unmarshal(body, &p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.submits++
		if f.failAll {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// 平台 Upsert 语义模拟：命中（幂等键）→ 整行覆盖但保留首次协议版本；
		// 未命中 → 新建。重复上报永不产生第二行。
		key := p.MerchantKey + "\x00" + p.TenantCode
		if existing, ok := f.rows[key]; ok {
			prev := existing.AgreementVersion
			*existing = fakeRegRow{
				MerchantKey: p.MerchantKey, InstallID: p.InstallID, TenantCode: p.TenantCode,
				CompanyName: p.CompanyName, LegalPerson: p.LegalPerson, Phone: p.Phone,
				Email: p.Email, LicenseNo: p.LicenseNo, Address: p.Address, RegMode: p.RegMode,
				AgreementVersion: prev, InitIP: p.InitIP,
			}
		} else {
			f.rows[key] = &fakeRegRow{
				MerchantKey: p.MerchantKey, InstallID: p.InstallID, TenantCode: p.TenantCode,
				CompanyName: p.CompanyName, LegalPerson: p.LegalPerson, Phone: p.Phone,
				Email: p.Email, LicenseNo: p.LicenseNo, Address: p.Address, RegMode: p.RegMode,
				AgreementVersion: p.AgreementVersion, InitIP: p.InitIP,
			}
		}
		// 平台 response.Success 的信封形态（code=200 非 0，见 platform-server utils/response）
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"登记完成","data":{"id":"reg-fake"}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRegPlatform) submissionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submits
}

func (f *fakeRegPlatform) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func (f *fakeRegPlatform) rowFor(merchantKey, tenantCode string) *fakeRegRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[merchantKey+"\x00"+tenantCode]
}

// withRegCompensationOn 开态环境：平台指向假平台，商户身份固定（清理时还原全局）
func withRegCompensationOn(t *testing.T, f *fakeRegPlatform) {
	t.Helper()
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("MERCHANT_API_SECRET", "s")         // 签名密钥：env 档
	t.Setenv("PLATFORM_MERCHANT_KEY", "mk-comp") // 客户端商户键
	oldKey := platform.GetMerchantKey()
	platform.SetMerchantKeyForTest("mk-comp") // payload 的 merchant_key 同源
	t.Cleanup(func() { platform.SetMerchantKeyForTest(oldKey) })
	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: f.srv.URL, Secret: "s", AdminUsername: "admin", AdminPassword: "p"}
}

// seedRegTenant 直插一条合成租户行（登记字段齐全，便于断言 payload 消费的是行现值）
func seedRegTenant(t *testing.T, code, syncState, syncErr string) *model.Tenant {
	t.Helper()
	row := &model.Tenant{
		Code: code, Name: "补偿扫描租户-" + code, Status: model.TenantStatusActive, Plan: "free",
		CompanyName: "补偿扫描公司-" + code, LegalPerson: "张三", Phone: "13800000001",
		Email: code + "@comp.example", LicenseNo: "LIC-" + code, Address: "杭州市西湖区1号",
		RegMode:          model.TenantRegModeSaaS,
		AgreementVersion: serviceCurrentAgreementVersion,
		RegulatoryAck:    true,
		InitIP:           "10.0.0.7",
		InitUA:           "abc123",
		PlatformSync:     syncState,
		PlatformSyncErr:  syncErr,
	}
	if err := db.GetDB().Create(row).Error; err != nil {
		t.Fatalf("铺租户行失败: %v", err)
	}
	return row
}

func getRegTenant(t *testing.T, id uint) *model.Tenant {
	t.Helper()
	var row model.Tenant
	if err := db.GetDB().First(&row, id).Error; err != nil {
		t.Fatalf("回读租户行失败: %v", err)
	}
	return &row
}

// TestTenantRegCompensation_OffModeZeroEgress off 模式一轮扫描零出域（zero_egress 同款）：
// 即使 PlatformCfg 指着一个可达靶、库里也躺着 pending/failed 行，扫描必须一帧不发，
// 且行状态原样保留（不得被改写成 skipped）。
func TestTenantRegCompensation_OffModeZeroEgress(t *testing.T) {
	testutil.NewTestDB(t, &model.Tenant{})

	var hits int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
	}))
	defer target.Close()

	t.Setenv("PLATFORM_ENABLED", "false") // 关态判定必须来自开关，而不是"碰巧没配置"
	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: target.URL, Secret: "s", AdminPassword: "p"}

	failed := seedRegTenant(t, "tcomp-off-f", model.TenantSyncFailed, "历史失败原因")
	pending := seedRegTenant(t, "tcomp-off-p", model.TenantSyncPending, "")

	comp := newTenantRegCompensator()
	res, err := comp.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("off 模式扫描不应报错: %v", err)
	}
	if !res.ZeroEgress {
		t.Error("off 模式应返回 ZeroEgress=true")
	}
	if res.Scanned != 0 || res.Attempted != 0 {
		t.Errorf("off 模式不得查库扫描，实际 Scanned=%d Attempted=%d", res.Scanned, res.Attempted)
	}
	if hits != 0 {
		t.Fatalf("off 模式发生了 %d 次出站请求，零出域承诺被破", hits)
	}
	// 行状态原样：pending/failed 不被改写为 skipped（否则恢复 on 后扫描取不到它们）
	if got := getRegTenant(t, failed.ID); got.PlatformSync != model.TenantSyncFailed || got.PlatformSyncErr != "历史失败原因" {
		t.Errorf("failed 行被 off 模式扫描改写: sync=%q err=%q", got.PlatformSync, got.PlatformSyncErr)
	}
	if got := getRegTenant(t, pending.ID); got.PlatformSync != model.TenantSyncPending {
		t.Errorf("pending 行被 off 模式扫描改写: sync=%q", got.PlatformSync)
	}
}

// TestTenantRegCompensation_ResubmitsAndUpsertIdempotent 真库 E2E 主判据：
// failed/pending 行 → 一轮扫描 → 假平台按 Upsert 语义收报不重复建行 → 状态 synced；
// 已 synced 行不重报；第二轮收敛后零重复出站。
func TestTenantRegCompensation_ResubmitsAndUpsertIdempotent(t *testing.T) {
	testutil.NewTestDB(t, &model.Tenant{})
	fake := newFakeRegPlatform(t)
	withRegCompensationOn(t, fake)

	failed := seedRegTenant(t, "tcomp-e2e-f", model.TenantSyncFailed, "上次平台不可达")
	pending := seedRegTenant(t, "tcomp-e2e-p", model.TenantSyncPending, "")
	synced := seedRegTenant(t, "tcomp-e2e-s", model.TenantSyncSynced, "")

	// ack 丢失型 pending 的前置：平台其实已经收到过 B 租户的一份登记
	// （首报成功但用户端置 synced 的回写丢了），这里直接给假平台预铺一行。
	failedPayloadAtPlatform := &fakeRegRow{
		MerchantKey: "mk-comp", TenantCode: pending.Code,
		CompanyName: "平台端旧公司名", AgreementVersion: "2020-01-01.v0",
	}
	fake.rows["mk-comp\x00"+pending.Code] = failedPayloadAtPlatform

	comp := newTenantRegCompensator()
	res, err := comp.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描一轮失败: %v", err)
	}
	if res.Scanned != 2 || res.Attempted != 2 || res.Succeeded != 2 {
		t.Fatalf("首轮读数不符: %+v（应只扫 failed+pending 两行，synced 不重报）", res)
	}
	if hit := fake.rowFor("mk-comp", synced.Code); hit != nil {
		t.Errorf("已 synced 行不得重报，假平台却收到了 %s", synced.Code)
	}
	if got := fake.submissionCount(); got != 2 {
		t.Fatalf("假平台应恰收到 2 次重报，实际 %d", got)
	}
	if got := fake.rowCount(); got != 2 {
		t.Fatalf("平台 Upsert 幂等被破：2 次重报 + 1 行预铺应仍为 2 行，实际 %d", got)
	}

	// 平台侧行内容：pending 行（ack 丢失型）被整行覆盖为租户行现值，
	// 但首次登记的协议版本按平台 Upsert 语义保留
	row := fake.rowFor("mk-comp", pending.Code)
	if row == nil || row.CompanyName != pending.CompanyName || row.AgreementVersion != "2020-01-01.v0" {
		t.Errorf("平台侧行覆盖结果不符: %+v", row)
	}
	// failed 行首次上报：payload 消费的是租户行现值 + install 身份（二审定死的重报上下文）
	row = fake.rowFor("mk-comp", failed.Code)
	if row == nil {
		t.Fatal("failed 行的重报未到达假平台")
	}
	if row.CompanyName != failed.CompanyName || row.LegalPerson != failed.LegalPerson ||
		row.Phone != failed.Phone || row.Email != failed.Email ||
		row.LicenseNo != failed.LicenseNo || row.Address != failed.Address ||
		row.InitIP != failed.InitIP || row.RegMode != model.TenantRegModeSaaS {
		t.Errorf("重报 payload 应逐字段消费租户行现值: %+v", row)
	}
	if row.MerchantKey != "mk-comp" {
		t.Errorf("merchant_key 应取本部署商业身份, got %q", row.MerchantKey)
	}
	if row.InstallID != currentInstallID() {
		t.Errorf("install_id 应与 currentInstallID() 同源, got %q want %q", row.InstallID, currentInstallID())
	}

	// 用户侧行状态：pending/failed → synced，错误文本清空
	for _, id := range []uint{failed.ID, pending.ID} {
		got := getRegTenant(t, id)
		if got.PlatformSync != model.TenantSyncSynced || got.PlatformSyncErr != "" {
			t.Errorf("租户 %d 应置 synced 且清错误, got sync=%q err=%q", id, got.PlatformSync, got.PlatformSyncErr)
		}
	}

	// 第二轮：已收敛（无 pending/failed 行）→ 零取行零出站
	res2, err := comp.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮扫描失败: %v", err)
	}
	if res2.Scanned != 0 || res2.Attempted != 0 {
		t.Errorf("收敛后不应再扫描出行: %+v", res2)
	}
	if got := fake.submissionCount(); got != 2 {
		t.Errorf("收敛后不得再出站, 假平台累计收到 %d 次（应仍为 2）", got)
	}
}

// TestTenantRegCompensation_BackoffThrottles 连续失败退避格：
// 首败后下一周期照常重试，随后按 ×2 指数拉长（封顶 6h），退避未到点的轮次跳过；
// 每次失败错误文本持续落 platform_sync_err（持久留痕），成功后记账清零。
func TestTenantRegCompensation_BackoffThrottles(t *testing.T) {
	testutil.NewTestDB(t, &model.Tenant{})
	fake := newFakeRegPlatform(t)
	fake.mu.Lock()
	fake.failAll = true // 平台持续故障
	fake.mu.Unlock()
	withRegCompensationOn(t, fake)

	row := seedRegTenant(t, "tcomp-bo-f", model.TenantSyncFailed, "")

	comp := newTenantRegCompensator()
	cur := time.Now()
	comp.now = func() time.Time { return cur } // 注时：每轮推进一个周期

	// 第 1 轮（T0）：失败 → 计数 1，下次尝试 T0+10min
	res, err := comp.ScanOnce(context.Background())
	if err != nil || res.Attempted != 1 || res.Succeeded != 0 {
		t.Fatalf("第 1 轮读数不符: res=%+v err=%v", res, err)
	}
	// 第 2 轮（T0+10min）：恰到点，照常重试 → 计数 2，下次 T0+30min
	cur = cur.Add(tenantRegCompensationInterval)
	res, _ = comp.ScanOnce(context.Background())
	if res.Attempted != 1 || res.BackoffSkipped != 0 {
		t.Fatalf("第 2 轮应照常重试（base=一个周期不跳轮）: %+v", res)
	}
	// 第 3 轮（T0+20min）：退避未到点（下次 T0+30min）→ 跳过、零出站
	cur = cur.Add(tenantRegCompensationInterval)
	res, _ = comp.ScanOnce(context.Background())
	if res.Attempted != 0 || res.BackoffSkipped != 1 {
		t.Fatalf("第 3 轮应被退避跳过: %+v", res)
	}
	if got := fake.submissionCount(); got != 2 {
		t.Fatalf("退避轮不得出站，假平台累计 %d 次（应仍为 2）", got)
	}
	// 第 4 轮（T0+30min）：到点重试 → 计数 3
	cur = cur.Add(tenantRegCompensationInterval)
	res, _ = comp.ScanOnce(context.Background())
	if res.Attempted != 1 {
		t.Fatalf("第 4 轮应到点重试: %+v", res)
	}
	if got := comp.failCount[row.ID]; got != 3 {
		t.Errorf("连续失败计数应推进到 3, got %d", got)
	}
	// 失败留痕持续刷新（持久化在 platform_sync_err，不新增表/列）
	if got := getRegTenant(t, row.ID); got.PlatformSync != model.TenantSyncFailed || got.PlatformSyncErr == "" {
		t.Errorf("失败行状态与错误留痕不符: sync=%q err=%q", got.PlatformSync, got.PlatformSyncErr)
	}

	// 平台恢复 → 退避到点后重报成功、置 synced 并清账
	fake.mu.Lock()
	fake.failAll = false
	fake.mu.Unlock()
	cur = cur.Add(tenantRegBackoffMax) // 直接推进到退避封顶之外，任何排程都到点
	res, _ = comp.ScanOnce(context.Background())
	if res.Succeeded != 1 {
		t.Fatalf("平台恢复后应重报成功: %+v", res)
	}
	if got := getRegTenant(t, row.ID); got.PlatformSync != model.TenantSyncSynced {
		t.Errorf("恢复后应置 synced, got %q", got.PlatformSync)
	}
	if got := comp.failCount[row.ID]; got != 0 {
		t.Errorf("成功后失败计数应清零, got %d", got)
	}
	if _, ok := comp.nextAttempt[row.ID]; ok {
		t.Error("成功后退避排程应清空")
	}
}

// TestTenantRegCompensation_RunnerOffModeNotStarted off 模式 runner 整个不启动：
// 两通道即刻关闭、Stop 幂等、零出站（即使 PlatformCfg 指着可达靶）。
func TestTenantRegCompensation_RunnerOffModeNotStarted(t *testing.T) {
	var hits int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	t.Setenv("PLATFORM_ENABLED", "false")
	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: target.URL, Secret: "s", AdminPassword: "p"}

	r := StartTenantRegistrationCompensation(context.Background())
	select {
	case <-r.firstDone:
	default:
		t.Error("off 模式首轮通道应即刻关闭（没有首轮可跑）")
	}
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("off 模式 Stop 应立即返回（没有协程可等）")
	}
	if hits != 0 {
		t.Fatalf("off 模式发生了 %d 次出站请求", hits)
	}
}

// TestRetryRegistrationSync_ManualPathRegression 手动重试按钮回归：
// 成功路径置 synced；平台不可达时仍返回 nil、行留 failed（失败不阻断的既有语义，
// 补偿扫描就是给这条留 failed 的行兜底的）——两条路径并存不互斥。
func TestRetryRegistrationSync_ManualPathRegression(t *testing.T) {
	testutil.NewTestDB(t, &model.Tenant{})
	fake := newFakeRegPlatform(t)
	withRegCompensationOn(t, fake)
	svc := NewTenantService()

	row := seedRegTenant(t, "tcomp-manual", model.TenantSyncFailed, "旧错误")
	if err := svc.RetryRegistrationSync(context.Background(), row.ID); err != nil {
		t.Fatalf("手动重试应成功: %v", err)
	}
	if got := fake.submissionCount(); got != 1 {
		t.Fatalf("手动重试应恰发出 1 次上报, got %d", got)
	}
	if got := getRegTenant(t, row.ID); got.PlatformSync != model.TenantSyncSynced || got.PlatformSyncErr != "" {
		t.Errorf("手动重试后应 synced 且清错误: sync=%q err=%q", got.PlatformSync, got.PlatformSyncErr)
	}

	// 平台故障：返回 nil（不阻断），行留 failed 带错误留痕 —— 交给补偿扫描兜底
	fake.mu.Lock()
	fake.failAll = true
	fake.mu.Unlock()
	row2 := seedRegTenant(t, "tcomp-manual2", model.TenantSyncPending, "")
	if err := svc.RetryRegistrationSync(context.Background(), row2.ID); err != nil {
		t.Fatalf("平台不可达时手动重试不应报错（既有「失败不阻断」语义）: %v", err)
	}
	if got := getRegTenant(t, row2.ID); got.PlatformSync != model.TenantSyncFailed || got.PlatformSyncErr == "" {
		t.Errorf("平台不可达应留 failed 带留痕: sync=%q err=%q", got.PlatformSync, got.PlatformSyncErr)
	}

	// 不存在的租户：报错（既有语义）
	if err := svc.RetryRegistrationSync(context.Background(), 999999); err == nil {
		t.Error("重试不存在的租户应报错")
	}
}
