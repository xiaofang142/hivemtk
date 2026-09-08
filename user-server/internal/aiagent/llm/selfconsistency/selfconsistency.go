// Package selfconsistency 实现 Self-Consistency 多采样投票
//
// 学术依据：
//   - Wang, X. et al. (2022). "Self-Consistency Improves Chain of Thought Reasoning in Language Models".
//   - 业界：CoT 推理任务的事实标准增强（GSM8K 17.9% → 78.9%）
//
// 原理：
//   - 同一 prompt 采样 N 次（不同 temperature）
//   - 多数投票（majority vote）选最一致答案
//   - 业界 N 默认 5-10（成本 / 收益 trade-off）
//
// 适用：
//   - 推理 / 数学 / 事实性问题（答案可枚举）
//   - 不可用于开放式生成（每个答案都不一样）
//
// 业界集成模式：
//   - LLM Router + Self-Consistency（router 选择不同 provider 投票）
//   - 同 provider + 不同 temperature（控制方差）
//   - 同 provider + 不同 seed（random）
package selfconsistency

import (
	"context"
	"sort"
	"sync"
)

// Sampler 多采样接口
type Sampler[T any] interface {
	// Sample 单次采样
	Sample(ctx context.Context) (T, error)
}

// Voter 投票接口
type Voter[T any] interface {
	// Key 提取用于投票的 key（答案的等价性判定）
	Key(answer T) string
}

// SelfConsistency 多采样投票器
type SelfConsistency[T any] struct {
	samples int
}

// NewSelfConsistency 构造投票器
//
// samples <= 0 默认 5（业界推荐）
func NewSelfConsistency[T any](samples int) *SelfConsistency[T] {
	if samples <= 0 {
		samples = 5
	}
	return &SelfConsistency[T]{samples: samples}
}

// VoteResult 投票结果
type VoteResult[T any] struct {
	Winner     T
	WinnerKey  string
	Count      int
	Total      int
	Confidence float64
	AllKeys    []VoteCount[T]
}

// VoteCount 单个答案的票数
type VoteCount[T any] struct {
	Key    string
	Count  int
	Sample T
}

// Run 并发采样 N 次 + 投票
//
// 流程：
//  1. 并发 N 次 Sample
//  2. Key 提取用于投票
//  3. 多数票胜出
//  4. 返回 Winner + Confidence
//
// 设计：
//   - 并发采样：N 个 goroutine 同步执行（总时间 ≈ 1 次采样时间）
//   - 错误处理：单个失败不阻断整体，err 返回到 result 字段
//   - 平票：取最先出现的 key
func (sc *SelfConsistency[T]) Run(ctx context.Context, sampler Sampler[T], voter Voter[T]) (VoteResult[T], error) {
	if sampler == nil {
		return VoteResult[T]{}, ErrNilSampler
	}
	if voter == nil {
		return VoteResult[T]{}, ErrNilVoter
	}

	results := make([]sampledAnswer[T], sc.samples)
	var wg sync.WaitGroup
	errCh := make(chan error, sc.samples)
	for i := 0; i < sc.samples; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ans, err := sampler.Sample(ctx)
			results[idx] = sampledAnswer[T]{answer: ans, err: err}
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	counts := make(map[string]int)
	samples := make(map[string]T)

	allKeysMap := make(map[string]VoteCount[T])
	total := 0
	for _, r := range results {
		if r.err != nil {
			continue
		}
		key := voter.Key(r.answer)
		counts[key]++
		if _, ok := samples[key]; !ok {
			samples[key] = r.answer
		}

		if _, ok := allKeysMap[key]; !ok {
			allKeysMap[key] = VoteCount[T]{Key: key, Count: counts[key], Sample: r.answer}
		} else {

			existing := allKeysMap[key]
			existing.Count = counts[key]
			allKeysMap[key] = existing
		}
		total++
	}

	if total == 0 {
		return VoteResult[T]{
			AllKeys: []VoteCount[T]{},
		}, ErrNoValidSamples
	}

	type kv struct {
		key   string
		count int
		idx   int
	}
	pairs := make([]kv, 0, len(counts))
	idx := 0
	for k, c := range counts {
		pairs = append(pairs, kv{key: k, count: c, idx: idx})
		idx++
	}
	sort.SliceStable(pairs, func(i, j int) bool {

		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].idx < pairs[j].idx
	})
	winner := pairs[0]

	allKeys := make([]VoteCount[T], 0, len(allKeysMap))
	for _, v := range allKeysMap {
		allKeys = append(allKeys, v)
	}
	sort.SliceStable(allKeys, func(i, j int) bool {
		return allKeys[i].Count > allKeys[j].Count
	})

	// 代表值优先取与归一化 key 完全一致的原始样本（大小写规范的那个）；
	// 若同 key 无任何原始样本等于 key，则退回首见样本
	winnerRep := samples[winner.key]
	for _, r := range results {
		if r.err != nil {
			continue
		}
		if voter.Key(r.answer) == winner.key {
			if norm, ok := any(winner.key).(T); ok && any(r.answer) == any(norm) {
				winnerRep = r.answer
				break
			}
		}
	}
	return VoteResult[T]{
		Winner:     winnerRep,
		WinnerKey:  winner.key,
		Count:      winner.count,
		Total:      total,
		Confidence: float64(winner.count) / float64(total),
		AllKeys:    allKeys,
	}, nil
}

type sampledAnswer[T any] struct {
	answer T
	err    error
}

// Errors
var (
	ErrNilSampler     = constErr("nil sampler")
	ErrNilVoter       = constErr("nil voter")
	ErrNoValidSamples = constErr("no valid samples")
)

type constErr string

func (e constErr) Error() string { return string(e) }
