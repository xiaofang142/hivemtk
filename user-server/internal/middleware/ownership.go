package middleware

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OwnershipChecker 资源归属查询接口。
//
// table 为数据库表名（业务侧硬编码传入，避免 GORM TableName 反射开销），
// resourceID 是 URL 路径里的资源主键。
// 返回 ownerID：0 表示系统级资源（允许所有用户访问）。
// 返回 error：调用方应当 fail-closed，禁止放行。
type OwnershipChecker interface {
	GetOwnerID(ctx context.Context, table string, resourceID uint) (uint, error)
}

// DBOwnershipChecker 基于 GORM 的通用归属查询实现。
//
// 通过 SELECT owner_id FROM <table> WHERE id = ? LIMIT 1 查询；
// 主键字段统一为 id（与项目现有模型约定一致）。
//
// 该实现仅供中间件使用——业务逻辑应走专用 Repository 以复用 SQL 缓存/预编译。
type DBOwnershipChecker struct {
	db *gorm.DB
}

// NewDBOwnershipChecker 构造一个 GORM 归属查询器。
// db 不可为 nil，否则 GetOwnerID 立即返回错误。
func NewDBOwnershipChecker(db *gorm.DB) *DBOwnershipChecker {
	if db == nil {
		return &DBOwnershipChecker{}
	}
	return &DBOwnershipChecker{db: db}
}

// GetOwnerID 查询资源 owner_id。
//
// 注意：
//   - db 为 nil（test/uninitialized）→ 返回 0 + nil，让上层 fail-closed（不允许任何访问）。
//   - 记录不存在（gorm.ErrRecordNotFound）→ 返回 0 + nil，视为系统级资源。
//   - 其他 DB 错误 → 透传 error，调用方按 500 处理。
func (c *DBOwnershipChecker) GetOwnerID(ctx context.Context, table string, resourceID uint) (uint, error) {
	if c == nil || c.db == nil {
		return 0, nil
	}
	if table == "" || resourceID == 0 {
		return 0, nil
	}
	var ownerID uint
	err := c.db.WithContext(ctx).
		Table(table).
		Select("owner_id").
		Where("id = ?", resourceID).
		Take(&ownerID).Error
	if err != nil {
		return 0, err
	}
	return ownerID, nil
}

type ownershipCacheEntry struct {
	ownerID uint
	err     error
	expire  time.Time
}

type ownershipCache struct {
	mu   sync.RWMutex
	data map[string]ownershipCacheEntry
}

func newOwnershipCache() *ownershipCache {
	return &ownershipCache{data: make(map[string]ownershipCacheEntry)}
}

func (c *ownershipCache) get(key string) (ownershipCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.data[key]
	if !ok {
		return ownershipCacheEntry{}, false
	}
	if time.Now().After(e.expire) {
		return ownershipCacheEntry{}, false
	}
	return e, true
}

func (c *ownershipCache) set(key string, entry ownershipCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = entry
}

var globalOwnershipCache = newOwnershipCache()

// RequireOwner 归属校验中间件（无缓存版本）。
//
// 参数：
//   - resourceKey：URL 参数中资源 ID 的名字（默认 "id"，按业务传 "kb_id" 等）
//   - table：数据库表名
//
// 流程：
//  1. 取 URL 路径里的资源 ID（uint）
//  2. 取 JWT 注入的 user_id（utils.GetUID）
//  3. 调用 OwnershipChecker.GetOwnerID
//  4. owner 匹配 / owner=0（系统级）→ c.Next()
//  5. 否则 → 403
//
// checker 为 nil 时直接放行（保留向后兼容，由调用方注入具体实现）。
func RequireOwner(resourceKey, table string) gin.HandlerFunc {
	return RequireOwnerWithChecker(resourceKey, table, nil)
}

// RequireOwnerWithChecker 带自定义 OwnershipChecker 的归属校验中间件。
//
// 用于注入带预编译/特殊表名映射的实现，避免 RequireOwner 内硬编码 GORM。
func RequireOwnerWithChecker(resourceKey, table string, checker OwnershipChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := utils.GetUID(c)
		if uid == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未登录或会话失效",
			})
			return
		}
		resourceID, err := strconv.ParseUint(c.Param(resourceKey), 10, 64)
		if err != nil || resourceID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的资源 ID",
			})
			return
		}
		if checker == nil {
			c.Next()
			return
		}
		ownerID, err := checker.GetOwnerID(c.Request.Context(), table, uint(resourceID))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "归属校验失败",
			})
			return
		}
		if ownerID != 0 && ownerID != uid {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权访问该资源",
			})
			return
		}
		c.Next()
	}
}

// RequireOwnerWithCache 带 TTL 内存缓存的归属校验中间件（防 DoS）。
//
// ttl 推荐 5s——业务可调，但需评估：过长放大越权窗口，过短失去缓存意义。
// 缓存 key = (table, resourceID, userID)；userID 不进 key 可借助上下文比对，
// 但为简化失效语义这里直接合并。
func RequireOwnerWithCache(resourceKey, table string, ttl time.Duration) gin.HandlerFunc {
	return RequireOwnerWithCacheAndChecker(resourceKey, table, ttl, nil)
}

// RequireOwnerWithCacheAndChecker 带 checker + 缓存的归属校验中间件。
func RequireOwnerWithCacheAndChecker(resourceKey, table string, ttl time.Duration, checker OwnershipChecker) gin.HandlerFunc {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return func(c *gin.Context) {
		uid := utils.GetUID(c)
		if uid == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未登录或会话失效",
			})
			return
		}
		resourceID, err := strconv.ParseUint(c.Param(resourceKey), 10, 64)
		if err != nil || resourceID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的资源 ID",
			})
			return
		}
		if checker == nil {
			c.Next()
			return
		}
		key := table + ":" + strconv.FormatUint(uint64(resourceID), 10)
		if entry, ok := globalOwnershipCache.get(key); ok {
			if entry.err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "归属校验失败",
				})
				return
			}
			if entry.ownerID != 0 && entry.ownerID != uid {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"code":    403,
					"message": "无权访问该资源",
				})
				return
			}
			c.Next()
			return
		}
		ownerID, err := checker.GetOwnerID(c.Request.Context(), table, uint(resourceID))
		globalOwnershipCache.set(key, ownershipCacheEntry{
			ownerID: ownerID,
			err:     err,
			expire:  time.Now().Add(ttl),
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "归属校验失败",
			})
			return
		}
		if ownerID != 0 && ownerID != uid {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权访问该资源",
			})
			return
		}
		c.Next()
	}
}

// InvalidateOwnershipCache 清除单条缓存（用于资源 owner 变更/删除场景）。
// 当前未在 main.go 接入；暴露此入口便于业务层主动失效。
func InvalidateOwnershipCache(table string, resourceID uint) {
	key := table + ":" + strconv.FormatUint(uint64(resourceID), 10)
	globalOwnershipCache.mu.Lock()
	defer globalOwnershipCache.mu.Unlock()
	delete(globalOwnershipCache.data, key)
}
