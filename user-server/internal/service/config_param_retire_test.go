package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 本文件覆盖 2026-10-12「阶段六：不留配置债」引入的三件事：
//  1. 下架（Deprecated）——13 条接不上的旋钮从「过渡态」推进到「终态」；
//  2. 组元数据（config_param_groups）——侧栏中文名/排序的事实源从前端搬进库；
//  3. Unit / Risk——让页面能把「60」翻译成「60 秒」、能把填错会挂的旋钮标出来。

// markerToken 与门禁脚本 scripts/check-config-param-readpoints.py 里的 MARKER 同步。
// 这里写死字面量而不是从脚本 import：跨语言没有可靠的单一源，而它是个纯约定串，
// 两边不一致时下面 TestRetiredParamsHaveNoDeadMarker 会先红一次。
const markerToken = "未接线"

// retiredSeedKeys 是 16 条下架项的完整清单，逐条写死而不是从种子里过滤。
//
// 为什么写死：如果用「把种子里
// deprecated 的都算进来」来生成期望值，那有人误把一条正常参数标成下架时，
// 这条例外会跟着一起"通过"——用例再也发现不了。下架是一次不可逆的产品决定，
// 值得用字面量钉住。
var retiredSeedKeys = []struct{ group, key string }{
	{"bridge", "polling_max_timeout"},
	{"bridge", "polling_default_timeout"},
	{"bridge", "max_reply_content_bytes"},
	{"pagination", "page_max_size"},
	{"pagination", "page_default_size"},
	// D-7（2026-10-23）：chat_ws_ping_period / chat_ws_pong_wait / chat_ws_write_wait
	// 三条随 /ws/chat 死代码整体删除（连"已下架"的登记也一并移除——参数本身已不存在，
	// 页面上不该再有它的任何痕迹）。16 → 13。
	{"agent_tool", "result_cache_ttl"},
	{"sales", "insight_limit"},
	{"lock", "ai_lock_ttl"},
	{"frontend_ui", "websocket_reconnect_max_wait_ms"},
	{"frontend_ui", "toast_duration_ms"},
	{"feedback", "bandit_min_samples_for_exploit"},
	{"feedback", "bandit_exploration_floor"},
	{"feedback", "bandit_traffic_ceiling"},
	// 2026-11-26：journeyTracker 无导入或启动入口，删除后下架三个无消费方参数。
	{"frontend_ws", "journey_history_max"},
	{"frontend_ws", "journey_queue_max"},
	{"frontend_ws", "journey_flush_interval_ms"},
}

func seedDefsIndex() map[string]ParamDef {
	idx := make(map[string]ParamDef, len(DefaultParamDefs()))
	for _, d := range DefaultParamDefs() {
		idx[d.Group+"."+d.Key] = d
	}
	return idx
}

func retiredCoordinates() map[string]bool {
	out := map[string]bool{}
	for _, r := range retiredSeedKeys {
		out[r.group+"."+r.key] = true
	}
	return out
}

// TestRetiredSetMatchesTheRetiredList 钉住下架清单的边界：
// 一条不多（有人新标了一条下架要在这里解释）、一条不少（有人忘了标，门禁读数会漂）。
//
// 函数名原为 …AreExactlyTheElevenOnes，条数进了名字：10-12 那批从 9 涨到 11、
// 本轮又涨到 13，每次都得改名才能对上，而改名会打断按名字过滤子集的那类复跑。
// 判据本身与条数无关（两侧集合互相包含），所以把数字留在 retiredSeedKeys 里就够了。
func TestRetiredSetMatchesTheRetiredList(t *testing.T) {
	idx := seedDefsIndex()
	got := map[string]bool{}
	for coord, d := range idx {
		if d.Deprecated {
			got[coord] = true
		}
	}
	want := retiredCoordinates()
	for coord := range want {
		if !got[coord] {
			t.Errorf("%s 应标 Deprecated=true，但种子没标——它会继续在页面上提供一个改了不生效的旋钮", coord)
		}
	}
	for coord := range got {
		if !want[coord] {
			t.Errorf("%s 被标了 Deprecated，但它不在 retiredSeedKeys 里——"+
				"下架是一次产品决定，改这里要同时改清单", coord)
		}
	}
}

// TestEveryRetiredParamExplainsWhy 钉住 DeprecatedReason 非空且说清了两件事：
// 为什么接不上、要接上得先做什么。只有「已下架」三个字而没有理由的旋钮，
// 用户在页面上无从判断该不该找人来处理。
func TestEveryRetiredParamExplainsWhy(t *testing.T) {
	idx := seedDefsIndex()
	for _, r := range retiredSeedKeys {
		coord := r.group + "." + r.key
		d, ok := idx[coord]
		if !ok {
			t.Errorf("%s 不在 DefaultParamDefs() 里", coord)
			continue
		}
		reason := strings.TrimSpace(d.DeprecatedReason)
		if reason == "" {
			t.Errorf("%s 的 DeprecatedReason 是空的", coord)
			continue
		}
		if len([]rune(reason)) < 20 {
			t.Errorf("%s 的 DeprecatedReason 只有 %d 个字，大概率没写清「为什么接不上」：%q",
				coord, len([]rune(reason)), reason)
		}
	}
}

// TestRetiredParamsHaveNoDeadMarker 下架之后不能再挂着「未接线」那句。
//
// 两种标注在页面上讲的是同一件事（改了不生效），但语义不同：未接线是过渡态、
// 下架是终态。两个同时挂着，用户既以为它还可能被接上，又以为它已经彻底消失。
// 门禁那边也已把下架单列成第五类，文案里再留「未接线」只会让两套判据互相打架。
func TestRetiredParamsHaveNoDeadMarker(t *testing.T) {
	idx := seedDefsIndex()
	for _, r := range retiredSeedKeys {
		coord := r.group + "." + r.key
		d, ok := idx[coord]
		if !ok {
			continue
		}
		if !strings.Contains(d.Name, "已下架") {
			t.Errorf("%s 的 Name 里没有「已下架」：%q（用户要能一眼看出这个旋钮不生效）", coord, d.Name)
		}
		if strings.Contains(d.Name, markerToken) || strings.Contains(d.Description, markerToken) {
			t.Errorf("%s 仍带「%s」标注：%q / %q——下架是终态，别再留过渡态的措辞",
				coord, markerToken, d.Name, d.Description)
		}
	}
}

func newRetireFixture(t *testing.T) (*ConfigParamService, *repository.ConfigParamRepository, *gorm.DB) {
	t.Helper()
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{},
		&model.ConfigParamGroup{})
	if gdb == nil {
		t.Skip("no DB")
	}
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("SeedConfigParams: %v", err)
	}
	repo := repository.NewConfigParamRepository(gdb)
	svc := NewConfigParamService()
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc, repo, gdb
}

// TestListHidesRetiredParamsByDefault 钉住「管理端默认看不见下架项」。
//
// 口径为什么这样定：页面上出现一个能编辑的旋钮，用户改完就会去验证它。
// 那个旋钮如果不生效，这个「验证」必然失败，而失败的原因藏在代码里而不是页面上——
// 所以管理端宁可不列（List 默认过滤），也不要列出来再靠一句提示解释。
func TestListHidesRetiredParamsByDefault(t *testing.T) {
	svc, _, _ := newRetireFixture(t)
	ctx := context.Background()

	hidden, err := svc.List(ctx, false)
	if err != nil {
		t.Fatalf("List(false): %v", err)
	}
	shown, err := svc.List(ctx, true)
	if err != nil {
		t.Fatalf("List(true): %v", err)
	}
	if len(shown) != len(hidden)+len(retiredSeedKeys) {
		t.Errorf("List(true)=%d 条、List(false)=%d 条，差值 %d != 下架项数 %d",
			len(shown), len(hidden), len(shown)-len(hidden), len(retiredSeedKeys))
	}
	hiddenSet := map[string]bool{}
	for _, p := range hidden {
		hiddenSet[p.Group+"."+p.Key] = true
	}
	shownSet := map[string]bool{}
	for _, p := range shown {
		shownSet[p.Group+"."+p.Key] = true
	}
	for coord := range retiredCoordinates() {
		if hiddenSet[coord] {
			t.Errorf("%s 下架了却出现在默认列表里——运维会看到一个改了不生效的旋钮", coord)
		}
		if !shownSet[coord] {
			t.Errorf("%s 带 include_deprecated 也看不到——用户没法查历史值", coord)
		}
	}
}

// TestRuntimeCacheStillReadsRetiredParams 钉住读路径与管理端口径**故意相反**。
//
// 这是本批最容易被后来人"顺手统一"掉的一处。管理端不该给用户看改了不生效的旋钮；
// 但代码侧必须还读得到它——否则将来有人重新接线、忘了摘下架标记，那个旋钮会
// 变成"页面不显示、代码每读一次 miss 一次、每读一次静默回落兜底"的幽灵：
// 参数被改过、行为跟着变，而页面和日志里都找不到它在哪改的。
func TestRuntimeCacheStillReadsRetiredParams(t *testing.T) {
	_, repo, gdb := newRetireFixture(t)
	ctx := context.Background()

	got, err := repo.ListByGroup(ctx, "bridge", true)
	if err != nil {
		t.Fatalf("ListByGroup(true): %v", err)
	}
	found := false
	for _, p := range got {
		if p.Key == "polling_default_timeout" {
			found = true
		}
	}
	if !found {
		t.Fatal("读取路径 ListByGroup(includeDeprecated=true) 读不到已下架参数——" +
			"将来重新接线会变成每次读都 miss 的幽灵")
	}

	// 绕过护栏直接写库（模拟历史数据 / 直接改库），确认读取侧读到的是改后的值而不是 miss。
	if err := gdb.Model(&model.ConfigParam{}).
		Where(&model.ConfigParam{Group: "bridge", Key: "polling_default_timeout"}).
		Update("param_value", "45").Error; err != nil {
		t.Fatalf("直写库失败: %v", err)
	}
	fresh := NewConfigParamService()
	if v := fresh.GetDuration(ctx, "bridge", "polling_default_timeout", 7); v != 45*time.Second {
		t.Errorf("读取侧读到 %v，want 45s——已下架参数在读取路径上被静默丢弃了", v)
	}
}

// TestDeprecatedParamsRejectWrites 钉住三条写入路径都被 ErrParamDeprecated 拦下。
//
// 拦而不是"收下但当没这回事"：API 收下改动会写审计日志，页面上会提示保存成功，
// 用户接着去验证行为变化却什么也没发生——这比当场报错难查得多。
// （ReadOnly 另有一层检查，这里是第二道，两者不冲突。）
func TestDeprecatedParamsRejectWrites(t *testing.T) {
	svc, _, _ := newRetireFixture(t)
	ctx := context.Background()

	err := svc.UpdateValue(ctx, "bridge", "polling_default_timeout", "60", 1)
	if !errors.Is(err, repository.ErrParamDeprecated) {
		t.Errorf("UpdateValue(已下架) err=%v，want ErrParamDeprecated", err)
	}
	err = svc.ResetToDefault(ctx, "bridge", "polling_default_timeout", 1)
	if !errors.Is(err, repository.ErrParamDeprecated) {
		t.Errorf("ResetToDefault(已下架) err=%v，want ErrParamDeprecated", err)
	}
	// 整组重置必须跳过下架项，而不是让整组操作失败——
	// 否则「重置整个 bridge 组」会因为两条历史遗留而彻底用不了。
	if err := svc.BulkResetGroup(ctx, "bridge", 1); err != nil {
		t.Errorf("BulkResetGroup(含下架项的组) err=%v：整组重置应该跳过下架项，不该整组失败", err)
	}
}

// mustGetRawParam 直接从库里取原始行，不过任何一层过滤。
//
// 为什么不用仓储的 GetByGroupKey：它确实不过滤 deprecated，但它存在的意义是
// "service 类型化读取的底层"，用它来断言 DeprecatedAt 这类"只有 seed 才维护的
// 定义列"，会把「仓储读路径」和「seed 写路径」两件事混进一条断言里。直查库最直白。
func mustGetRawParam(ctx context.Context, t *testing.T, gdb *gorm.DB, group, key string) model.ConfigParam {
	t.Helper()
	var p model.ConfigParam
	if err := gdb.WithContext(ctx).
		Where(&model.ConfigParam{Group: group, Key: key}).First(&p).Error; err != nil {
		t.Fatalf("取 %s.%s 失败: %v", group, key, err)
	}
	return p
}

// TestDeprecatedAtIsWrittenOnlyOnce 钉住 DeprecatedAt 的时间戳语义。
//
// 它是「什么时候下架的」这一条排查线索的载体。只在 false→true 的那一次写：
// 每次启动都刷一遍的话，日期会变成启动时间，而运维拿它对不上「上周三那次改动后
// 行为变了没有」。反过来（true→false，即重新接线）清空它，避免留着一个陈旧的
// 下架日期骗人。
func TestDeprecatedAtIsWrittenOnlyOnce(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if gdb == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	// 先手工插一行非下架的参数再跑 seed：它已经在种子里，所以走 syncParamDef 而非 Create。
	if err := gdb.Create(&model.ConfigParam{
		Group: "bridge", Key: "polling_default_timeout", Name: "旧文案",
		Description: "旧说明", ValueType: "duration", Value: "30",
		DefaultValue: "30", Deprecated: false,
	}).Error; err != nil {
		t.Fatalf("插入基线行失败: %v", err)
	}
	if err := SeedConfigParams(ctx); err != nil {
		t.Fatalf("SeedConfigParams: %v", err)
	}

	first := mustGetRawParam(ctx, t, gdb, "bridge", "polling_default_timeout")
	if !first.Deprecated {
		t.Fatal("syncParamDef 没有把存量行的 Deprecated 刷成 true")
	}
	if first.DeprecatedAt == nil {
		t.Fatal("false→true 那次必须写 DeprecatedAt")
	}
	stamp := *first.DeprecatedAt

	// 第二次 seed：时间戳不该被刷新成"这次启动的时间"。
	time.Sleep(1100 * time.Millisecond) // 毫秒精度下两个 now() 必须可区分
	if err := SeedConfigParams(ctx); err != nil {
		t.Fatalf("第二次 SeedConfigParams: %v", err)
	}
	second := mustGetRawParam(ctx, t, gdb, "bridge", "polling_default_timeout")
	if second.DeprecatedAt == nil || !second.DeprecatedAt.Equal(stamp) {
		t.Errorf("DeprecatedAt 被第二次 seed 刷新了：%v → %v"+
			"（下架日期是排查「何时开始变」的线索，不能每次启动都冲掉）",
			stamp, second.DeprecatedAt)
	}
}

// ── 组元数据 ──

// TestParamGroupCoversEverySeedGroup 与 TestSeedGroupsAreAllDeclared 是一对：
// 前者保证「每个参数组都有中文名」，后者保证「没有为不存在的组写中文名」。
// 单向覆盖不够——只查前者的话，给已删组留一条孤儿记录照样能过，而 UI 会把一个
// 根本不存在的分组渲染出来。
func TestParamGroupCoversEverySeedGroup(t *testing.T) {
	declared := map[string]ParamGroupDef{}
	for _, d := range DefaultParamGroupDefs() {
		declared[d.Group] = d
	}
	for _, d := range DefaultParamDefs() {
		g, ok := declared[d.Group]
		if !ok {
			t.Errorf("参数组 %q 没有在 DefaultParamGroupDefs() 里登记中文名——侧栏会回落显示英文", d.Group)
			continue
		}
		if strings.TrimSpace(g.Label) == "" {
			t.Errorf("参数组 %q 的 Label 是空的", d.Group)
		}
	}
}

func TestSeedGroupsAreAllDeclared(t *testing.T) {
	used := map[string]bool{}
	for _, d := range DefaultParamDefs() {
		used[d.Group] = true
	}
	for _, d := range DefaultParamGroupDefs() {
		if !used[d.Group] {
			t.Errorf("DefaultParamGroupDefs() 里登记了参数表不存在的组 %q——UI 会渲染出一个空分组", d.Group)
		}
	}
}

func TestParamGroupLabelsAreUniqueAndSortOrdered(t *testing.T) {
	seen := map[string]string{}
	seenOrder := map[int]string{}
	for _, d := range DefaultParamGroupDefs() {
		if prev, dup := seen[d.Label]; dup {
			t.Errorf("组 %q 与 %q 的中文名都是 %q——"+
				"侧栏会出现两列一模一样的名字，用户分不出哪个改哪个", d.Group, prev, d.Label)
		}
		seen[d.Label] = d.Group
		if d.SortOrder < 0 {
			t.Errorf("组 %q 的 SortOrder=%d 是负数", d.Group, d.SortOrder)
		}
		if d.SortOrder != 0 {
			if prev, dup := seenOrder[d.SortOrder]; dup {
				t.Errorf("组 %q 与 %q 的 SortOrder 都是 %d——"+
					"侧栏里它们的先后会退化成按 group 名兜底排序，而那个顺序对用户没有意义",
					d.Group, prev, d.SortOrder)
			}
			seenOrder[d.SortOrder] = d.Group
		}
		if strings.TrimSpace(d.Description) == "" {
			t.Errorf("组 %q 没有说明——页面上折叠起来时用户看不到这个组管什么", d.Group)
		}
	}
}

// TestSortOrderZeroSortsLast 钉住「没排过序」的组排在末尾而不是最前。
//
// SortOrder=0 是"没排过"的哨兵值。如果直接 ORDER BY sort_order 升序，0 会顶到最前，
// 于是每加一个没配顺序的新组，侧栏顺序就在两次发布之间悄悄变——而顺序是组级属性，
// 运维是照着记忆在侧栏里找组的，顺序一飘就找不到。
// 这里必须在真库上跑：排序发生在 SQL 的 ORDER BY 里，Go 侧的分片排序测不到它。
func TestSortOrderZeroSortsLast(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParamGroup{})
	if gdb == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()
	repo := repository.NewConfigParamRepository(gdb)
	seed := []model.ConfigParamGroup{
		{Group: "zz_unranked", Label: "未排", SortOrder: 0},
		{Group: "aa_first", Label: "甲", SortOrder: 10},
		{Group: "bb_second", Label: "乙", SortOrder: 20},
	}
	if err := repo.UpsertGroups(ctx, seed); err != nil {
		t.Fatalf("UpsertGroups: %v", err)
	}
	got, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	order := make([]string, 0, len(got))
	for _, g := range got {
		order = append(order, g.Group)
	}
	want := []string{"aa_first", "bb_second", "zz_unranked"}
	if len(order) != len(want) {
		t.Fatalf("返回 %d 组，want %d：%v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("排序结果 = %v，want %v（sort_order=0 的组必须排最后）", order, want)
		}
	}
}

func TestSeedParamGroupsIsIdempotent(t *testing.T) {
	gdb := testutil.NewTestDBOrSkip(t, &model.ConfigParamGroup{})
	if gdb == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	if err := SeedParamGroups(ctx, repository.NewConfigParamRepository(gdb)); err != nil {
		t.Fatalf("SeedParamGroups: %v", err)
	}
	repo := repository.NewConfigParamRepository(gdb)
	first, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(first) != len(DefaultParamGroupDefs()) {
		t.Errorf("ListGroups 返回 %d 组，want %d", len(first), len(DefaultParamGroupDefs()))
	}

	// 再跑一次：行数不能变。UpsertGroups 用 OnConflict 逐组更新而不是整表重建，
	// 整表重建会抹掉运营手工排的序，也会把别的进程正在读的行短暂清空。
	if err := SeedParamGroups(ctx, repository.NewConfigParamRepository(gdb)); err != nil {
		t.Fatalf("第二次 SeedParamGroups: %v", err)
	}
	second, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("第二次 ListGroups: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("第二次 seed 后组数从 %d 变成 %d——UpsertGroups 整表重建了", len(first), len(second))
	}
}

func TestSeedParamGroupsRejectsNilDB(t *testing.T) {
	if err := SeedParamGroups(context.Background(), nil); !errors.Is(err, repository.ErrConfigParamStoreUnavailable) {
		t.Errorf("SeedParamGroups(nil) err=%v，want ErrConfigParamStoreUnavailable", err)
	}
}

// ── Unit / Risk ──

// TestRiskValuesAreFromTheKnownSet Risk 决定页面上要不要弹二次确认，
// 取值域必须是闭集：新增一个没被前端认识的字符串，效果等同于"这条旋钮没标危险"。
func TestRiskValuesAreFromTheKnownSet(t *testing.T) {
	allowed := map[string]bool{"": true, "warning": true, "danger": true}
	for _, d := range DefaultParamDefs() {
		if !allowed[d.Risk] {
			t.Errorf("%s.%s 的 Risk=%q 不在 {\"\", \"warning\", \"danger\"} 里——"+
				"前端认不出来，等于没标", d.Group, d.Key, d.Risk)
		}
	}
}

// TestDangerParamsHaveUnitOrDescription 危险旋钮是"填错了会出事"的那一批，
// 它们必须至少有一样能让人读懂的补充信息（单位或长说明）。
//
// 只有 Name 和一个数字时，用户无从判断 5 到底是"5 秒""5 MB"还是"5 条"，
// 而这一类旋钮恰恰是填错代价最高的。
func TestDangerParamsHaveUnitOrDescription(t *testing.T) {
	for _, d := range DefaultParamDefs() {
		if d.Risk != "danger" {
			continue
		}
		if strings.TrimSpace(d.Unit) == "" && len([]rune(strings.TrimSpace(d.Description))) < 15 {
			t.Errorf("%s.%s 标了 danger，却没有单位也没有像样的说明——"+
				"用户面对一个会出事的数字却读不出它的量纲", d.Group, d.Key)
		}
	}
}

// TestUnitIsNotFilledForDurationOrBool duration 的单位恒为秒、bool 没有单位，
// 填了就是"给每个消费者多一个可能填错的地方"——Unit 会原样显示在页面上。
func TestUnitIsNotFilledForDurationOrBool(t *testing.T) {
	for _, d := range DefaultParamDefs() {
		if (d.ValueType == "duration" || d.ValueType == "bool") && strings.TrimSpace(d.Unit) != "" {
			t.Errorf("%s.%s 的 ValueType=%s 却不该有 Unit=%q",
				d.Group, d.Key, d.ValueType, d.Unit)
		}
	}
}
