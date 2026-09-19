// kb_canary_test.go T-P2-05（G-1 知识库版本 + 灰度）的业务层用例。
//
// 分两组：
//   - 纯决策组（不打库）：命名空间折算、三态旗子、分桶的粘性与比例，以及"分桶用的是
//     仓内既有那套哈希而不是新造一套"这一条口径；
//   - 真库端到端组：拿真的 ragcache store 往 rag_answer_cache 写两个命名空间的行，
//     按桶路由各读各的，然后把版本切回去、再把灰度抬到 100%，**一次都不重新生成答案**
//     就能读到灰度期焐热的那一行 —— 这就是 AC②（回滚/转正不重灌）的实证形态。
package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// kbCanaryKB 造一行带版本/灰度参数的 KB（不落库，纯决策用例用）。
func kbCanaryKB(id uint, version int, enabled bool, percent int) *model.KnowledgeBase {
	on := enabled
	return &model.KnowledgeBase{ID: id, Version: version, CanaryEnabled: &on, CanaryPercent: percent}
}

// kbCanaryVec 1024 维查询向量（rag_answer_cache.query_vector 是 vector(1024)，
// 维度不匹配 PG 直接报错 ⇒ 这里不能图省事给短的）。
func kbCanaryVec(seed float32) []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = seed + float32(i)*1e-6
	}
	return v
}

func TestKBCanary_PromptVersionLiteralIsWhatProductionUsed(t *testing.T) {
	// 这条不是废话：整个存量答案缓存的可读性押在 "v1" 这个字面量上。
	// 谁改了这个常量（或改了 kbCachePromptVersion 的 <=1 归并），存量行就再也读不到，
	// 效果等于一次清空全量答案缓存 —— 所以让它在这里红。
	if faqPromptVersion != "v1" {
		t.Fatalf("faqPromptVersion 漂移成 %q：升级前的缓存键就是这个字面量，改它等于清缓存", faqPromptVersion)
	}
	for _, version := range []int{-5, 0, 1} {
		if got := kbCachePromptVersion(version); got != "v1" {
			t.Errorf("version=%d 必须折算成 v1（没 publish 过的 1 与 0/负数这类脏数据都靠这一条兜住同键）, got %q", version, got)
		}
	}
	for version, want := range map[int]string{2: "v2", 7: "v7", 12: "v12"} {
		if got := kbCachePromptVersion(version); got != want {
			t.Errorf("version=%d 期望 %s, got %s", version, want, got)
		}
	}
}

func TestKBCanary_FlagOffAndShadowNeverMoveTheKey(t *testing.T) {
	kb := kbCanaryKB(101, 4, true, 100) // 行上已经把版本推到 4、灰度 100%
	// 旗子的五档写法都要试：认不出的值判 off，"=true" 这类习惯写法只到 shadow。
	for _, raw := range []string{"", "off", "0", "nonsense", "shadow", "observe", "true", "1", "yes"} {
		t.Setenv(KBCanaryFlagEnv, raw)
		for _, oneID := range []string{"phone:aaa", "union:bbb", ""} {
			if got := KBAnswerVersionFor(kb, oneID); got != "v1" {
				t.Errorf("%s=%q one_id=%q 不该换键，got %s", KBCanaryFlagEnv, raw, oneID, got)
			}
		}
	}
}

func TestKBCanary_FlagOnRoutesStableAndCanaryNamespaces(t *testing.T) {
	t.Setenv(KBCanaryFlagEnv, "on")
	kb := kbCanaryKB(202, 4, true, 100)
	// 灰度 100% ⇒ 全员下一个号；OneID 为空的除外（没得可分桶就不放量）。
	if got := KBAnswerVersionFor(kb, "phone:aaa"); got != "v5" {
		t.Errorf("percent=100 期望灰度命名空间 v5（Version+1），got %s", got)
	}
	if got := KBAnswerVersionFor(kb, ""); got != "v4" {
		t.Errorf("OneID 为空不该放量（匿名流量会塌成同一个桶），got %s", got)
	}
	// percent=0：灰度没开，但版本机制是活的 ⇒ 用稳定组自己的号。
	kbOff := kbCanaryKB(202, 4, true, 0)
	if got := KBAnswerVersionFor(kbOff, "phone:aaa"); got != "v4" {
		t.Errorf("percent=0 期望稳定组 v4，got %s", got)
	}
	// 行上灰度没开 ⇒ 无论比例写多少都只用稳定号。
	kbDisarmed := kbCanaryKB(202, 2, false, 100)
	if got := KBAnswerVersionFor(kbDisarmed, "phone:aaa"); got != "v2" {
		t.Errorf("canary_enabled=false 期望 v2，got %s", got)
	}
	// 没 publish、也没配灰度的 KB（version 1 + percent 0）⇒ 旗子开着也还是 v1，
	// 这就是"两道锁任一没放都零影响"：现网所有存量 KB 都属于这一格。
	neverTouched := kbCanaryKB(202, 1, false, 0)
	if got := KBAnswerVersionFor(neverTouched, "phone:aaa"); got != "v1" {
		t.Errorf("version=1 且灰度未开时应保持今天的键 v1，got %s", got)
	}
}

func TestKBCanary_BucketIsStickyAndProportional(t *testing.T) {
	t.Setenv(KBCanaryFlagEnv, "on")
	const kbID = 303
	kb := kbCanaryKB(kbID, 1, true, 30)

	const n = 4000
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("phone:%06d", i)
	}
	canary := 0
	for _, id := range ids {
		if KBAnswerVersionFor(kb, id) == "v2" {
			canary++
		}
	}
	// 容差 3pp：只判"是不是真在按比例分"，不判哈希分布的精度（那是 flagBucketHash 自己的事）。
	if canary < int(n*0.27) || canary > int(n*0.33) {
		t.Errorf("percent=30 时 4000 个 OneID 的灰度数应落在 30%%±3pp，got %d", canary)
	}
	// 粘性：同一 OneID 反复判必须同桶，否则同一会话前一句吃 v2 后一句吃 v1，
	// 用户会看到答案来回变。
	for _, id := range ids[:200] {
		first := KBAnswerVersionFor(kb, id)
		for i := 0; i < 20; i++ {
			if got := KBAnswerVersionFor(kb, id); got != first {
				t.Fatalf("OneID %s 分桶不粘：第一次 %s，第 %d 次 %s", id, first, i+2, got)
			}
		}
	}
	// 版本号为另一个 KB 独立：同 oneID 换 kbID 应当重排（否则灰度人群永远同一批人）。
	other := kbCanaryKB(kbID+1, 1, true, 30)
	sameRatio := 0
	for _, id := range ids[:400] {
		if KBAnswerVersionFor(kb, id) == KBAnswerVersionFor(other, id) {
			sameRatio++
		}
	}
	if sameRatio == 400 {
		t.Error("换 kbID 后分桶结果一模一样 ⇒ 桶键没带 KB 维度，全系统的灰度人群会是同一批人")
	}
}

func TestKBCanary_BucketSharesFeatureFlagRolloutConvention(t *testing.T) {
	// 分桶不新造一套：与 feature_flag.go 的按百分比放量同一哈希键、同一判据。
	t.Setenv(KBCanaryFlagEnv, "on")
	kb := kbCanaryKB(404, 1, true, 45)
	for i := 0; i < 500; i++ {
		oneID := fmt.Sprintf("union:%d", i)
		inCanary := flagBucketHash(fmt.Sprintf("kb.%d", kb.ID), oneID)%100 < uint32(kb.CanaryPercent)
		got := KBAnswerVersionFor(kb, oneID)
		if inCanary && got != "v2" {
			t.Fatalf("flagBucketHash 判该进灰度，KBAnswerVersionFor 却给了 %s（one_id=%s）⇒ 两处口径分了", got, oneID)
		}
		if !inCanary && got != "v1" {
			t.Fatalf("flagBucketHash 判该留稳定组，KBAnswerVersionFor 却给了 %s（one_id=%s）", got, oneID)
		}
	}
}

func TestKBCanary_PercentBoundsGateTheRoute(t *testing.T) {
	t.Setenv(KBCanaryFlagEnv, "on")
	// 越界比例（脏数据/绕过 service 直接写库）一律不放量：判据是"开区间外即视同关闭"。
	for _, percent := range []int{-1, 101, 999} {
		kb := kbCanaryKB(505, 2, true, percent)
		for _, oneID := range []string{"a", "b", "c"} {
			if got := KBAnswerVersionFor(kb, oneID); got != "v2" {
				t.Errorf("percent=%d 属越界脏数据，应视同关闭（只用稳定组 v2），got %s", percent, got)
			}
		}
	}
	// 分桶函数自己的下界守卫：这条不是重复上面，而是直接钉住"负数经 uint32 转换恒真"
	// 那个坑 —— 少了 percent<=0 这一行，percent=-1 会放量 100% 而不是 0%。
	for _, oneID := range []string{"a", "b", "c", "phone:000001"} {
		if kbInCanaryBucket(505, oneID, -1) {
			t.Errorf("percent=-1 直接进分桶函数不该命中（uint32 下溢会把零放量翻成全量）")
		}
		if kbInCanaryBucket(505, oneID, 0) {
			t.Errorf("percent=0 直接进分桶函数不该命中")
		}
	}
	// 上界不挡也必须是全命中：hash%100 ∈ [0,99]，percent=100 天然放行（100% 放量靠这条）。
	for i := 0; i < 50; i++ {
		oneID := fmt.Sprintf("union:%d", i)
		if !kbInCanaryBucket(505, oneID, 100) {
			t.Fatalf("percent=100 应全量命中，%s 却被判稳定组", oneID)
		}
	}
}

func setupKBCanaryDB(t *testing.T) *gorm.DB {
	t.Helper()
	// rag_answer_cache 也建：版本命名空间的物理载体就是这张表，不打库就等于没验 AC②。
	return testutil.NewTestDB(t, &model.KnowledgeBase{}, &ragcache.RAGAnswerCache{})
}

// newKBForCacheTest 建一行 KB 并返回其十进制 ID 字符串（ragcache 的 kb_id 口径）。
func newKBForCacheTest(t *testing.T, database *gorm.DB, code string) *model.KnowledgeBase {
	t.Helper()
	svc := NewKnowledgeBaseService(database)
	enabled := true
	kb := &model.KnowledgeBase{
		KBCode: code, Type: model.KnowledgeBaseTypeFAQ, Name: "灰度库",
		OwnerType: model.KnowledgeBaseOwnerShared, Enabled: &enabled,
	}
	if err := svc.CreateKB(context.Background(), kb); err != nil {
		t.Fatalf("CreateKB: %v", err)
	}
	if kb.ID == 0 {
		t.Fatal("CreateKB 没回填 ID")
	}
	if kb.Version != 1 {
		t.Errorf("新建 KB 应从 1 号起（管理端别显示 version=0），got %d", kb.Version)
	}
	return kb
}

func TestKBCanary_TwoVersionsCoexistAndRollbackNeedsNoReingest(t *testing.T) {
	database := setupKBCanaryDB(t)
	ctx := context.Background()
	svc := NewKnowledgeBaseService(database)
	kb := newKBForCacheTest(t, database, "kb-canary-e2e")

	cacheSvc := ragcache.NewFAQAnswerCacheService(
		ragcache.NewPGAnswerCacheStore(database), ragcache.NewPGKBMetaReader(database), 0)
	kbIDStr := strconv.FormatUint(uint64(kb.ID), 10)

	// 找一个进灰度、一个留稳定组的 OneID（percent=50，扫几十个必有）。
	t.Setenv(KBCanaryFlagEnv, "on")
	if _, err := svc.SetKBCanary(ctx, kb.ID, true, 50); err != nil {
		t.Fatalf("SetKBCanary: %v", err)
	}
	reloaded, err := svc.GetKB(ctx, kb.ID)
	if err != nil || reloaded == nil {
		t.Fatalf("回读 KB 失败: %v", err)
	}
	if reloaded.CanaryPercent != 50 || reloaded.CanaryEnabled == nil || !*reloaded.CanaryEnabled {
		t.Fatalf("灰度参数没落库: %+v", reloaded)
	}
	var canaryID, stableID string
	for i := 0; i < 200 && (canaryID == "" || stableID == ""); i++ {
		oneID := fmt.Sprintf("phone:%05d", i)
		switch KBAnswerVersionFor(reloaded, oneID) {
		case "v2":
			if canaryID == "" {
				canaryID = oneID
			}
		case "v1":
			if stableID == "" {
				stableID = oneID
			}
		}
	}
	if canaryID == "" || stableID == "" {
		t.Fatalf("percent=50 却找不到两组 OneID（canary=%q stable=%q）", canaryID, stableID)
	}

	// 两组各生成一条答案并写进自己那组命名空间：内容刻意不同，好让"读到谁"可判。
	vec := kbCanaryVec(1)
	stableAnswer := "稳定版答案：满三百减三十，活动到本月底结束。"
	canaryAnswer := "灰度版答案：新用户首单立减五十，活动到本月底结束。"
	for _, tc := range []struct {
		oneID, answer string
	}{
		{stableID, stableAnswer},
		{canaryID, canaryAnswer},
	} {
		version := KBAnswerVersionFor(reloaded, tc.oneID)
		if err := cacheSvc.Store(ctx, ragcache.StoreRequest{
			KBID: kbIDStr, PromptVersion: version, QueryVector: vec,
			Answer: tc.answer, FromKnowledgeBase: true,
		}); err != nil {
			t.Fatalf("Store(%s): %v", version, err)
		}
	}

	// AC①：同一 KB、两个命名空间共存，各按桶读到各自的那条。
	for _, tc := range []struct {
		oneID, want string
	}{
		{stableID, stableAnswer},
		{canaryID, canaryAnswer},
	} {
		version := KBAnswerVersionFor(reloaded, tc.oneID)
		got, err := cacheSvc.Lookup(ctx, ragcache.LookupRequest{KBID: kbIDStr, PromptVersion: version, QueryVector: vec})
		if err != nil {
			t.Fatalf("Lookup(%s): %v", version, err)
		}
		if got == nil || got.Answer != tc.want {
			t.Errorf("one_id=%s ⇒ 命名空间 %s 应读到 %q，got %+v", tc.oneID, version, tc.want, got)
		}
	}

	info, err := svc.KBVersionInfo(ctx, kb.ID)
	if err != nil {
		t.Fatalf("KBVersionInfo: %v", err)
	}
	rows, _ := info["rows_by_namespace"].(map[string]int64)
	if rows["v1"] != 1 || rows["v2"] != 1 {
		t.Fatalf("两个版本的行应各 1 条共存，got %+v", rows)
	}
	var totalBefore int64
	if err := database.Raw(`SELECT COUNT(*) FROM rag_answer_cache WHERE kb_id = ?`, kbIDStr).
		Scan(&totalBefore).Error; err != nil {
		t.Fatalf("统计行数: %v", err)
	}

	// 回滚：把版本退回 1 并清灰度 ⇒ 全量流量回到 v1 那一行。
	if _, err := svc.PublishKBVersion(ctx, kb.ID, 1); err != nil {
		t.Fatalf("回滚 PublishKBVersion(target=1): %v", err)
	}
	rolled, err := svc.GetKB(ctx, kb.ID)
	if err != nil || rolled == nil {
		t.Fatalf("回滚后回读失败: %v", err)
	}
	if rolled.Version != 1 {
		t.Fatalf("回滚后 version 应为 1，got %d", rolled.Version)
	}
	if rolled.CanaryEnabled != nil && *rolled.CanaryEnabled {
		t.Error("切版本必须顺手清灰度，否则灰度目标会指到 v2 与稳定组撞号")
	}
	if got := KBAnswerVersionFor(rolled, canaryID); got != "v1" {
		t.Errorf("回滚后原灰度 OneID 也应读 v1，got %s", got)
	}
	res, err := cacheSvc.Lookup(ctx, ragcache.LookupRequest{KBID: kbIDStr, PromptVersion: "v1", QueryVector: vec})
	if err != nil || res == nil || res.Answer != stableAnswer {
		t.Fatalf("回滚后 v1 那行必须还在（被删掉就说明版本写入 bump 了 updated_at）: %+v err=%v", res, err)
	}

	// AC② 的正面形态：再把灰度抬到 100%，**一次都不重新生成答案**就能读到灰度期那行。
	if _, err := svc.SetKBCanary(ctx, kb.ID, true, 100); err != nil {
		t.Fatalf("重新放量 SetKBCanary: %v", err)
	}
	rearmed, err := svc.GetKB(ctx, kb.ID)
	if err != nil || rearmed == nil {
		t.Fatalf("重新放量后回读失败: %v", err)
	}
	if got := KBAnswerVersionFor(rearmed, canaryID); got != "v2" {
		t.Fatalf("重新放量后应回到 v2 命名空间，got %s", got)
	}
	res2, err := cacheSvc.Lookup(ctx, ragcache.LookupRequest{
		KBID: kbIDStr, PromptVersion: KBAnswerVersionFor(rearmed, canaryID), QueryVector: vec})
	if err != nil {
		t.Fatalf("Lookup(v2): %v", err)
	}
	if res2 == nil || res2.Answer != canaryAnswer {
		t.Errorf("回滚再放量不该重灌数据：灰度期写下的那行要能直接读到，got %+v", res2)
	}

	var totalAfter int64
	if err := database.Raw(`SELECT COUNT(*) FROM rag_answer_cache WHERE kb_id = ?`, kbIDStr).
		Scan(&totalAfter).Error; err != nil {
		t.Fatalf("统计行数: %v", err)
	}
	if totalAfter != totalBefore {
		t.Errorf("整个转正/回滚/再放量过程不该增删任何缓存行：before=%d after=%d", totalBefore, totalAfter)
	}
}

func TestKBCanary_VersionSwitchDoesNotTouchUpdatedAt(t *testing.T) {
	database := setupKBCanaryDB(t)
	ctx := context.Background()
	svc := NewKnowledgeBaseService(database)
	kb := newKBForCacheTest(t, database, "kb-canary-updated-at")

	updatedAtBefore, err := readKBUpdatedAt(ctx, database, kb.ID)
	if err != nil {
		t.Fatalf("读 updated_at: %v", err)
	}
	if _, err := svc.PublishKBVersion(ctx, kb.ID, 0); err != nil {
		t.Fatalf("PublishKBVersion: %v", err)
	}
	if _, err := svc.SetKBCanary(ctx, kb.ID, true, 10); err != nil {
		t.Fatalf("SetKBCanary: %v", err)
	}
	updatedAtAfter, err := readKBUpdatedAt(ctx, database, kb.ID)
	if err != nil {
		t.Fatalf("读 updated_at: %v", err)
	}
	if !updatedAtBefore.Equal(updatedAtAfter) {
		t.Errorf("版本与灰度写入不得 bump updated_at（它是答案缓存的失效信号）：%v ⇒ %v",
			updatedAtBefore, updatedAtAfter)
	}
}

func readKBUpdatedAt(ctx context.Context, database *gorm.DB, id uint) (out time.Time, err error) {
	err = database.WithContext(ctx).Raw(`SELECT updated_at FROM knowledge_bases WHERE id = ?`, id).Scan(&out).Error
	return
}

func TestKBCanary_PublishAndSetCanaryInputRules(t *testing.T) {
	database := setupKBCanaryDB(t)
	ctx := context.Background()
	svc := NewKnowledgeBaseService(database)
	kb := newKBForCacheTest(t, database, "kb-canary-inputs")

	if _, err := svc.PublishKBVersion(ctx, kb.ID, -3); err == nil {
		t.Error("负版本号应报错")
	}
	if _, err := svc.SetKBCanary(ctx, kb.ID, true, 101); err == nil {
		t.Error("percent>100 应报错")
	}
	if _, err := svc.SetKBCanary(ctx, kb.ID, true, -1); err == nil {
		t.Error("percent<0 应报错")
	}
	// 报错路径不得留下副作用：DB 里版本与灰度还是原样。
	got, err := svc.GetKB(ctx, kb.ID)
	if err != nil || got == nil {
		t.Fatalf("GetKB: %v", err)
	}
	if got.Version != 1 || got.CanaryPercent != 0 {
		t.Errorf("非法入参不该写库，got version=%d percent=%d", got.Version, got.CanaryPercent)
	}
	if _, err := svc.PublishKBVersion(ctx, 999999999, 0); err == nil {
		t.Error("不存在的 KB 应报错而不是静默成功")
	}
	if info, err := svc.KBVersionInfo(ctx, 999999999); err != nil || info != nil {
		t.Errorf("不存在的 KB 版本信息应为 (nil, nil)，got (%+v, %v)", info, err)
	}
	// 转正：target=0 ⇒ 下一个号，且灰度清零。
	out, err := svc.PublishKBVersion(ctx, kb.ID, 0)
	if err != nil {
		t.Fatalf("PublishKBVersion: %v", err)
	}
	if out.Version != 2 {
		t.Errorf("转正应把 version 推到 2，got %d", out.Version)
	}
	if out.CanaryEnabled != nil && *out.CanaryEnabled {
		t.Error("转正后应清灰度")
	}
	// 显式激活一个更高的号（灰度未跑过也能直接指定）。
	out, err = svc.PublishKBVersion(ctx, kb.ID, 9)
	if err != nil || out.Version != 9 {
		t.Fatalf("显式激活 9 号失败: %+v %v", out, err)
	}
	info, err := svc.KBVersionInfo(ctx, kb.ID)
	if err != nil {
		t.Fatalf("KBVersionInfo: %v", err)
	}
	if info["stable_namespace"] != "v9" || info["canary_namespace"] != "v10" {
		t.Errorf("版本信息命名空间不对: %+v", info)
	}
}

func TestKBCanary_ModeParsing(t *testing.T) {
	cases := map[string]kbCanaryMode{
		"":          kbCanaryOff,
		"off":       kbCanaryOff,
		"FALSE":     kbCanaryOff,
		"0":         kbCanaryOff,
		"garbage":   kbCanaryOff,
		"shadow":    kbCanaryShadow,
		"observe":   kbCanaryShadow,
		"true":      kbCanaryShadow, // 习惯写法不给换键能力
		"1":         kbCanaryShadow,
		"yes":       kbCanaryShadow,
		"on":        kbCanaryOn,
		"ENFORCE":   kbCanaryOn,
		" active ":  kbCanaryOn,
		"nonsense2": kbCanaryOff,
	}
	for raw, want := range cases {
		if got := parseKBCanaryMode(raw); got != want {
			t.Errorf("parseKBCanaryMode(%q) = %s，期望 %s", raw, got, want)
		}
	}
	if KBCanaryFlagEnv != "FF_LTC_KB_CANARY" {
		t.Errorf("旗子名漂移：文档按 FF_LTC_KB_CANARY 写，代码却读 %s", KBCanaryFlagEnv)
	}
}
