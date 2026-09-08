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
	db   *gorm.DB

	mu     sync.RWMutex
	cache  map[string]paramEntry
	loaded map[string]bool
	nowFn  func() time.Time
}

var globalConfigParam *ConfigParamService
var globalOnce sync.Once

// NewConfigParamService 构造（main 启动时调用）
func NewConfigParamService(db *gorm.DB) *ConfigParamService {
	return &ConfigParamService{
		repo:   repository.NewConfigParamRepository(db),
		db:     db,
		cache:  make(map[string]paramEntry, 256),
		loaded: make(map[string]bool, 32),
		nowFn:  time.Now,
	}
}

// SetGlobal 装配层注入单例（各 module 通过 GlobalConfigParam() 读取）
func SetGlobal(svc *ConfigParamService) {
	globalOnce.Do(func() {
		globalConfigParam = svc
	})
}

// SetGlobalForTest 测试专用：强制替换全局实例（绕过 globalOnce）。
// 生产代码禁止调用——生产一律走 SetGlobal/NewConfigParamService+Seed。
func SetGlobalForTest(svc *ConfigParamService) {
	globalConfigParam = svc
}

// GlobalConfigParam 获取全局单例（nil-safe：返回一个无 DB 的 fallback stub，
// 所有 Get* 方法走 fallback 默认值而非 panic）
func GlobalConfigParam() *ConfigParamService {
	if globalConfigParam != nil {
		return globalConfigParam
	}

	return &ConfigParamService{
		cache:  make(map[string]paramEntry),
		loaded: make(map[string]bool),
	}
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

	var created, existing int
	for _, def := range DefaultParamDefs() {
		p, err := repo.GetByGroupKey(ctx, def.Group, def.Key)
		if err == nil && p != nil {
			existing++
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
	logger.Infof("[ConfigParam] seed done: created=%d existing=%d total_defs=%d",
		created, existing, len(DefaultParamDefs()))
	return nil
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

	if d, err := time.ParseDuration(v); err == nil {
		return d
	}

	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return time.Duration(f * float64(time.Second))
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

	if s.db == nil {
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

func validateValue(valueType, value string, min, max *string) error {
	if min == nil && max == nil {
		return nil
	}
	switch valueType {
	case "int":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("value %q not valid int: %w", value, err)
		}
		if min != nil {
			m, _ := strconv.Atoi(*min)
			if v < m {
				return fmt.Errorf("value %d < min %d", v, m)
			}
		}
		if max != nil {
			m, _ := strconv.Atoi(*max)
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
			m, _ := strconv.ParseFloat(*min, 64)
			if v < m {
				return fmt.Errorf("value %f < min %f", v, m)
			}
		}
		if max != nil {
			m, _ := strconv.ParseFloat(*max, 64)
			if v > m {
				return fmt.Errorf("value %f > max %f", v, m)
			}
		}
	case "duration":

		v, err := strconv.ParseFloat(value, 64)
		if err != nil {

			return nil
		}
		if min != nil {
			m, _ := strconv.ParseFloat(*min, 64)
			if v < m {
				return fmt.Errorf("value %f < min %f", v, m)
			}
		}
		if max != nil {
			m, _ := strconv.ParseFloat(*max, 64)
			if v > m {
				return fmt.Errorf("value %f > max %f", v, m)
			}
		}
	}
	return nil
}
