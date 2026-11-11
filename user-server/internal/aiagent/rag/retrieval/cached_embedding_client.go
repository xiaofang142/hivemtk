package ragretrieval

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// CachedEmbeddingClient Embedding 服务的 Redis 缓存装饰器
type CachedEmbeddingClient struct {
	inner        llm.EmbeddingServiceInterface
	redis        RedisClient
	db           *gorm.DB
	ttlRedis     time.Duration
	ttlDB        time.Duration
	disableCache bool

	workerPool chan func()
	workerWg   sync.WaitGroup
	closeOnce  sync.Once
	closed     chan struct{}
}

const (
	defaultWorkerCount = 10
	defaultTaskQueue   = 20
)

type CachedEmbeddingClientConfig struct {
	TTLRedis     time.Duration
	TTLDB        time.Duration
	DisableCache bool
}

func DefaultCachedEmbeddingClientConfig() *CachedEmbeddingClientConfig {
	return &CachedEmbeddingClientConfig{
		TTLRedis:     7 * 24 * time.Hour,
		TTLDB:        30 * 24 * time.Hour,
		DisableCache: false,
	}
}

// NewCachedEmbeddingClient 构造装饰器。inner 必须非 nil；
// redis / db 可为 nil（禁用对应缓存层）。
func NewCachedEmbeddingClient(
	inner llm.EmbeddingServiceInterface,
	redis RedisClient,
	db *gorm.DB,
	cfg *CachedEmbeddingClientConfig,
) *CachedEmbeddingClient {
	if cfg == nil {
		cfg = DefaultCachedEmbeddingClientConfig()
	}
	c := &CachedEmbeddingClient{
		inner:        inner,
		redis:        redis,
		db:           db,
		ttlRedis:     cfg.TTLRedis,
		ttlDB:        cfg.TTLDB,
		disableCache: cfg.DisableCache,
		workerPool:   make(chan func(), defaultTaskQueue),
		closed:       make(chan struct{}),
	}
	c.startWorkers()
	return c
}

// Embed 批量 embedding（带缓存）：每个 text 独立查缓存（先 Redis 后 PG），
// 未命中的批量调 inner，再后台回填缓存。
func (c *CachedEmbeddingClient) Embed(ctx context.Context, cfg *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if cfg == nil {
		cfg = c.inner.DefaultConfig()
	}
	if c.disableCache || (c.redis == nil && c.db == nil) {
		return c.inner.Embed(ctx, cfg, texts)
	}

	model := cfg.Model
	expectDim := cfg.Dimension
	if expectDim <= 0 {
		expectDim = 1024
	}
	results := make([][]float32, len(texts))
	missIndices := make([]int, 0, len(texts))
	missTexts := make([]string, 0, len(texts))

	for i, text := range texts {
		hit := false
		if c.redis != nil {
			key := c.cacheKey(model, text)
			if cached, err := c.redis.Get(ctx, key); err == nil && cached != "" {
				if vec, err := decodeVec(cached); err == nil && len(vec) == expectDim {
					results[i] = vec
					hit = true
				}
			}
		}
		if !hit {
			missIndices = append(missIndices, i)
			missTexts = append(missTexts, text)
		}
	}

	if len(missIndices) > 0 && c.db != nil {
		newMissIndices := make([]int, 0, len(missIndices))
		newMissTexts := make([]string, 0, len(missTexts))
		for i, idx := range missIndices {
			text := missTexts[i]
			vec, hit := c.queryDBCache(ctx, model, text, expectDim)
			if hit {
				results[idx] = vec
				if c.redis != nil {
					_ = c.redis.Set(ctx, c.cacheKey(model, text), encodeVec(vec), c.ttlRedis)
				}
			} else {
				newMissIndices = append(newMissIndices, idx)
				newMissTexts = append(newMissTexts, text)
			}
		}
		missIndices = newMissIndices
		missTexts = newMissTexts
	}

	if len(missIndices) > 0 {
		vectors, err := c.inner.Embed(ctx, cfg, missTexts)
		if err != nil {
			return nil, err
		}
		for i, idx := range missIndices {
			if i >= len(vectors) {
				break
			}
			results[idx] = vectors[i]
			text, vec := missTexts[i], vectors[i]
			// 后台写入脱离请求生命周期但必须保留租户：Redis L1 键按租户分域，
			// PG L2 的 embedding_cache 是租户表（on 模式无租户会被 callback 拒绝）。
			ctxBg := tenant.BackgroundWithTenant(ctx)
			c.submit(func() {
				if c.redis != nil {
					_ = c.redis.Set(ctxBg, c.cacheKey(model, text), encodeVec(vec), c.ttlRedis)
				}
				if c.db != nil {
					c.persistDBCache(ctxBg, model, text, vec)
				}
			})
		}
	}
	return results, nil
}

func (c *CachedEmbeddingClient) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	vectors, err := c.Embed(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("embedding 返回空")
	}
	return vectors[0], nil
}

func (c *CachedEmbeddingClient) DefaultConfig() *llm.EmbeddingConfig {
	return c.inner.DefaultConfig()
}

// Close 关闭 worker pool，等待所有已提交的任务完成
func (c *CachedEmbeddingClient) Close() {
	c.closeOnce.Do(func() {
		close(c.workerPool)
	})
	c.workerWg.Wait()
}

func (c *CachedEmbeddingClient) submit(task func()) {
	select {
	case <-c.closed:
		logger.Warnf("[cached_embedding] submit after close, task dropped")
		return
	case c.workerPool <- task:

	default:
		task()
	}
}

func (c *CachedEmbeddingClient) startWorkers() {
	for i := 0; i < defaultWorkerCount; i++ {
		c.workerWg.Add(1)
		// 长驻缓存 worker（进程生命周期消费 task 队列）：utils.SafeGo 收口 panic
		utils.SafeGo(context.Background(), "rag.embeddingCache.worker", func(ctx context.Context) {
			defer c.workerWg.Done()
			for task := range c.workerPool {
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("[cached_embedding] worker task panic recovered: %v", r)
						}
					}()
					task()
				}()
			}
		})
	}
}

func (c *CachedEmbeddingClient) cacheKey(model, text string) string {
	return fmt.Sprintf("rag:emb:%s:%s", model, sha256Hex(normalizeQuery(text)))
}

// embeddingCacheTableExists 检查 embedding_cache 表是否存在（缺表时缓存层静默降级）。
func (c *CachedEmbeddingClient) embeddingCacheTableExists(ctx context.Context) bool {
	if c.db == nil {
		return false
	}
	var exists bool
	if err := c.db.WithContext(ctx).Table("information_schema.tables").
		Select("count(*) > 0").
		Where("table_schema = current_schema() AND table_name = 'embedding_cache'").
		Scan(&exists).Error; err != nil {
		return false
	}
	return exists
}

func (c *CachedEmbeddingClient) queryDBCache(ctx context.Context, model, text string, expectDim int) ([]float32, bool) {
	if !c.embeddingCacheTableExists(ctx) {
		return nil, false
	}
	hash := sha256Hex(normalizeQuery(text))
	var vecStr string
	err := c.db.WithContext(ctx).Table("embedding_cache").
		Select("embedding::text").
		Where("text_hash = ? AND model = ? AND (expires_at IS NULL OR expires_at > NOW())", hash, model).
		Scan(&vecStr).Error
	if err != nil || vecStr == "" {
		return nil, false
	}
	vec, err := parsePGVector(vecStr)
	if err != nil || len(vec) != expectDim {
		return nil, false
	}
	hitCtx := tenant.BackgroundWithTenant(ctx)
	c.submit(func() {
		_ = c.db.WithContext(hitCtx).Table("embedding_cache").
			Where("text_hash = ? AND model = ?", hash, model).
			Updates(map[string]any{
				"hit_count":    gorm.Expr("hit_count + 1"),
				"last_used_at": time.Now(),
			}).Error
	})
	return vec, true
}

func (c *CachedEmbeddingClient) persistDBCache(ctx context.Context, model, text string, vec []float32) {
	if !c.embeddingCacheTableExists(ctx) {
		return
	}
	hash := sha256Hex(normalizeQuery(text))
	vecLiteral := vecToPGString(vec)
	now := time.Now()
	expiresAt := now.Add(c.ttlDB)
	err := c.db.WithContext(ctx).Table("embedding_cache").
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "text_hash"}, {Name: "model"}},
			DoUpdates: clause.Assignments(map[string]any{
				"embedding":    gorm.Expr("EXCLUDED.embedding"),
				"dimension":    len(vec),
				"last_used_at": now,
				"expires_at":   expiresAt,
				"updated_at":   now,
			}),
		}).
		Create(map[string]any{
			"text_hash":    hash,
			"text_content": text,
			"model":        model,
			"dimension":    len(vec),
			"embedding":    gorm.Expr("?::vector", vecLiteral),
			"hit_count":    0,
			"last_used_at": now,
			"expires_at":   expiresAt,
			"created_at":   now,
			"updated_at":   now,
		}).Error
	if err != nil {
		logger.Errorf("cached_embedding: persistDBCache failed, hash=%s: %v", hash, err)
	}
}

var _ llm.EmbeddingServiceInterface = (*CachedEmbeddingClient)(nil)
