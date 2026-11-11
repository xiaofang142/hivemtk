package ragretrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// QueryRewriteStrategy 改写策略标签
type QueryRewriteStrategy string

const (
	StrategyNone       QueryRewriteStrategy = "none"
	StrategyHyDE       QueryRewriteStrategy = "hyde"
	StrategyMultiQuery QueryRewriteStrategy = "multiquery"
	StrategyHyDEMulti  QueryRewriteStrategy = "hyde_multiquery"
)

// RewrittenQuery 改写结果
type RewrittenQuery struct {
	Original     string               `json:"original"`
	Rewritten    string               `json:"rewritten"`
	MultiQueries []string             `json:"multi_queries"`
	UsedStrategy QueryRewriteStrategy `json:"used_strategy"`
	CacheHit     bool                 `json:"cache_hit"`
}

// QueryRewriter 查询改写器
type QueryRewriter struct {
	hydeGen     *HyDEGenerator
	multiGen    *MultiQueryGenerator
	redisClient RedisClient
	db          *gorm.DB
	redisTTL    time.Duration
}

type QueryRewriterConfig struct {
	RedisTTL time.Duration
}

func DefaultQueryRewriterConfig() *QueryRewriterConfig {
	return &QueryRewriterConfig{
		RedisTTL: 24 * time.Hour,
	}
}

// NewQueryRewriter 创建查询改写器。
// db / redisClient 可为 nil（跳过对应缓存层）；hydeGen / multiGen 为 nil 时视为禁用该策略。
func NewQueryRewriter(
	hydeGen *HyDEGenerator,
	multiGen *MultiQueryGenerator,
	redisClient RedisClient,
	db *gorm.DB,
	cfg *QueryRewriterConfig,
) *QueryRewriter {
	if cfg == nil {
		cfg = DefaultQueryRewriterConfig()
	}
	return &QueryRewriter{
		hydeGen:     hydeGen,
		multiGen:    multiGen,
		redisClient: redisClient,
		db:          db,
		redisTTL:    cfg.RedisTTL,
	}
}

// Rewrite 执行查询改写：空 query 直接返回（UsedStrategy=none）；
// 依次查 Redis（key: rag:rewrite:{sha256(normalized_query)}）与 DB
// query_rewrite_cache 表，未命中则并行调用 HyDE + Multi-Query 生成，
// 写回 Redis（TTL 24h）+ DB（异步）。缓存失败不阻断；HyDE / Multi-Query
// 独立决策，二者均失败时 UsedStrategy=none、Rewritten=原 query。
func (q *QueryRewriter) Rewrite(ctx context.Context, query string) (*RewrittenQuery, error) {
	if query == "" {
		return &RewrittenQuery{
			Original:     query,
			Rewritten:    query,
			UsedStrategy: StrategyNone,
		}, nil
	}

	hash := sha256Hex(normalizeQuery(query))
	redisKey := fmt.Sprintf("rag:rewrite:%s", hash)

	if q.redisClient != nil {
		if cached, err := q.redisClient.Get(ctx, redisKey); err == nil && cached != "" {
			var rw RewrittenQuery
			if err := json.Unmarshal([]byte(cached), &rw); err == nil {
				rw.CacheHit = true
				rw.Original = query
				go q.updateCacheStats(tenant.BackgroundWithTenant(ctx), hash)
				return &rw, nil
			}
		}
	}

	if q.db != nil {
		if dbCached, err := q.queryDB(ctx, hash); err == nil && dbCached != nil {
			if q.redisClient != nil {
				if data, err := json.Marshal(dbCached); err == nil {
					_ = q.redisClient.Set(ctx, redisKey, string(data), q.redisTTL)
				}
			}
			dbCached.CacheHit = true
			dbCached.Original = query
			return dbCached, nil
		}
	}

	var (
		hydeDoc      string
		multiQueries []string
		hydeErr      error
		multiErr     error
		strategy     QueryRewriteStrategy = StrategyNone
	)
	var wg sync.WaitGroup
	if q.hydeGen != nil && q.hydeGen.IsEnabled() {
		wg.Add(1)
		// utils.SafeGo 收口：HyDE 生成（LLM）panic 不击穿进程（失败路径同 hydeErr 降级语义）
		utils.SafeGo(ctx, "rag.rewrite.hyde", func(ctx context.Context) {
			defer wg.Done()
			hydeDoc, hydeErr = q.hydeGen.Generate(ctx, query)
		})
	}
	if q.multiGen != nil && q.multiGen.IsEnabled() {
		wg.Add(1)
		// utils.SafeGo 收口：多查询改写（LLM）panic 不击穿进程（失败路径同 multiErr 降级语义）
		utils.SafeGo(ctx, "rag.rewrite.multiQuery", func(ctx context.Context) {
			defer wg.Done()
			multiQueries, multiErr = q.multiGen.Generate(ctx, query)
		})
	}
	wg.Wait()

	rw := &RewrittenQuery{Original: query}
	if hydeErr == nil && hydeDoc != "" {
		rw.Rewritten = hydeDoc
		strategy = StrategyHyDE
	} else {
		rw.Rewritten = query
	}
	if multiErr == nil && len(multiQueries) > 0 {
		rw.MultiQueries = multiQueries
		if strategy == StrategyHyDE {
			strategy = StrategyHyDEMulti
		} else {
			strategy = StrategyMultiQuery
		}
	}
	rw.UsedStrategy = strategy

	if q.redisClient != nil {
		if data, err := json.Marshal(rw); err == nil {
			_ = q.redisClient.Set(ctx, redisKey, string(data), q.redisTTL)
		}
	}
	if q.db != nil {
		go q.persistToDB(tenant.BackgroundWithTenant(ctx), hash, query, rw)
	}

	return rw, nil
}

// rewriteCacheTableExists 检查 query_rewrite_cache 表是否存在（缺表时静默跳过缓存层）。
func (q *QueryRewriter) rewriteCacheTableExists(ctx context.Context) bool {
	if q.db == nil {
		return false
	}
	var exists bool
	if err := q.db.WithContext(ctx).Table("information_schema.tables").
		Select("count(*) > 0").
		Where("table_schema = current_schema() AND table_name = 'query_rewrite_cache'").
		Scan(&exists).Error; err != nil {
		return false
	}
	return exists
}

func (q *QueryRewriter) queryDB(ctx context.Context, hash string) (*RewrittenQuery, error) {

	if !q.rewriteCacheTableExists(ctx) {
		return nil, nil
	}

	var row struct {
		HyDEDoc      string `gorm:"column:hyde_doc"`
		MultiQueries string `gorm:"column:multi_queries"`
		RewriteType  string `gorm:"column:rewrite_type"`
	}
	err := q.db.WithContext(ctx).Table("query_rewrite_cache").
		Select("hyde_doc, multi_queries, rewrite_type").
		Where("query_hash = ? AND (expires_at IS NULL OR expires_at > NOW())", hash).
		Scan(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	if row.HyDEDoc == "" && row.MultiQueries == "" {
		return nil, nil
	}
	rw := &RewrittenQuery{
		Rewritten:    row.HyDEDoc,
		UsedStrategy: QueryRewriteStrategy(row.RewriteType),
	}
	if row.MultiQueries != "" {
		if err := json.Unmarshal([]byte(row.MultiQueries), &rw.MultiQueries); err == nil && rw.Rewritten == "" && len(rw.MultiQueries) > 0 {
			rw.Rewritten = rw.MultiQueries[0]
		}
	}
	return rw, nil
}

func (q *QueryRewriter) persistToDB(ctx context.Context, hash, query string, rw *RewrittenQuery) {
	if q.db == nil {
		return
	}

	if !q.rewriteCacheTableExists(ctx) {
		return
	}
	multiJSON, _ := json.Marshal(rw.MultiQueries)
	now := time.Now()
	expiry := now.AddDate(0, 0, 30)
	err := q.db.WithContext(ctx).Table("query_rewrite_cache").
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "query_hash"}},
			DoUpdates: clause.Assignments(map[string]any{
				"hyde_doc":      rw.Rewritten,
				"multi_queries": string(multiJSON),
				"rewrite_type":  string(rw.UsedStrategy),
				"updated_at":    now,
			}),
		}).
		Create(map[string]any{
			"query_hash":     hash,
			"original_query": query,
			"hyde_doc":       rw.Rewritten,
			"multi_queries":  string(multiJSON),
			"rewrite_model":  "local",
			"rewrite_type":   string(rw.UsedStrategy),
			"hit_count":      0,
			"expires_at":     expiry,
			"created_at":     now,
			"updated_at":     now,
		}).Error
	if err != nil {
		logger.Errorf("query_rewriter: persist rewrite cache failed, hash=%s: %v", hash, err)
	}
}

func (q *QueryRewriter) updateCacheStats(ctx context.Context, hash string) {
	if q.db == nil {
		return
	}
	if !q.rewriteCacheTableExists(ctx) {
		return
	}
	_ = q.db.WithContext(ctx).Table("query_rewrite_cache").
		Where("query_hash = ?", hash).
		Updates(map[string]any{
			"hit_count":    gorm.Expr("hit_count + 1"),
			"last_used_at": time.Now(),
		}).Error
}
