package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// MergePreviewRule 预览请求中的单条合并规则
//
// 契约对齐前端 MergeRuleConfig.vue 的扁平规则结构：
// {id, name, type, fields:[], threshold, priority, action, enabled}
// 后端仅消费与合并判定相关的字段（fields/op/threshold/enabled）。
type MergePreviewRule struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type,omitempty"`
	Fields      []string  `json:"fields"`
	Field       string    `json:"field"`
	Op          string    `json:"op"`
	Value       string    `json:"value"`
	Threshold   int       `json:"threshold"`
	Priority    int       `json:"priority"`
	Action      string    `json:"action,omitempty"`
	Enabled     bool      `json:"enabled"`
	BuiltIn     bool      `json:"built_in"`
	Description string    `json:"description"`
	Example     string    `json:"example"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// MergePreviewSample 一条候选合并对样例。Rule/Action 让预览能说明
// "按哪条规则、以何种动作（自动/审核）命中"，与正式执行口径一致。
type MergePreviewSample struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Score  int    `json:"score"`
	Rule   string `json:"rule,omitempty"`
	Action string `json:"action,omitempty"`
}

// MergePreview 预览结果
type MergePreview struct {
	CandidateCount int                  `json:"candidateCount"`
	Samples        []MergePreviewSample `json:"samples"`
}

// DefaultPreviewSampleLimit OneID 合并预览的最大采样数（参数中心未配置时的兜底）
const DefaultPreviewSampleLimit = 20

var previewSampleLimitProvider = func() int { return DefaultPreviewSampleLimit }

// SetPreviewSampleLimitProvider 由装配层注入参数中心读取口（nil 视为不注入）
func SetPreviewSampleLimitProvider(fn func() int) {
	if fn != nil {
		previewSampleLimitProvider = fn
	}
}

// previewSampleLimit 非正值一律回落兜底：容量参数传 0 会 make 出零长切片，
// 下面的 len 判据又恒成立，循环会在第一条就 break，预览永远空。
func previewSampleLimit() int {
	v := previewSampleLimitProvider()
	if v <= 0 {
		return DefaultPreviewSampleLimit
	}
	return v
}

// PreviewMergeRules 预览「按当前规则会合并哪些身份」：
// 拉取全量客户，按每条启用规则的匹配字段分组，
// 同组（值相等或前缀相同）≥2 个客户即产生候选合并对。
// candidateCount 为去重后的候选对总数；samples 最多返回 20 条。
func (s *OneIDMergeRuleService) PreviewMergeRules(ctx context.Context, rules []MergePreviewRule) (*MergePreview, error) {
	repo := repository.NewCustomerRepository()
	const pageSize = 500
	customers := make([]*model.Customer, 0, pageSize*2)
	for offset := 0; ; offset += pageSize {
		batch, _, err := repo.List(ctx, offset/pageSize+1, pageSize, "")
		if err != nil {
			logger.Warnf("[OneID preview] 拉取客户失败 offset=%d: %v", offset, err)
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		customers = append(customers, batch...)
		if len(batch) < pageSize {
			break
		}
	}

	type previewPair struct {
		Score  int
		Rule   string
		Action string
	}
	pairScore := make(map[string]previewPair)
	addPair := func(a, b *model.Customer, score int, rule, action string) {
		if a.ID == b.ID || a.ID == "" || b.ID == "" {
			return
		}
		x, y := a.ID, b.ID
		if x > y {
			x, y = y, x
		}
		key := x + "|" + y
		if score > pairScore[key].Score {
			pairScore[key] = previewPair{Score: score, Rule: rule, Action: action}
		}
	}

	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		// FA-44：预览与正式合并共用 evaluateRule（多字段/阈值口径一致），
		// 单字段构造一条 MergeRule，逐字段判定后取最高分。
		fields := r.Fields
		if len(fields) == 0 && r.Field != "" {
			fields = []string{r.Field}
		}
		op := r.Op
		if op == "" {
			op = "eq"
		}
		action := ruleAction(MergeRule{Action: r.Action})
		// 预览展示分沿用既有契约「命中即展示该规则的阈值」（缺省 100），
		// 而命中判定本身仍走 evaluateRule 的多字段/阈值口径，与正式合并一致。
		displayScore := r.Threshold
		if displayScore <= 0 {
			displayScore = 100
		}
		for _, f := range fields {
			mr := MergeRule{Fields: []string{f}, Field: f, Op: op, Threshold: r.Threshold, Action: r.Action}
			// eq/prefix 可按值分组加速；like 逐对比较。两路最终判定都走 evaluateRule。
			if op != "like" {
				groups := make(map[string][]*model.Customer)
				for _, c := range customers {
					v := customerIdentityMap(c)[f]
					if v == "" {
						continue
					}
					key := v
					if op == "prefix" {
						if len(v) < 7 {
							continue
						}
						key = v[:7]
					}
					groups[key] = append(groups[key], c)
				}
				for _, owners := range groups {
					if len(owners) < 2 {
						continue
					}
					for i := 0; i < len(owners); i++ {
						for j := i + 1; j < len(owners); j++ {
							if hit, _ := evaluateRule(customerIdentityMap(owners[i]), customerIdentityMap(owners[j]), mr); hit {
								addPair(owners[i], owners[j], displayScore, r.Name, action)
							}
						}
					}
				}
				continue
			}
			for i := 0; i < len(customers); i++ {
				for j := i + 1; j < len(customers); j++ {
					if hit, _ := evaluateRule(customerIdentityMap(customers[i]), customerIdentityMap(customers[j]), mr); hit {
						addPair(customers[i], customers[j], displayScore, r.Name, action)
					}
				}
			}
		}
	}

	keys := make([]string, 0, len(pairScore))
	for k := range pairScore {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sampleLimit := previewSampleLimit()
	samples := make([]MergePreviewSample, 0, sampleLimit)
	for _, k := range keys {
		if len(samples) >= sampleLimit {
			break
		}
		parts := strings.SplitN(k, "|", 2)
		p := pairScore[k]
		samples = append(samples, MergePreviewSample{From: parts[0], To: parts[1], Score: p.Score, Rule: p.Rule, Action: p.Action})
	}
	return &MergePreview{CandidateCount: len(pairScore), Samples: samples}, nil
}

// ProbePreviewSampleLimit 暴露读取口当前值，供装配层测试断言接线确实生效。
func ProbePreviewSampleLimit() int { return previewSampleLimit() }
