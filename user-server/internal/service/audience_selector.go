// Package service 的圈选层：把 SOP 的"这轮该找谁"从一张手填的静态名单，换成按条件实时取。
//
// 三条口径是本卡存在的理由，改动这一块前先读它们：
//  1. **不新建人群表**（C7）：三个源（RFM 分层 / 客户标签 / churn 评分）各自已有权威表，
//     圈选只做读和交，不物化任何"人群"对象。
//  2. **报错一律上抛**：吞成"0 人"是本卡唯一会让下游全部判错的失败模式 —— 读不到表和
//     这个条件没人命中，在下游看起来逐字节相同。
//  3. **空结果要说清是哪种空**：数据源没在跑（churn_scores 的统计源现为 `return nil, nil`，
//     周批每轮自己空跑退出、一行都不写）与条件太严，是两种必须分别处置的结论。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	// DefaultAudienceLimit 未写 limit 时一轮圈多少人来开工。
	DefaultAudienceLimit = 200
	// MaxAudienceLimit 是硬上限：配置写得再大也夹到这里。调度器每人一条 SOPExecution，
	// 没有这一道，一个手滑的 limit 就能把一轮变成几千条并发执行。
	MaxAudienceLimit = 500

	// audienceSegmentPageSize 分页取 RFM 分层时的页宽。
	// 不能直接把 limit 当 pageSize 传：ListBySegment 把 pageSize>200 静默改成 20，
	// 于是"上限 500"会悄悄变成"只取前 20"。
	audienceSegmentPageSize = 200

	// Reasons 取值。no_match 与 source_empty 的差别就是判据 3：
	// 前者改条件，后者去查数据源有没有在跑。
	audienceReasonNoCondition = "no_condition"
	audienceReasonNoMatch     = "no_match"
	audienceReasonSourceEmpty = "source_empty"

	audienceSourceChurn = "churn"
	audienceSourceRFM   = "rfm"
)

// TriggerConfig 上的三个键。前端目前没有任何写入方（本轮实测：user-web/src 里
// trigger_config 零引用），所以这三把键的**唯一**生产者就是运营在 SOP 配置里手填的 JSON，
// 以及调度器自己写的 audience_preview。
const (
	triggerKeyAudience        = "audience"
	triggerKeyAudienceConfirm = "audience_confirmed"
	triggerKeyAudiencePreview = "audience_preview"
)

// AudienceConfig TriggerConfig["audience"] 解析后的圈选条件。
type AudienceConfig struct {
	Segments  []string
	Tags      []string
	MaxPAlive float64 // >0 时启用 churn 条件：只取 p_alive 低于该值的客户
	Limit     int
}

// HasCondition 是否至少有一条生效条件。
func (c AudienceConfig) HasCondition() bool {
	return len(c.Segments) > 0 || len(c.Tags) > 0 || c.MaxPAlive > 0
}

// limit 归一化：未写走默认，写大了夹到硬上限。
func (c AudienceConfig) limit() int {
	if c.Limit <= 0 {
		return DefaultAudienceLimit
	}
	if c.Limit > MaxAudienceLimit {
		return MaxAudienceLimit
	}
	return c.Limit
}

// AudienceSelection 一次圈选的结果。
type AudienceSelection struct {
	CustomerIDs []string
	// Truncated 至少一个条件的命中数超过了本轮上限。交集是在截断后的集合上算的，
	// 所以它同时意味着"可能有人被漏掉"，不只是"名单小了"。
	Truncated bool
	// Reasons 名单为空、或某个条件空手时的因（见上面的取值）。圈到人时为 nil。
	Reasons []string
}

// AudienceSelector 按条件实时取名单。只读，不写任何表。
type AudienceSelector struct {
	rfm   repository.CustomerRFMRepository
	tags  repository.CustomerTagAssignmentRepository
	churn *repository.ChurnScoreRepository
}

// NewAudienceSelectorWithDB 用同一个库装配三个源。
func NewAudienceSelectorWithDB(database *gorm.DB) *AudienceSelector {
	if database == nil {
		return nil
	}
	return &AudienceSelector{
		rfm:   repository.NewCustomerRFMRepositoryWithDB(database),
		tags:  repository.NewCustomerTagAssignmentRepositoryWithDB(database),
		churn: repository.NewChurnScoreRepository(database),
	}
}

// Select 逐条件取名单后取交集。
//
// 交集（不是并集）是配置的语义：勾了 champion 又勾了 vip，要的是"既是 champion 又打了 vip 标"，
// 并集会让规模变成两拨人之和。任一条件空手，结果即为空 —— 但每个空手的条件都留一条 reason，
// 所以所有条件都会跑完（不因前面空手而提前返回，那会藏掉"另一个条件其实也没人"）。
// 条件顺序固定为 segments → tags → churn，输出名单沿用第一个条件的排序
// （RFM 按价值分倒序、标签按最近打标、churn 按 p_alive 升序），让截断时留下的是"最该触达的那批"。
func (s *AudienceSelector) Select(ctx context.Context, cfg AudienceConfig) (*AudienceSelection, error) {
	out := &AudienceSelection{CustomerIDs: []string{}}
	if !cfg.HasCondition() {
		out.Reasons = append(out.Reasons, audienceReasonNoCondition)
		return out, nil
	}
	if s == nil || s.rfm == nil || s.tags == nil || s.churn == nil {
		return nil, errors.New("audience: 圈选源未装配")
	}
	limit := cfg.limit()

	var sets [][]string

	for _, seg := range cfg.Segments {
		ids, total, err := s.listBySegment(ctx, seg, limit)
		if err != nil {
			return nil, fmt.Errorf("圈选 segment=%s 失败: %w", seg, err)
		}
		if len(ids) == 0 {
			hasRows, err := s.rfmHasAnyRow(ctx)
			if err != nil {
				return nil, fmt.Errorf("圈选 segment=%s 判源失败: %w", seg, err)
			}
			if hasRows {
				out.Reasons = append(out.Reasons, audienceReasonNoMatch+":segment="+seg)
			} else {
				out.Reasons = append(out.Reasons, audienceReasonSourceEmpty+":"+audienceSourceRFM)
			}
		} else if int64(len(ids)) < total {
			out.Truncated = true
		}
		sets = append(sets, ids)
	}

	for _, tag := range cfg.Tags {
		ids, total, err := s.tags.ListCustomerIDsByTag(ctx, tag, limit)
		if err != nil {
			return nil, fmt.Errorf("圈选 tag=%s 失败: %w", tag, err)
		}
		if len(ids) == 0 {
			out.Reasons = append(out.Reasons, audienceReasonNoMatch+":tag="+tag)
		} else if int64(len(ids)) < total {
			out.Truncated = true
		}
		sets = append(sets, ids)
	}

	if cfg.MaxPAlive > 0 {
		// 多取一条用来判"是取不完还是真没人"：ListByPAliveBelow 自己不报总数。
		ids, err := s.listChurnRisk(ctx, cfg.MaxPAlive, limit+1)
		if err != nil {
			return nil, fmt.Errorf("圈选 churn p_alive<%s 失败: %w", formatAudienceFloat(cfg.MaxPAlive), err)
		}
		if len(ids) > limit {
			out.Truncated = true
			ids = ids[:limit]
		}
		if len(ids) == 0 {
			n, err := s.churn.Count(ctx)
			if err != nil {
				return nil, fmt.Errorf("圈选 churn 源计数失败: %w", err)
			}
			if n == 0 {
				out.Reasons = append(out.Reasons, audienceReasonSourceEmpty+":"+audienceSourceChurn)
			} else {
				out.Reasons = append(out.Reasons, audienceReasonNoMatch+":"+audienceSourceChurn)
			}
		}
		sets = append(sets, ids)
	}

	out.CustomerIDs = intersectAudienceIDs(sets)
	return out, nil
}

// listBySegment 按页取某一层的全部命中（最多 limit 个），并回传该层总人数。
func (s *AudienceSelector) listBySegment(ctx context.Context, segment string, limit int) ([]string, int64, error) {
	ids := make([]string, 0, limit)
	var total int64
	for page := 1; ; page++ {
		rows, count, err := s.rfm.ListBySegment(ctx, segment, page, audienceSegmentPageSize)
		if err != nil {
			return nil, 0, err
		}
		total = count
		for _, row := range rows {
			if len(ids) >= limit {
				break
			}
			ids = append(ids, row.CustomerID)
		}
		if len(rows) < audienceSegmentPageSize || len(ids) >= limit {
			break
		}
	}
	return ids, total, nil
}

// listChurnRisk 取 p_alive 低于阈值的客户键。
//
// ⚠️ 这个键**直接当 customer_id 用**，而它的语义目前无从核对：churn_scores 的生产者是桩
// （`defaultChurnStatsQuery` 回 nil，见 churn_score_job.go），所以仓库里没有任何一行真实数据
// 能证明 customer_key 写的是 customers.customer_id 而不是 oneid/渠道键 —— 列宽也不同
// （varchar(120) vs customer_id 的 varchar(64)）。接生产者的人必须先对齐这两者，
// 否则 churn 条件圈出来的人会整批对不上号。在那之前，该条件在真实环境恒报 source_empty:churn。
func (s *AudienceSelector) listChurnRisk(ctx context.Context, threshold float64, limit int) ([]string, error) {
	rows, err := s.churn.ListByPAliveBelow(ctx, threshold, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.CustomerKey)
	}
	return ids, nil
}

// rfmHasAnyRow 分层表里有没有任何一行 —— 用来把"整表没数据（分层 cron 没在跑）"
// 和"这一层暂时没人"分开。只看总量：分层是按人写的，单看一层分不出这两者。
func (s *AudienceSelector) rfmHasAnyRow(ctx context.Context) (bool, error) {
	dist, err := s.rfm.CountBySegment(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range dist {
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// intersectAudienceIDs 多集合求交，顺序沿用第一个集合；单个集合直接原样返回。
func intersectAudienceIDs(sets [][]string) []string {
	if len(sets) == 0 {
		return []string{}
	}
	if len(sets) == 1 {
		return sets[0]
	}
	members := make([]map[string]struct{}, 0, len(sets)-1)
	for _, set := range sets[1:] {
		m := make(map[string]struct{}, len(set))
		for _, id := range set {
			m[id] = struct{}{}
		}
		members = append(members, m)
	}
	out := make([]string, 0, len(sets[0]))
	for _, id := range sets[0] {
		inAll := true
		for _, m := range members {
			if _, ok := m[id]; !ok {
				inAll = false
				break
			}
		}
		if inAll {
			out = append(out, id)
		}
	}
	return out
}

func formatAudienceFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// parseAudienceConfig 读 TriggerConfig["audience"]。
//
// 第二项表示"有没有声明 audience 这个块"：调度器据此区分"没配圈选（走静态名单通道）"
// 和"配了圈选但条件一条都没生效（要报 no_condition）"。
// 形状不符（不是对象、字段类型错）降级为该条件不存在 —— 不猜也不 panic。
// jsonb 解出来数字恒为 float64，所以 limit 只认 float64，写成字符串按未配置处理。
func parseAudienceConfig(cfg model.JSONMap) (AudienceConfig, bool) {
	raw, ok := cfg[triggerKeyAudience]
	if !ok {
		return AudienceConfig{}, false
	}
	block, ok := toAnyKeyMap(raw)
	if !ok {
		return AudienceConfig{}, false
	}
	return AudienceConfig{
		Segments:  audienceStringSlice(block["segments"]),
		Tags:      audienceStringSlice(block["tags"]),
		MaxPAlive: audiencePositiveFloat(block["max_p_alive"]),
		Limit:     int(audiencePositiveFloat(block["limit"])),
	}, true
}

func toAnyKeyMap(raw any) (map[string]any, bool) {
	switch m := raw.(type) {
	case map[string]any:
		return m, true
	case model.JSONMap:
		return m, true
	}
	return nil, false
}

func audienceStringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func audiencePositiveFloat(raw any) float64 {
	if v, ok := raw.(float64); ok && v > 0 {
		return v
	}
	return 0
}
