// seed_helpers.go 公共辅助函数与种子数据标识常量
//
// 设计要点：
// 所有种子数据通过 SourceOfSeed="seed_demo" 标记，便于精确清理
// 不修改表结构，仅在业务字段中嵌入 demo 标记（如 Username 前缀、Description 后缀等）
// 时间辅助：daysAgo(n) 生成过去 n 天的时间，便于时间分布
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
)

// seedTag 嵌入种子数据的标记，便于精确清理（不影响业务字段语义）
const seedTag = "[seed-demo]"

// seededRand 使用固定种子的随机数生成器，确保每次运行结果可复现
var seededRand = rand.New(rand.NewSource(42))

// nowRef 当前时间参考点（运行时取 time.Now()，便于单元测试时注入）
var nowRef = time.Now()

// daysAgo 返回 n 天前的时间
func daysAgo(n int) time.Time {
	return nowRef.AddDate(0, 0, -n)
}

// hoursAgo 返回 n 小时前的时间
func hoursAgo(n int) time.Time {
	return nowRef.Add(-time.Duration(n) * time.Hour)
}



// randInt 在 [min, max] 范围内生成随机整数
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return seededRand.Intn(max-min+1) + min
}

// randFloat 在 [min, max] 范围内生成随机浮点数
func randFloat(min, max float64) float64 {
	if max <= min {
		return min
	}
	return min + seededRand.Float64()*(max-min)
}

// zeroVectorString 生成 pgvector 兼容的 N 维零向量字符串（"[0,0,...]"）
// 用于 ChampionDialogue.embedding (NOT NULL vector(1024)) 等 NOT NULL 字段的占位写入。
// 真实业务向量由 internal/aiagent/rag/embedding 服务在运行时异步生成。
func zeroVectorString(n int) string {
	if n <= 0 {
		return "[]"
	}
	buf := make([]byte, 0, n*3)
	buf = append(buf, '[')
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '0')
	}
	buf = append(buf, ']')
	return string(buf)
}

// randPick 从切片中随机选择一个元素
func randPick[T any](items []T) T {
	var zero T
	if len(items) == 0 {
		return zero
	}
	return items[seededRand.Intn(len(items))]
}



// toJSONString 将任意值序列化为 JSON 字符串（失败时返回 "{}"）
func toJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// toJSONArrayString 将字符串切片序列化为 JSON 数组字符串
func toJSONArrayString(items []string) string {
	if items == nil {
		items = []string{}
	}
	return toJSONString(items)
}



// batchInsert 批量插入（每批 100 条），减少单次 SQL 大小
//
// pgvector 兼容：seed 阶段统一 Omit `embedding` 字段（种子数据不调用真实 embedding 服务，
// 避免在导入种子时强行拉起本地 TEI/GPU）。可空 embedding 列由 PG 写入 NULL；
// NOT NULL embedding 列（如 champion_dialogues.embedding NOT NULL vector(1024)）的批量写入
// 需走 batchInsertWithEmbedding，调用方需保证 items[i].Embedding 已是非空字符串。
// 业务数据由 `internal/aiagent/rag/embedding/` 服务在运行时异步生成。
func batchInsert[T any](database *gorm.DB, items []T, batchSize int) error {
	if len(items) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		if err := database.Omit("embedding").Create(&batch).Error; err != nil {
			return fmt.Errorf("batch insert [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// batchInsertWithEmbedding 强制写入 embedding 字段（用于 NOT NULL vector 列）；
// 调用方需保证 items[i].Embedding 已是非空字符串（如 zeroVectorString(1024)）。
func batchInsertWithEmbedding[T any](database *gorm.DB, items []T, batchSize int) error {
	if len(items) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		if err := database.Create(&batch).Error; err != nil {
			return fmt.Errorf("batch insert with embedding [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// cleanByCondition 按条件清理（返回受影响行数）
//
// 物理删除（Unscoped）：seed 阶段需要彻底清掉旧数据，避免软删除记录与新写入的 unique 约束冲突。
// 软删除字段（gorm.DeletedAt）的表在 seed 阶段不走软删，运行时业务侧软删逻辑不受影响。
func cleanByCondition(database *gorm.DB, model any, query string, args ...any) (int64, error) {
	tx := database.Unscoped().Where(query, args...).Delete(model)
	return tx.RowsAffected, tx.Error
}




