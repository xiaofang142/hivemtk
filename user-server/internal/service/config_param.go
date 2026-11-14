package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	llmpkg "hivemtk-user/internal/aiagent/llm"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

// errConfigParamNotFound 把仓储层的 gorm.ErrRecordNotFound 折成业务码 4004。
// 控制器原本把 "record not found" 原样透传且 code=400，页面读起来既不像
// 「参数不存在」也违反 API 规范（404=不存在），这里统一收口。
func errConfigParamNotFound(group, key string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bizerr.New(bizerr.CodeNotFound, fmt.Sprintf("配置参数不存在（%s.%s）", group, key))
	}
	return err
}

type paramEntry struct {
	value     string
	expiresAt time.Time
}

const configParamTTL = 60 * time.Second

// ConfigParamService 动态参数服务
//
// 对外提供 GetInt/GetFloat/GetBool/GetDuration/GetString 五个类型化读取方法。
// 启动时 Seed 一次，运行期读操作走内存缓存（sync.RWMutex），写操作后失效缓存。
//
// 设计约束：
//   - 不允许 nil DB（启动必须 Migrate + Seed）
//   - 缓存 key = "group.key"（off 模式）/ "t{tenantID}|group.key"（on 模式）
//   - 读操作在缓存 miss 时拉一次 group 全量（同组一次性加载，减少 DB 往返）
//   - on 模式读取顺序：本租户覆盖行 → config_params（平台默认值）→ 代码兜底；
//     写操作只落 tenant_config_params，绝不改平台默认值
type ConfigParamService struct {
	repo       *repository.ConfigParamRepository
	tenantRepo *repository.TenantConfigParamRepository

	mu     sync.RWMutex
	cache  map[string]paramEntry
	loaded map[string]bool
	nowFn  func() time.Time
}

var (
	globalConfigParamMu sync.RWMutex
	globalConfigParam   *ConfigParamService
)

// NewConfigParamService 构造（句柄由 repository 自取全局库；main 启动时调用）
func NewConfigParamService() *ConfigParamService {
	return &ConfigParamService{
		repo:       repository.NewConfigParamRepositoryDefault(),
		tenantRepo: repository.NewTenantConfigParamRepositoryDefault(),
		cache:      make(map[string]paramEntry, 256),
		loaded:     make(map[string]bool, 32),
		nowFn:      time.Now,
	}
}

// SetGlobal 装配层注入单例（各 module 通过 GlobalConfigParam() 读取）。
//
// 语义：**后写覆盖**，不是"只生效一次"。
//
// 本函数不能用 sync.Once：
// 于是第二次及以后的 SeedConfigParams 造出的新实例被**静默丢弃**，全局仍指向首个实例。
// 而 ConfigParamService 是按 key 的**负缓存**（未命中即把默认值写入 cache 并置 loaded），
// 因此旧实例会把"表还空着时读到的 0"永久固化 —— 之后再怎么 seed 都读不回真值。
// 测试侧的表现就是 TestSeedConfigParams 读到全零（bridge.polling_max_timeout = 0s）。
//
// 生产侧 cmd/api/main.go:119 只调用一次，故改为后写覆盖对生产**无行为变化**；
// 同时本函数原先是无锁写、GlobalConfigParam 无锁读，在 -race 下本身即数据竞争，
// 现统一用 RWMutex 保护。
func SetGlobal(svc *ConfigParamService) {
	globalConfigParamMu.Lock()
	globalConfigParam = svc
	globalConfigParamMu.Unlock()
}

// SetGlobalForTest 测试专用：强制替换全局实例。
// 生产代码禁止调用——生产一律走 SetGlobal/NewConfigParamService+Seed。
// 语义已与 SetGlobal 一致（后写覆盖），保留本名是为了让"这是测试在动全局状态"这件事在调用点可见。
func SetGlobalForTest(svc *ConfigParamService) {
	SetGlobal(svc)
}

// GlobalConfigParam 获取全局单例（nil-safe：返回一个无 DB 的 fallback stub，
// 所有 Get* 方法走 fallback 默认值而非 panic）
func GlobalConfigParam() *ConfigParamService {
	globalConfigParamMu.RLock()
	svc := globalConfigParam
	globalConfigParamMu.RUnlock()
	if svc != nil {
		return svc
	}

	return &ConfigParamService{
		cache:  make(map[string]paramEntry),
		loaded: make(map[string]bool),
	}
}

// BridgeOnlineGraceParam / BridgeOnlineGraceDefault 是"这台桥接账号此刻够得着吗"的唯一时长来源。
//
// 在线判定散在四个读侧（管理面列表、渠道总览计数、主动触达选号、清扫脚本），各自写死一份秒数
// 就会造出互相矛盾的报告：列表说在线、总览报另一个数、发出去的出站行没人来取。
// 默认值与 config_params 里 bridge/online_grace_window 的 DefaultValue("30") 同值，改动要一起改。
const (
	BridgeOnlineGraceParam   = "online_grace_window"
	BridgeOnlineGraceDefault = 30 * time.Second
)

// BridgeOnlineGraceWindow 读在线宽限窗口；参数未装配时按默认值（GlobalConfigParam 本身 nil-safe）。
func BridgeOnlineGraceWindow(ctx context.Context) time.Duration {
	return GlobalConfigParam().GetDuration(ctx, "bridge", BridgeOnlineGraceParam, BridgeOnlineGraceDefault)
}

// BridgeDeliverRateLimitParam / BridgeDeliverRateLimitDefault 是"服务端下发限速兜底"的唯一来源。
//
// 权威架构文档 BRIDGE-ARCHITECTURE-2026-08-14.md §4.2 明文承诺「限速（后端）：兜底 60/min
// （防 AI 失控）」，但代码从未落地（2026-11-14 二次论证全库 grep 零命中）——本条补齐承诺。
// 语义：单 (channel, account_id) 每 60s 内最多被认领下发 N 次（认领即「交给桥端去发」，
// 计数口径= outbound 行 claimed_at）。前端本有拟人节奏限速（rate-limiter.js 三层），这里
// 是服务端最后防线：前端被关闭/篡改/AI 批量误生成时不至于向平台页面倾泻消息。
// 0 = 停用兜底（前端限速独挑）；默认 60 与文档承诺同值。
// 默认值与 config_params 里 bridge/deliver_rate_limit_per_min 的 DefaultValue("60") 同值，改动要一起改。
const (
	BridgeDeliverRateLimitParam   = "deliver_rate_limit_per_min"
	BridgeDeliverRateLimitDefault = 60
)

// BridgeDeliverRateLimitPerMin 读服务端下发限速兜底阈值；参数未装配时按默认值（nil-safe）。
func BridgeDeliverRateLimitPerMin(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, "bridge", BridgeDeliverRateLimitParam, BridgeDeliverRateLimitDefault)
}

// PatrolUnreadFirstChannelsParam / PatrolUnreadFirstChannelsDefault 是"巡检要不要未读优先"的唯一下发源。
//
// M-5（2026-11-11）巡检机制优化：扩展 patrol 原先每 30s 全量遍历会话列表（未读数恒为 0、
// 各渠道 detectUnread 的结果被丢弃）。开启未读优先的渠道，巡检只点开带未读徽章的会话，
// 未读为 0 的会话靠活动会话 MutationObserver + 全量 rescan 兜底。渠道徽章检测不可靠时
// 把渠道移出清单即回退现行顺序遍历（关 = 完全旧行为）。
// 默认值与 config_params 里 bridge/patrol_unread_first_channels 的 DefaultValue 同值，改动要一起改。
const (
	PatrolUnreadFirstChannelsParam   = "patrol_unread_first_channels"
	PatrolUnreadFirstChannelsDefault = `["douyin","tiktok","xianyu","kuaishou"]`
)

// PatrolUnreadFirstChannels 读巡检未读优先渠道清单（JSON 字符串数组）；参数未装配/解析失败
// 按默认清单回落（GlobalConfigParam 本身 nil-safe）。空数组合法 = 全渠道关（现行行为）。
func PatrolUnreadFirstChannels(ctx context.Context) []string {
	raw := GlobalConfigParam().GetString(ctx, "bridge", PatrolUnreadFirstChannelsParam, PatrolUnreadFirstChannelsDefault)
	return parsePatrolUnreadFirstChannels(raw)
}

// parsePatrolUnreadFirstChannels 把参数值解析为渠道清单。
// 非法 JSON / 非字符串数组 → 回落默认清单（fail-open 到「已知渠道未读优先」，与未装配同形）；
// 空数组合法 = 全渠道关（扩展侧顺序遍历现行行为）。
func parsePatrolUnreadFirstChannels(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[ConfigParam] bridge/%s 解析失败（%v），回落默认清单", PatrolUnreadFirstChannelsParam, err)
		out = nil
	}
	if out == nil {
		_ = json.Unmarshal([]byte(PatrolUnreadFirstChannelsDefault), &out)
	}
	return out
}

const (
	AbExposureBufferParam      = "ab_exposure_buffer"
	InboxOverdueThresholdParam = "inbox_overdue_threshold"
	InboxStaffLoadLimitParam   = "inbox_default_staff_load_limit"
)

// AbExposureBufferSize 读 AB 实验曝光异步落库缓冲容量，未装配时回落默认 AbExposureBuffer=1024。
func AbExposureBufferSize(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, "misc", AbExposureBufferParam, AbExposureBuffer)
}

// InboxOverdueWindow 读收件箱会话超时标记为 overdue 的时长，未装配时回落默认 InboxOverdueThreshold=30m。
func InboxOverdueWindow(ctx context.Context) time.Duration {
	return GlobalConfigParam().GetDuration(ctx, "session", InboxOverdueThresholdParam, InboxOverdueThreshold)
}

// InboxStaffLoadLimit 读单个客服默认负载上限，未装配时回落默认 InboxDefaultStaffLoadLimit=30。
func InboxStaffLoadLimit(ctx context.Context) int {
	return GlobalConfigParam().GetInt(ctx, "session", InboxStaffLoadLimitParam, InboxDefaultStaffLoadLimit)
}

// SeedConfigParams 启动时调用：AutoMigrate + Upsert 默认参数。
// 首次启动会写入全部 60+ 参数；后续启动只补齐缺失项，不覆盖用户已改值。
func SeedConfigParams(ctx context.Context) error {
	// 建表已收敛到启动期 AutoMigrate（db.RegisterExtraModels），此处仅 seed。
	// 表自检与行写入都在仓储侧：本函数只做「读种子定义 → 比对 → 决定增/刷」，
	// 不持有查询能力（五层边界）。
	repo := repository.NewConfigParamRepositoryDefault()
	if err := repo.TableReady(ctx); err != nil {
		return fmt.Errorf("config_params 表不可用: %w", err)
	}
	svc := NewConfigParamService()
	SetGlobal(svc)

	knowledgesvc.SetConfigReader(svc)

	ctxBG := context.Background()
	llmpkg.SetEmbeddingMaxBatchGetter(func() int {
		return svc.GetInt(ctxBG, "knowledge", "embedding_max_batch", 64)
	})
	platform.SetHeartbeatIntervalGetter(func() time.Duration {
		return svc.GetDuration(ctxBG, "misc", "heartbeat_interval", 3*time.Minute)
	})

	var created, existing, refreshed int
	// seed 失败的参数**必须冒泡**。
	//
	// 插入失败（比如撞唯一索引）后这一条
	// 就不存在了——管理台看不到它、代码里的 Get* 永远读到 fallback，
	// 而启动日志里只有一行 Warn，混在几百行 INFO 里没人会注意到。
	// 于是「加一条参数」变成了一件要看运气的操作：参数可能根本没进库，
	// 而所有人都会以为它进库了。攒起来返回 error，让调用方至少在启动日志里看到红字。
	var seedFailures []string
	for _, def := range DefaultParamDefs() {
		p, err := repo.GetByGroupKey(ctx, def.Group, def.Key)
		if err == nil && p != nil {
			existing++
			if syncParamDef(ctx, repo, p, def) {
				refreshed++
			}
			continue
		}

		if err := repo.Create(ctx, &model.ConfigParam{
			Group:            def.Group,
			Key:              def.Key,
			Name:             def.Name,
			Description:      def.Description,
			ValueType:        def.ValueType,
			Value:            def.DefaultValue,
			DefaultValue:     def.DefaultValue,
			Min:              def.Min,
			Max:              def.Max,
			Step:             def.Step,
			Unit:             def.Unit,
			Risk:             def.Risk,
			Deprecated:       def.Deprecated,
			DeprecatedAt:     deprecatedAtIf(def.Deprecated),
			DeprecatedReason: def.DeprecatedReason,
			ReadOnly:         def.ReadOnly,
			Restart:          def.Restart,
			Category:         def.Category,
		}); err != nil {
			logger.Errorf("[ConfigParam] seed create %s.%s failed: %v（这条参数没有进库：管理台看不到、代码只会读兜底值）",
				def.Group, def.Key, err)
			seedFailures = append(seedFailures, fmt.Sprintf("%s/%s: %v", def.Group, def.Key, err))
			continue
		}
		created++
	}
	logger.Infof("[ConfigParam] seed done: created=%d existing=%d refreshed_defs=%d total_defs=%d failed=%d",
		created, existing, refreshed, len(DefaultParamDefs()), len(seedFailures))
	if len(seedFailures) > 0 {
		return fmt.Errorf("参数种子有 %d/%d 条没进库（这些参数等于不存在：管理台不显示、代码读兜底值）：%s",
			len(seedFailures), len(DefaultParamDefs()), strings.Join(seedFailures, "; "))
	}
	return nil
}

// SeedParamGroups 把分组元数据（中文名/说明/排序）upsert 进库。启动期调用，与
// SeedConfigParams 分开。
//
// 为什么分成两次 seed：这两件事的失败后果根本不同级。参数值没进库 ⇒ 管理台看不到、
// 代码读兜底值，那条链路的行为与声明不一致，必须冒泡（0.3 起 seed 失败会返回 error）。
// 而组元数据只决定侧栏那列中文名，读不到时前端回落显示英文 group key——
// 纯外观降级，功能不残废。把两者绑在一起的后果是：任何一个只声明 &model.ConfigParam{}
// 的测试 fixture 都会在 seed 那一步撞 `relation "config_param_groups" does not exist`。
// 曾为此在 testutil 里藏耦合（靠 reflect 拿类型名绕开 import 成环），那等于把
// 「哪张表和哪张表绑着」这条知识藏进一个所有测试共用的黑箱里，而它本可以由两个独立的
// 入口自然表达。改为独立入口后：失败只降级外观、只在 main.go 一处调用、
// 且各有各的测试。
//
// 五层边界 §3.2：本函数收仓储而不是 *gorm.DB——句柄→仓储的转换在装配点
// （cmd/api/main.go）完成。repo 为 nil（或仓储没装句柄）时返回
// ErrConfigParamStoreUnavailable，调用方据此降级外观而不阻断启动。
func SeedParamGroups(ctx context.Context, repo *repository.ConfigParamRepository) error {
	if repo == nil {
		return repository.ErrConfigParamStoreUnavailable
	}
	return seedParamGroups(ctx, repo)
}

// seedParamGroups 把分组元数据 upsert 进库，并清掉"种子里已经没有、且组内再无活参数"的组。
//
// 为什么还要清：分组表只有 upsert、没有删除路径，于是「把某个组从种子里摘掉」在
// 存量库上等于没摘——组行还在，侧栏照样渲染它（前端有意保留 0 条数的组，好让
// 「全部参数已下架」的组仍可点进去），点进去是一张空表，外加一个已经下架的旋钮。
// 那正是 ConfigParam.Deprecated 那段注释要避免的东西，只是升了一级（从"失效的旋钮"
// 变成"失效的分组"）。实测样本：D-7 删掉的 "wechat" 组在库里带了 3 条全 deprecated
// 的参数，侧栏一直挂着「微信 0」。
//
// 两条边界：① 只在种子是唯一写入方时才这么做（本表确实只有本函数写，无新建分组接口）；
// ② 组内**还有一条活参数就不删**——删了会把真正的旋钮连入口一起藏掉，代价远大于
// 留一个空组。
func seedParamGroups(ctx context.Context, repo *repository.ConfigParamRepository) error {
	defs := DefaultParamGroupDefs()
	if len(defs) == 0 {
		return nil
	}
	rows := make([]model.ConfigParamGroup, 0, len(defs))
	keep := make([]string, 0, len(defs))
	for _, d := range defs {
		rows = append(rows, model.ConfigParamGroup{
			Group:       d.Group,
			Label:       d.Label,
			Description: d.Description,
			SortOrder:   d.SortOrder,
		})
		keep = append(keep, d.Group)
	}
	if err := repo.UpsertGroups(ctx, rows); err != nil {
		return err
	}
	pruned, err := repo.PruneRetiredGroups(ctx, keep)
	if err != nil {
		// 清理失败只降级为"多留几个空组"，不能反过来让 seed 报错：
		// 组元数据只决定侧栏文案，功能不残废（与 SeedParamGroups 分级口径一致）。
		logger.Warnf("[ConfigParam] 清理已下架分组失败（侧栏会多留空组）: %v", err)
		return nil
	}
	if pruned > 0 {
		logger.Infof("[ConfigParam] 已清理 %d 个已下架分组（种子里已无该组，且组内无在用参数）", pruned)
	}
	return nil
}

// syncParamDef 把存量行的**定义列**对齐当前种子定义。
//
// 为什么必须对齐：SeedConfigParams 原本只补插缺失行，于是种子里改文案等于没改——
// 存量库继续展示建库那一次写进去的旧说明。实测代价：bridge 的两条轮询超时参数的说明写着
// 「超时由服务端主动返回」，而服务端从来没有长轮询实现（ingest 立即返回，回复走 SSE 推送
// 或扩展侧 /outbox 轮询），运营在管理台改这个值不会有任何效果，页面上那句话却一直是旧的。
// 种子是这些文案的唯一事实源，不刷新就等于没有事实源。
//
// 唯一不许碰的是 value：运营改过的那个值才是这条链路上要保住的东西。
func syncParamDef(ctx context.Context, repo *repository.ConfigParamRepository, p *model.ConfigParam, def ParamDef) bool {
	updates := map[string]any{}
	if p.Name != def.Name {
		updates["Name"] = def.Name
	}
	if p.Description != def.Description {
		updates["Description"] = def.Description
	}
	if p.ValueType != def.ValueType {
		updates["ValueType"] = def.ValueType
	}
	if p.DefaultValue != def.DefaultValue {
		updates["DefaultValue"] = def.DefaultValue
	}
	if !sameOptionalPtr(p.Min, def.Min) {
		updates["Min"] = def.Min
	}
	if !sameOptionalPtr(p.Max, def.Max) {
		updates["Max"] = def.Max
	}
	if !sameOptionalPtr(p.Step, def.Step) {
		updates["Step"] = def.Step
	}
	if p.ReadOnly != def.ReadOnly {
		updates["ReadOnly"] = def.ReadOnly
	}
	if p.Restart != def.Restart {
		updates["Restart"] = def.Restart
	}
	if p.Category != def.Category {
		updates["Category"] = def.Category
	}
	if p.Unit != def.Unit {
		updates["Unit"] = def.Unit
	}
	if p.Risk != def.Risk {
		updates["Risk"] = def.Risk
	}
	if p.DeprecatedReason != def.DeprecatedReason {
		updates["DeprecatedReason"] = def.DeprecatedReason
	}
	if p.Deprecated != def.Deprecated {
		updates["Deprecated"] = def.Deprecated
		// DeprecatedAt 只在 false→true 的那一刻写。
		// 每次启动都刷新的话，「什么时候下架的」会被冲成最近一次启动时间，
		// 而排查「上周三那次改动之后行为变了没有」时这个字段就是唯一线索。
		// 反向（true→false，重新接线）则清空：它不再是下架的行，留个下架时间只会误导。
		if def.Deprecated {
			updates["DeprecatedAt"] = time.Now()
		} else {
			updates["DeprecatedAt"] = nil
		}
	}
	if len(updates) == 0 {
		return false
	}
	// 按主键定位，不再用 (group,key) 条件：这两列本身也可能在本次更新里。
	if err := repo.ApplyDefinitionUpdates(ctx, p.ID, updates); err != nil {
		logger.Warnf("[ConfigParam] seed 参数定义刷新失败 %s.%s: %v（管理台会继续展示旧说明，事实订正到不了运营眼前）",
			def.Group, def.Key, err)
		return false
	}
	return true
}

func sameOptionalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// deprecatedAtIf 新建一行时给下架条目打上时刻；非下架返回 nil。
func deprecatedAtIf(deprecated bool) *time.Time {
	if !deprecated {
		return nil
	}
	now := time.Now()
	return &now
}

func (s *ConfigParamService) GetInt(ctx context.Context, group, key string, fallback int) int {
	v, ok := s.getString(ctx, group, key)
	if !ok {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func (s *ConfigParamService) GetFloat(ctx context.Context, group, key string, fallback float64) float64 {
	v, ok := s.getString(ctx, group, key)
	if !ok {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func (s *ConfigParamService) GetBool(ctx context.Context, group, key string, fallback bool) bool {
	v, ok := s.getString(ctx, group, key)
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// GetDuration 值单位为秒（float 可带小数），返回 time.Duration
func (s *ConfigParamService) GetDuration(ctx context.Context, group, key string, fallback time.Duration) time.Duration {
	v, ok := s.getString(ctx, group, key)
	if !ok {
		return fallback
	}

	sec, err := parseDurationSeconds(v)
	if err != nil {
		return fallback
	}
	return time.Duration(sec * float64(time.Second))
}

// parseDurationSeconds 把 duration 参数的原始字符串解析成「秒」。
//
// 这是读取侧（GetDuration）与校验侧（validateValue）共用的唯一口径：
// 先试 Go duration 字面量（"30s"/"5m"/"1h30m"），再退到「裸数字 = 秒」（"30"/"0.5"）。
//
// 两侧必须共用一个函数，否则会裂成两种形态：
//   - 校验只认裸数字、读取认 duration 字面量 → 写进去的值读出来是 fallback（静默失效）
//   - 校验解析失败就放过 → 任意值都能绕过 min/max 写进库
//
// 现存 duration 参数的 DefaultValue/Min/Max/Step 一律是裸秒数（如 "500"/"3600"），
// 走的是第二条路径；Go 字面量是为「运维在页面上习惯性敲 30s」留的口子。
func parseDurationSeconds(raw string) (float64, error) {
	if d, err := time.ParseDuration(raw); err == nil {
		return d.Seconds(), nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("value %q not valid duration: want Go duration (30s/5m/1h30m) or a bare seconds number", raw)
	}
	return f, nil
}

func (s *ConfigParamService) GetString(ctx context.Context, group, key, fallback string) string {
	v, ok := s.getString(ctx, group, key)
	if !ok {
		return fallback
	}
	return v
}

func (s *ConfigParamService) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// tenantParamScope 返回当前读取/写入应使用的租户。
//
// ok=false 有两种语义，调用方必须区分：
//   - off 模式：单机行为，读写都走实例级 config_params
//   - on 模式但 ctx 未绑定租户：读取允许回落平台默认值（不是别的租户的数据），
//     写入必须拒绝（见 UpdateValue），否则一次漏绑中间件就会改写所有人的默认值
func tenantParamScope(ctx context.Context) (uint, bool) {
	if !tenant.IsMultiTenant() {
		return 0, false
	}
	return tenant.FromContext(ctx)
}

func tenantCachePrefix(tenantID uint) string {
	if tenantID == 0 {
		return ""
	}
	return "t" + strconv.FormatUint(uint64(tenantID), 10) + "|"
}

func (s *ConfigParamService) getString(ctx context.Context, group, key string) (string, bool) {
	tenantID, scoped := tenantParamScope(ctx)
	prefix := tenantCachePrefix(tenantID)
	cacheKey := prefix + group + "." + key
	loadKey := prefix + group

	now := s.now()
	s.mu.RLock()
	e, hit := s.cache[cacheKey]
	expired := hit && !now.Before(e.expiresAt)
	s.mu.RUnlock()
	if hit && !expired {
		return e.value, true
	}
	if expired {
		s.mu.Lock()
		delete(s.cache, cacheKey)
		s.loaded[loadKey] = false
		s.mu.Unlock()
	}

	if s.repo == nil {
		return "", false
	}

	s.mu.Lock()
	if s.loaded[loadKey] {

		e, ok := s.cache[cacheKey]
		s.mu.Unlock()
		if ok {
			return e.value, true
		}
		return "", false
	}

	s.loaded[loadKey] = true
	s.mu.Unlock()

	// 这里传 includeDeprecated=true，与管理端 List 的口径**故意相反**：
	// 管理端列表不该给用户看改了不生效的旋钮，但代码读取路径必须能读到它。
	// 两者混用会造成一类极隐蔽的故障：有人重新接线了某个旋钮却忘了去掉下架标记，
	// 代码侧从此每次读都 miss、每次静默回落兜底值，而页面上它明明白白写着「可配置」。
	// 让读取侧无视下架标记，这个故障就只剩下「该去把标记摘掉」这一种解法。
	params, err := s.repo.ListByGroup(ctx, group, true)
	if err != nil {

		logger.Warnf("[ConfigParam] load group %s failed: %v", group, err)
		s.mu.Lock()
		s.loaded[loadKey] = false
		s.mu.Unlock()
		return "", false
	}

	// 先铺平台默认值，再让本租户覆盖行逐键顶掉。
	effective := make(map[string]string, len(params))
	for _, p := range params {
		effective[p.Key] = p.Value
	}
	if scoped && s.tenantRepo != nil {
		// OnlyTenant：读取路径的 ctx 可能带着 SystemScope（后台任务/迁移），
		// 隔离 callback 见到它就会跳过租户条件、读到跨租户 union。
		// 这里必须钉死在当前租户上，不能把两套数据混进同一张 effective 表。
		tctx := tenant.OnlyTenant(ctx, tenantID)
		overrides, oerr := s.tenantRepo.ListByGroup(tctx, tenantID, group)
		if oerr != nil {
			// 覆盖行读失败不阻断读取：平台默认值是合法值，不是别的租户数据。
			// 但必须出声——否则租户改了参数却不生效，日志里一行痕迹都没有。
			logger.Warnf("[ConfigParam] load tenant %d group %s overrides failed（本次按平台默认值提供）: %v", tenantID, group, oerr)
		} else {
			for _, o := range overrides {
				effective[o.Key] = o.Value
			}
		}
	}

	s.mu.Lock()
	filledAt := s.now().Add(configParamTTL)
	for k, v := range effective {
		s.cache[prefix+group+"."+k] = paramEntry{value: v, expiresAt: filledAt}
	}
	s.mu.Unlock()

	s.mu.RLock()
	e, ok := s.cache[cacheKey]
	s.mu.RUnlock()
	if ok {
		return e.value, true
	}
	return "", false
}

// List 返回全部参数（管理端 CRUD）
// repoNotInitializedErr 是「参数中心没装 DB」的统一哨兵。返回 error 而不是 panic：
// 装配顺序出错（setupConfigParamRoutes 早于 SeedConfigParams）时这条路径必然被走到，
// panic 会让整个进程起不来，而 error 只是让这一次查询空手——前者把一次配置面故障
// 放大成整站不可用。
var repoNotInitializedErr = repository.ErrConfigParamStoreUnavailable

func (s *ConfigParamService) List(ctx context.Context, includeDeprecated bool) ([]model.ConfigParam, error) {
	if s.repo == nil {
		return nil, repoNotInitializedErr
	}
	params, err := s.repo.List(ctx, includeDeprecated)
	if err != nil {
		return nil, err
	}
	return s.applyTenantOverrides(ctx, params)
}

// ListByGroup 按分组返回参数
func (s *ConfigParamService) ListByGroup(ctx context.Context, group string, includeDeprecated bool) ([]model.ConfigParam, error) {
	if s.repo == nil {
		return nil, repoNotInitializedErr
	}
	params, err := s.repo.ListByGroup(ctx, group, includeDeprecated)
	if err != nil {
		return nil, err
	}
	return s.applyTenantOverrides(ctx, params)
}

// applyTenantOverrides 把定义行的 Value 换成「该租户的生效值」。
//
// 返回的仍是 model.ConfigParam：管理端页面结构与校验边界（Min/Max/Step/ValueType）
// 都来自定义，租户只覆盖值。未覆盖的键保持平台默认值。
// 与读取路径不同，这里覆盖行查失败必须返回 error——管理端把平台默认值当成
// 「当前租户的值」展示，会让管理员以为自己改过（或没改过），比报错更糟。
func (s *ConfigParamService) applyTenantOverrides(ctx context.Context, params []model.ConfigParam) ([]model.ConfigParam, error) {
	// 租户可见性先盖再判：它是对「这个键能不能按租户改」的回答，
	// 与本次请求有没有租户上下文、有没有覆盖行都无关。
	// 放在下面两个提前返回之后，平台管理员看到的列表里这一列会整列空白。
	stampTenantScope(params)
	tenantID, scoped := tenantParamScope(ctx)
	if !scoped || s.tenantRepo == nil || len(params) == 0 {
		return params, nil
	}
	overrides, err := s.tenantRepo.ListAll(tenant.OnlyTenant(ctx, tenantID), tenantID)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return params, nil
	}
	byKey := make(map[string]string, len(overrides))
	for _, o := range overrides {
		byKey[o.Group+"\x00"+o.Key] = o.Value
	}
	for i := range params {
		if v, ok := byKey[params[i].Group+"\x00"+params[i].Key]; ok {
			params[i].Value = v
		}
	}
	return params, nil
}

// Groups 返回分组元数据（中文名 / 说明 / 排序），管理端侧栏直接渲染这一份。
func (s *ConfigParamService) Groups(ctx context.Context) ([]model.ConfigParamGroup, error) {
	if s.repo == nil {
		return nil, repoNotInitializedErr
	}
	return s.repo.ListGroups(ctx)
}

// UpdateValue 更新单个参数值（会做范围校验 + 失效缓存）
func (s *ConfigParamService) UpdateValue(ctx context.Context, group, key, newValue string, actorID uint) error {
	if s.repo == nil {
		return fmt.Errorf("config_param repo not initialized")
	}

	// on 模式：写租户覆盖行，平台默认值不动。
	// 没有租户上下文时**拒绝写入**而不是回落实例级：回落会让一次漏绑中间件
	// 的请求把某个租户的改动写成全体租户的新默认值，且审计里看不出是谁干的。
	if tenantID, scoped := tenantParamScope(ctx); scoped {
		if s.tenantRepo == nil {
			return repoNotInitializedErr
		}
		p, err := s.definitionForWrite(ctx, group, key)
		if err != nil {
			return err
		}
		// 租户覆盖只在「读取点把请求 ctx 交进来」时才生效。这一层拦的是：读取点还在按
		// 平台域解析时，一次「保存成功」的回执配上照旧的行为，是本页最难查的一类不一致。
		//
		// 只拦写不拦重置（ResetToDefault 不走这里）：删掉一条不生效的覆盖行是往正确方向
		// 走的一步，把它一起拦下就等于没收唯一能清理遗留覆盖行的入口。
		if err := tenantScopedWriteErr(group, key); err != nil {
			return err
		}
		newValue, err = normalizeValueForType(p.ValueType, newValue)
		if err != nil {
			return err
		}
		if err := validateValue(p.ValueType, newValue, p.Min, p.Max); err != nil {
			return err
		}
		oldValue, _ := s.getString(ctx, group, key)
		if err := s.tenantRepo.UpsertOverride(tenant.OnlyTenant(ctx, tenantID), tenantID, group, key, newValue, oldValue, actorID); err != nil {
			return err
		}
		s.invalidateTenant(tenantID, group, key)
		return nil
	}
	if tenant.IsMultiTenant() {
		return repository.ErrTenantParamContextMissing
	}

	p, err := s.repo.GetByGroupKey(ctx, group, key)
	if err != nil {
		return errConfigParamNotFound(group, key, err)
	}
	// 只读在 service 层先判一次：仓储层同样会拦，但它拦下时给的是
	// 「这一行不存在」那种形状，而这一行明明就在页面上。
	if p.ReadOnly {
		return repository.ErrParamReadOnly
	}
	if err := validateValue(p.ValueType, newValue, p.Min, p.Max); err != nil {
		return err
	}
	newValue, err = normalizeValueForType(p.ValueType, newValue)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateValue(ctx, group, key, newValue, actorID); err != nil {
		return err
	}

	s.invalidate(group, key)
	return nil
}

// definitionForWrite 取平台定义并做只读/下架校验（租户写路径共用）。
func (s *ConfigParamService) definitionForWrite(ctx context.Context, group, key string) (*model.ConfigParam, error) {
	p, err := s.repo.GetByGroupKey(ctx, group, key)
	if err != nil {
		return nil, errConfigParamNotFound(group, key, err)
	}
	if p.ReadOnly {
		return nil, repository.ErrParamReadOnly
	}
	if p.Deprecated {
		return nil, repository.ErrParamDeprecated
	}
	return p, nil
}

// normalizeValueForType 写侧归一化。
//
// 布尔一律折成 "true"/"false" 再入库。写侧若放过 "1"/"t"/"TRUE" 这类别名，
// 读取侧 ParseBool 认它、页面却按字符串比 'true'（编辑器里开关显示为关、表格里
// 显示为 1），配置生效与否和它长什么样分家——这是最难查的一种不一致。
func normalizeValueForType(valueType, newValue string) (string, error) {
	if valueType != "bool" {
		return newValue, nil
	}
	b, err := strconv.ParseBool(newValue)
	if err != nil {
		return "", err
	}
	return strconv.FormatBool(b), nil
}

// UpdatePlatformDefault 更新**平台默认值**（config_params 表本体），不写租户覆盖行。
//
// 为什么需要这条独立入口：SaaS 模式下 UpdateValue 看到请求 ctx 带着租户绑定，
// 就一律写 tenant_config_params 覆盖行 —— 这对「按租户分叉的参数」是对的，
// 但对**平台级参数**（platform.api_host 这类：这台部署连哪个平台，是部署级事实）
// 就成了死路：平台运营方自己也是一个租户，admin 身份照样落进租户分支，
// 于是「改这台实例连哪个平台」这件事**没有任何人能做**。
//
// 与 UpdateValue 的分工：
//   - UpdateValue           ⇒ 写本租户的覆盖行（租户级参数）
//   - UpdatePlatformDefault ⇒ 写平台默认行（平台级参数，租户覆盖行对它无意义）
//
// 判据放在 service 层而不是 controller 层：controller 不知道自己写的是
// 哪一档，而「这一键是不是平台级」是 config_param_tenant_scope.go 里的事实。
// 守卫放在 service ⇒ 任何调用方都绕不过去。
//
// 只允许写 platformScopedParams 里的键：这条路径绕过租户分支，
// 若让它能写任意键，租户就能通过它改写全体租户的默认值 —— 那正是
// UpdateValue 刻意不让人做的事。
func (s *ConfigParamService) UpdatePlatformDefault(ctx context.Context, group, key, newValue string, actorID uint) error {
	if s.repo == nil {
		return repoNotInitializedErr
	}
	full := fullParamKey(group, key)
	if _, isPlatformScoped := platformScopedParams[full]; !isPlatformScoped {
		return fmt.Errorf("参数 %s 不是平台级参数：平台默认值写入只对平台级参数开放，"+
			"租户级参数请走普通参数更新（写本租户覆盖行）", full)
	}

	p, err := s.repo.GetByGroupKey(ctx, group, key)
	if err != nil {
		return err
	}
	if p.ReadOnly {
		return repository.ErrParamReadOnly
	}
	if p.Deprecated {
		return repository.ErrParamDeprecated
	}
	if err := validateValue(p.ValueType, newValue, p.Min, p.Max); err != nil {
		return err
	}
	newValue, err = normalizeValueForType(p.ValueType, newValue)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateValue(ctx, group, key, newValue, actorID); err != nil {
		return err
	}
	// 平台默认值变了，**所有租户**的该键缓存都要失效：租户覆盖行里可能留着
	// 旧值的拷贝，不一起清就会出现「平台默认改了但某租户还是旧值」。
	s.invalidate(group, key)
	s.invalidateGroup(group)
	return nil
}

// ResetToDefault 重置单条为默认值
func (s *ConfigParamService) ResetToDefault(ctx context.Context, group, key string, actorID uint) error {
	if s.repo == nil {
		return repoNotInitializedErr
	}
	if tenantID, scoped := tenantParamScope(ctx); scoped {
		if s.tenantRepo == nil {
			return repoNotInitializedErr
		}
		p, err := s.definitionForWrite(ctx, group, key)
		if err != nil {
			return err
		}
		oldValue, _ := s.getString(ctx, group, key)
		if _, err := s.tenantRepo.DeleteOverride(tenant.OnlyTenant(ctx, tenantID), tenantID, group, key, oldValue, p.DefaultValue, actorID); err != nil {
			return err
		}
		s.invalidateTenant(tenantID, group, key)
		return nil
	}
	if tenant.IsMultiTenant() {
		return repository.ErrTenantParamContextMissing
	}
	if err := s.repo.ResetToDefault(ctx, group, key, actorID); err != nil {
		return errConfigParamNotFound(group, key, err)
	}
	s.invalidate(group, key)
	return nil
}

// BulkResetGroup 整组重置默认值
func (s *ConfigParamService) BulkResetGroup(ctx context.Context, group string, actorID uint) error {
	if s.repo == nil {
		return repoNotInitializedErr
	}
	if tenantID, scoped := tenantParamScope(ctx); scoped {
		if s.tenantRepo == nil {
			return repoNotInitializedErr
		}
		if _, err := s.tenantRepo.BulkDeleteGroup(tenant.OnlyTenant(ctx, tenantID), tenantID, group, actorID); err != nil {
			return err
		}
		s.invalidateTenantGroup(tenantID, group)
		return nil
	}
	if tenant.IsMultiTenant() {
		return repository.ErrTenantParamContextMissing
	}
	if err := s.repo.BulkResetGroup(ctx, group, actorID); err != nil {
		return err
	}
	s.invalidateGroup(group)
	return nil
}

// AuditLogs 变更日志
func (s *ConfigParamService) AuditLogs(ctx context.Context, limit int) ([]model.ConfigParamAuditLog, error) {
	if s.repo == nil {
		return nil, repoNotInitializedErr
	}
	if tenantID, scoped := tenantParamScope(ctx); scoped {
		if s.tenantRepo == nil {
			return nil, repoNotInitializedErr
		}
		logs, err := s.tenantRepo.AuditLogs(tenant.OnlyTenant(ctx, tenantID), tenantID, limit)
		if err != nil {
			return nil, err
		}
		// 对外保持既有响应形状（管理端页面只认这一套字段）；租户归属由
		// 请求上下文与查询条件保证，不需要把 tenant_id 回传给前端。
		out := make([]model.ConfigParamAuditLog, len(logs))
		for i, l := range logs {
			out[i] = model.ConfigParamAuditLog{
				ID:         l.ID,
				ParamGroup: l.ParamGroup,
				ParamKey:   l.ParamKey,
				OldValue:   l.OldValue,
				NewValue:   l.NewValue,
				Action:     l.Action,
				ActorID:    l.ActorID,
				CreatedAt:  l.CreatedAt,
			}
		}
		return out, nil
	}
	if tenant.IsMultiTenant() {
		return nil, repository.ErrTenantParamContextMissing
	}
	return s.repo.AuditLogs(ctx, limit)
}

func (s *ConfigParamService) invalidate(group, key string) {
	s.mu.Lock()
	delete(s.cache, group+"."+key)
	s.loaded[group] = false
	s.mu.Unlock()
}

func (s *ConfigParamService) invalidateTenant(tenantID uint, group, key string) {
	prefix := tenantCachePrefix(tenantID)
	s.mu.Lock()
	delete(s.cache, prefix+group+"."+key)
	s.loaded[prefix+group] = false
	s.mu.Unlock()
}

// invalidateGroup 清掉整组在**所有域**（平台域 + 各租户域）的缓存。
//
// 为什么必须连租户域一起清：缓存键分两种形态 —— 平台域是 "group.key"，
// 租户域是 "t<租户ID>|group.key"（见 tenantCachePrefix）。只按 "group." 前缀
// 匹配的话，租户域的键一条都清不掉，于是平台默认值改了之后，**已经读过一次的
// 租户仍拿到旧值** —— 而这正是"平台默认值写入"这条通道要修的那类不一致
// （用户改完平台地址，部分租户仍连着旧平台）。
//
// 匹配方式：键里 "|" 之后才是 group/key，所以判据是「键的 group 部分等于目标组」
// —— 对平台域与租户域同时成立。
func (s *ConfigParamService) invalidateGroup(group string) {
	s.mu.Lock()
	for k := range s.cache {
		if paramCacheKeyGroup(k) == group {
			delete(s.cache, k)
		}
	}
	s.loaded[group] = false
	// 租户域的 loaded 标记带前缀，一并清掉
	for k := range s.loaded {
		if paramCacheKeyGroup(k) == group {
			delete(s.loaded, k)
		}
	}
	s.mu.Unlock()
}

// paramCacheKeyGroup 从缓存键里取出 group 部分。
//
// 键形态有两种："group.key"（平台域）与 "t<租户ID>|group.key"（租户域）。
// 租户域那截前缀长度不定，所以不能按固定前缀切，要找最后一个 "|"。
func paramCacheKeyGroup(cacheKey string) string {
	if i := strings.LastIndexByte(cacheKey, '|'); i >= 0 {
		cacheKey = cacheKey[i+1:]
	}
	if j := strings.IndexByte(cacheKey, '.'); j >= 0 {
		return cacheKey[:j]
	}
	return cacheKey
}

func (s *ConfigParamService) invalidateTenantGroup(tenantID uint, group string) {
	prefix := tenantCachePrefix(tenantID)
	keyPrefix := prefix + group + "."
	s.mu.Lock()
	for k := range s.cache {
		if strings.HasPrefix(k, keyPrefix) {
			delete(s.cache, k)
		}
	}
	s.loaded[prefix+group] = false
	s.mu.Unlock()
}

// validateValue 校验待写入的值是否符合该参数的类型与范围。
//
// 三条纪律（前两条都是被真实绕过口教出来的）：
//  1. 类型校验**无条件执行**，不因为没配 min/max 就跳过。
//     否则运维给一个 int 参数写进 "abc"，GetInt 读到解析失败、悄悄回退到编译期兜底值，
//     页面显示「保存成功」、行为却完全没变——这跟「登记了没人读」是同一种病。
//  2. 解析失败一律 return error，**没有任何一条分支允许 return nil 蒙混过关**。
//     （缺陷：duration 分支在 time.ParseDuration/ParseFloat 失败时直接 return nil，
//     等于把 min/max 校验整个关掉，写 "999999999s" 也能存进去。）
//  3. switch 对 value_type **穷举**，未知类型走 default 报错。
//     （缺陷：bool 与 string 两型根本不在 switch 里，任何值都能存进布尔开关；
//     今天登记表里有 int/float/duration/bool/json/string 六型，
//     TestEverySeedValueTypeHasAValidationBranch 把「集合与分支一一对应」钉成判据。）
//
// min/max 自身解析失败也报错而不是当 0 处理——库里存了个坏边界是数据问题，
// 静默当 0 会让所有值都「通过校验」，比报错难查得多。
func validateValue(valueType, value string, min, max *string) error {
	switch valueType {
	case "int":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("value %q not valid int: %w", value, err)
		}
		if min != nil {
			m, err := strconv.Atoi(*min)
			if err != nil {
				return fmt.Errorf("min %q of int param not valid: %w", *min, err)
			}
			if v < m {
				return fmt.Errorf("value %d < min %d", v, m)
			}
		}
		if max != nil {
			m, err := strconv.Atoi(*max)
			if err != nil {
				return fmt.Errorf("max %q of int param not valid: %w", *max, err)
			}
			if v > m {
				return fmt.Errorf("value %d > max %d", v, m)
			}
		}
	case "float":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("value %q not valid float: %w", value, err)
		}
		if min != nil {
			m, err := strconv.ParseFloat(*min, 64)
			if err != nil {
				return fmt.Errorf("min %q of float param not valid: %w", *min, err)
			}
			if v < m {
				return fmt.Errorf("value %f < min %f", v, m)
			}
		}
		if max != nil {
			m, err := strconv.ParseFloat(*max, 64)
			if err != nil {
				return fmt.Errorf("max %q of float param not valid: %w", *max, err)
			}
			if v > m {
				return fmt.Errorf("value %f > max %f", v, m)
			}
		}
	case "duration":
		// 口径与 GetDuration 严格一致（parseDurationSeconds），
		// 单位统一折成秒再比大小，否则「值写 30s、边界写 30」这类混写会算出荒谬结果。
		v, err := parseDurationSeconds(value)
		if err != nil {
			return err
		}
		if min != nil {
			m, err := parseDurationSeconds(*min)
			if err != nil {
				return fmt.Errorf("min %q of duration param not valid: %w", *min, err)
			}
			if v < m {
				return fmt.Errorf("value %fs < min %fs", v, m)
			}
		}
		if max != nil {
			m, err := parseDurationSeconds(*max)
			if err != nil {
				return fmt.Errorf("max %q of duration param not valid: %w", *max, err)
			}
			if v > m {
				return fmt.Errorf("value %fs > max %fs", v, m)
			}
		}
	case "bool":
		// 判据与读取侧严格同一条：GetBool 用的就是 strconv.ParseBool。
		// 少这里时 "banana" 能存进布尔开关，页面显示「保存成功」，
		// GetBool 解析失败却静默回落兜底值——开关看起来坏了，实际是库里那格脏了。
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("value %q not valid bool: %w", value, err)
		}
	case "json":
		// 三条整份 JSON 的旋钮（权限矩阵 / LLM 降级策略 / Embedding 全局覆盖）
		// 由消费方 json.Unmarshal，坏 JSON 会被静默忽略并沿用内置默认，
		// 所以语法必须在写侧就拦住，而不是靠前端那个 JSON 按钮。
		// 报错文案刻意不回显 value：那几份 blob 里带 api_key。
		if !json.Valid([]byte(value)) {
			return errors.New("value 不是一段合法的 JSON")
		}
	case "string":
		// 自由文本没有可校验的形态。空串是合法值：读取侧把 "" 当「显式置空」
		// 而不是「这行没配」（getString 命中即返回，不回落兜底）。
	default:
		// 未知类型不许走「什么都不校验」这条路。登记表新增一个 value_type 时
		// 忘了在这里加分支，后果是那一类参数全都能写任意值且静默生效失败。
		return fmt.Errorf("unknown value_type %q：新增类型必须同时在 validateValue 里加校验分支", valueType)
	}
	return nil
}
