package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	llmpkg "hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

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
//   - 缓存 key = "group.key"
//   - 读操作在缓存 miss 时拉一次 group 全量（同组一次性加载，减少 DB 往返）
type ConfigParamService struct {
	repo *repository.ConfigParamRepository

	mu     sync.RWMutex
	cache  map[string]paramEntry
	loaded map[string]bool
	nowFn  func() time.Time
}

var (
	globalConfigParamMu sync.RWMutex
	globalConfigParam   *ConfigParamService
)

// NewConfigParamService 构造（main 启动时调用）
func NewConfigParamService(db *gorm.DB) *ConfigParamService {
	return &ConfigParamService{
		repo:   repository.NewConfigParamRepository(db),
		cache:  make(map[string]paramEntry, 256),
		loaded: make(map[string]bool, 32),
		nowFn:  time.Now,
	}
}

// SetGlobal 装配层注入单例（各 module 通过 GlobalConfigParam() 读取）。
//
// 语义：**后写覆盖**，不是"只生效一次"。
//
// 历史缺陷（见 TASKS_AUDIT_2026-09-16.md · TEST-05）：本函数原用 sync.Once，
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
// 在线判定散在四个读侧（管理面列表、渠道总览计数、主动触达选号、巡检脚本），各自写死一份秒数
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
func SeedConfigParams(ctx context.Context, db *gorm.DB) error {
	// 建表已收敛到启动期 AutoMigrate（db.RegisterExtraModels），此处仅 seed
	if err := db.WithContext(ctx).First(&model.ConfigParam{}).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("config_params 表不可用: %w", err)
	}
	repo := repository.NewConfigParamRepository(db)
	svc := NewConfigParamService(db)
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
	for _, def := range DefaultParamDefs() {
		p, err := repo.GetByGroupKey(ctx, def.Group, def.Key)
		if err == nil && p != nil {
			existing++
			if syncParamDef(ctx, db, p, def) {
				refreshed++
			}
			continue
		}

		if err := db.WithContext(ctx).Create(&model.ConfigParam{
			Group:        def.Group,
			Key:          def.Key,
			Name:         def.Name,
			Description:  def.Description,
			ValueType:    def.ValueType,
			Value:        def.DefaultValue,
			DefaultValue: def.DefaultValue,
			Min:          def.Min,
			Max:          def.Max,
			Step:         def.Step,
			ReadOnly:     def.ReadOnly,
			Restart:      def.Restart,
			Category:     def.Category,
		}).Error; err != nil {
			logger.Warnf("[ConfigParam] seed create %s.%s failed: %v", def.Group, def.Key, err)
			continue
		}
		created++
	}
	logger.Infof("[ConfigParam] seed done: created=%d existing=%d refreshed_defs=%d total_defs=%d",
		created, existing, refreshed, len(DefaultParamDefs()))
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
func syncParamDef(ctx context.Context, db *gorm.DB, p *model.ConfigParam, def ParamDef) bool {
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
	if len(updates) == 0 {
		return false
	}
	// 按主键定位，不再用 (group,key) 条件：这两列本身也可能在本次更新里。
	if err := db.WithContext(ctx).Model(&model.ConfigParam{}).
		Where("id = ?", p.ID).Updates(updates).Error; err != nil {
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

func (s *ConfigParamService) getString(ctx context.Context, group, key string) (string, bool) {
	cacheKey := group + "." + key

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
		s.loaded[group] = false
		s.mu.Unlock()
	}

	if s.repo == nil {
		return "", false
	}

	s.mu.Lock()
	if s.loaded[group] {

		e, ok := s.cache[cacheKey]
		s.mu.Unlock()
		if ok {
			return e.value, true
		}
		return "", false
	}

	s.loaded[group] = true
	s.mu.Unlock()

	params, err := s.repo.ListByGroup(ctx, group)
	if err != nil {

		logger.Warnf("[ConfigParam] load group %s failed: %v", group, err)
		s.mu.Lock()
		s.loaded[group] = false
		s.mu.Unlock()
		return "", false
	}

	s.mu.Lock()
	filledAt := s.now().Add(configParamTTL)
	for _, p := range params {
		s.cache[p.Group+"."+p.Key] = paramEntry{value: p.Value, expiresAt: filledAt}
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
func (s *ConfigParamService) List(ctx context.Context) ([]model.ConfigParam, error) {
	return s.repo.List(ctx)
}

// ListByGroup 按分组返回参数
func (s *ConfigParamService) ListByGroup(ctx context.Context, group string) ([]model.ConfigParam, error) {
	return s.repo.ListByGroup(ctx, group)
}

// UpdateValue 更新单个参数值（会做范围校验 + 失效缓存）
func (s *ConfigParamService) UpdateValue(ctx context.Context, group, key, newValue string, actorID uint) error {
	if s.repo == nil {
		return fmt.Errorf("config_param repo not initialized")
	}

	p, err := s.repo.GetByGroupKey(ctx, group, key)
	if err != nil {
		return err
	}
	if err := validateValue(p.ValueType, newValue, p.Min, p.Max); err != nil {
		return err
	}
	if err := s.repo.UpdateValue(ctx, group, key, newValue, actorID); err != nil {
		return err
	}

	s.invalidate(group, key)
	return nil
}

// ResetToDefault 重置单条为默认值
func (s *ConfigParamService) ResetToDefault(ctx context.Context, group, key string, actorID uint) error {
	if err := s.repo.ResetToDefault(ctx, group, key, actorID); err != nil {
		return err
	}
	s.invalidate(group, key)
	return nil
}

// BulkResetGroup 整组重置默认值
func (s *ConfigParamService) BulkResetGroup(ctx context.Context, group string, actorID uint) error {
	if err := s.repo.BulkResetGroup(ctx, group, actorID); err != nil {
		return err
	}
	s.invalidateGroup(group)
	return nil
}

// AuditLogs 变更日志
func (s *ConfigParamService) AuditLogs(ctx context.Context, limit int) ([]model.ConfigParamAuditLog, error) {
	return s.repo.AuditLogs(ctx, limit)
}

func (s *ConfigParamService) invalidate(group, key string) {
	s.mu.Lock()
	delete(s.cache, group+"."+key)
	s.loaded[group] = false
	s.mu.Unlock()
}

func (s *ConfigParamService) invalidateGroup(group string) {
	s.mu.Lock()
	for k := range s.cache {
		if len(k) >= len(group) && k[:len(group)] == group {
			delete(s.cache, k)
		}
	}
	s.loaded[group] = false
	s.mu.Unlock()
}

// validateValue 校验待写入的值是否符合该参数的类型与范围。
//
// 两条纪律（都是被真实绕过口教出来的）：
//  1. 类型校验**无条件执行**，不因为没配 min/max 就跳过。
//     否则运维给一个 int 参数写进 "abc"，GetInt 读到解析失败、悄悄回退到编译期兜底值，
//     页面显示「保存成功」、行为却完全没变——这跟「登记了没人读」是同一种病。
//  2. 解析失败一律 return error，**没有任何一条分支允许 return nil 蒙混过关**。
//     （历史缺陷：duration 分支在 time.ParseDuration/ParseFloat 失败时直接 return nil，
//     等于把 min/max 校验整个关掉，写 "999999999s" 也能存进去。）
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
	}
	return nil
}
