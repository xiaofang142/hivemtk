// Package selfconsistency 实现 Self-Consistency 多采样投票：同一 prompt 采样 N 次
// （不同 temperature），多数投票选最一致答案。适用于推理 / 数学 / 事实性等答案可
// 枚举的问题，不可用于开放式生成。业界集成模式：Router 选不同 provider 投票，或
// 同 provider + 不同 temperature（控制方差）/ 不同 seed（random）。
package selfconsistency

import (
	"context"
	"sort"
	"sync"

	"hivemtk-user/internal/pkg/utils"
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

// NewSelfConsistency 构造投票器；samples <= 0 默认 5（业界推荐）。
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

// Run 并发采样 N 次 + 投票：并发 N 个 Sample（总时间 ≈ 1 次采样），Key 提取等价答案，
// 多数票胜出。单个采样失败不阻断整体；平票取最先出现的 key。
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
		// SafeGo：并发采样跑不可信 LLM 输出链，panic 不能击穿进程；
		// panic 路 wg.Done 已 defer，本轮结果保持零值参与聚合（降级不崩溃）。
		utils.SafeGo(ctx, "llm.selfconsistency.sample", func(_ context.Context) {
			defer wg.Done()
			ans, err := sampler.Sample(ctx)
			results[i] = sampledAnswer[T]{answer: ans, err: err}
			if err != nil {
				errCh <- err
			}
		})
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
