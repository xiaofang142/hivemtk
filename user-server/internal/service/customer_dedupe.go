package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// DuplicateCandidate 一对疑似重复客户及命中依据。
type DuplicateCandidate struct {
	LeftID    string `json:"left_id"`
	RightID   string `json:"right_id"`
	LeftName  string `json:"left_name"`
	RightName string `json:"right_name"`
	Score     int    `json:"score"`
	Rule      string `json:"rule"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
}

// DedupeScanOptions 去重扫描参数。
type DedupeScanOptions struct {
	MaxCustomers    int  // 单次扫描最多读取客户数（0 用默认）
	NameThreshold   int  // 名称相似度阈值 0-100（0 用默认 85）
	SameAccountOnly bool // 仅在同一来源账号内比对（默认 true，缩小跨账号误合并面）
}

// CustomerDedupeService 客户去重合并（FR-003/005）。
//
// 勘察结论：本仓已有一套完整的 OneID 规则引擎（OneIDMergeRuleService：手机号/邮箱/
// 外部 ID/抖音/小红书）+ 执行器（CustomerIdentityService.MergeByIdentity →
// CustomerService.MergeCustomers）。本服务不重建那套能力，而是复用其规则判定与合并
// 执行，补齐「名称相似度」与「合并审计留痕」两块缺口，并对管理端暴露统一入口。
type CustomerDedupeService struct {
	customers repository.CustomerRepository
	ruleSvc   *OneIDMergeRuleService
	mergeRepo repository.CustomerMergeRepository
	auditRepo repository.OperationLogRepository
}

func NewCustomerDedupeService() *CustomerDedupeService {
	return &CustomerDedupeService{
		customers: repository.NewCustomerRepository(),
		ruleSvc:   NewOneIDMergeRuleService(),
		mergeRepo: repository.NewCustomerMergeRepository(),
		auditRepo: repository.NewOperationLogRepository(),
	}
}

const (
	defaultDedupeMaxCustomers  = 5000
	defaultDedupeNameThreshold = 85
	dedupePageSize             = 500
)

// FindDuplicates 扫描疑似重复客户。
// 判定顺序：① 既有 OneID 规则（手机号/邮箱/外部 ID/渠道 OpenID）；
// ② 名称相似度（仅作为候选提示，action=review，不自动合并）。
func (s *CustomerDedupeService) FindDuplicates(ctx context.Context, opts DedupeScanOptions) ([]DuplicateCandidate, error) {
	maxCustomers := opts.MaxCustomers
	if maxCustomers <= 0 {
		maxCustomers = defaultDedupeMaxCustomers
	}
	nameThreshold := opts.NameThreshold
	if nameThreshold <= 0 {
		nameThreshold = defaultDedupeNameThreshold
	}
	if nameThreshold > 100 {
		nameThreshold = 100
	}

	all := make([]*model.Customer, 0, dedupePageSize*2)
	for page := 1; len(all) < maxCustomers; page++ {
		batch, _, err := s.customers.List(ctx, page, dedupePageSize, "")
		if err != nil {
			return nil, fmt.Errorf("扫描客户失败 page=%d: %w", page, err)
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if len(batch) < dedupePageSize {
			break
		}
	}
	if len(all) > maxCustomers {
		all = all[:maxCustomers]
	}

	// 规则集只读一次：ApplyRulesWithAction 内部每次都会 GetRules（KV/DB 读），
	// 在 O(n²) 比对里逐对调用会把一次扫描变成上万次 KV 读。
	rules, err := s.ruleSvc.GetRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取 OneID 合并规则失败: %w", err)
	}
	identities := make([]map[string]string, len(all))
	for i, c := range all {
		identities[i] = customerIdentityMap(c)
	}

	seen := make(map[string]bool)
	candidates := make([]DuplicateCandidate, 0)
	addPair := func(a, b *model.Customer, score int, rule, action, reason string) {
		if a == nil || b == nil || a.ID == b.ID || a.ID == "" || b.ID == "" {
			return
		}
		x, y := a.ID, b.ID
		if x > y {
			x, y = y, x
		}
		key := x + "|" + y
		if seen[key] {
			return
		}
		seen[key] = true
		candidates = append(candidates, DuplicateCandidate{
			LeftID: x, RightID: y, LeftName: a.Name, RightName: b.Name,
			Score: score, Rule: rule, Action: action, Reason: reason,
		})
	}

	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			a, b := all[i], all[j]
			if opts.SameAccountOnly && a.SourceAccountID != "" && b.SourceAccountID != "" &&
				a.SourceAccountID != b.SourceAccountID {
				continue
			}
			if hit, ruleName, action, score := matchDedupeRules(rules, identities[i], identities[j]); hit {
				addPair(a, b, score, ruleName, action, "命中既有 OneID 合并规则")
				continue
			}
			if ns := NameSimilarityScore(a.Name, b.Name); ns >= nameThreshold {
				addPair(a, b, ns, "名称相似度", "review",
					fmt.Sprintf("名称相似度 %d ≥ 阈值 %d，需人工复核", ns, nameThreshold))
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].LeftID < candidates[j].LeftID
	})
	return candidates, nil
}

// matchDedupeRules 用已加载的规则集判定一对身份，口径与
// OneIDMergeRuleService.ApplyRulesWithAction 完全一致（evaluateRule + ruleAction）。
func matchDedupeRules(set *MergeRuleSet, a, b map[string]string) (bool, string, string, int) {
	if set == nil {
		return false, "", "", 0
	}
	best := 0
	bestName, bestAction := "", ""
	for _, group := range [][]MergeRule{set.BuiltIn, set.Custom} {
		for _, r := range group {
			if !r.Enabled {
				continue
			}
			if hit, score := evaluateRule(a, b, r); hit && score > best {
				best, bestName, bestAction = score, r.Name, ruleAction(r)
			}
		}
	}
	return best > 0, bestName, bestAction, best
}

// Merge 执行合并并写审计（FR-005）。复用 CustomerService.MergeCustomers 保证
// 身份/会话/同意关系的迁移语义与既有链路完全一致。
func (s *CustomerDedupeService) Merge(ctx context.Context, primaryID, secondaryID, operatorName, reason, source string) (*model.Customer, error) {
	if primaryID == "" || secondaryID == "" || primaryID == secondaryID {
		return nil, errors.New("primary_id / secondary_id 必须不同且非空")
	}
	primary, err := s.customers.GetByID(ctx, primaryID)
	if err != nil || primary == nil {
		return nil, fmt.Errorf("主客户不存在: %w", err)
	}
	secondary, err := s.customers.GetByID(ctx, secondaryID)
	if err != nil || secondary == nil {
		return nil, fmt.Errorf("次要客户不存在: %w", err)
	}

	original, _ := json.Marshal(map[string]any{
		"id": secondary.ID, "name": secondary.Name, "phone": secondary.Phone,
		"email": secondary.Email, "unified_id": secondary.UnifiedID,
		"source_account_id": secondary.SourceAccountID, "source_channel": secondary.SourceChannel,
		"industry": secondary.Industry, "merged_at": time.Now().UTC().Format(time.RFC3339),
	})
	audit := &model.CustomerMerge{
		MergeID:        fmt.Sprintf("cm_%d", time.Now().UnixNano()),
		OneID:          primary.UnifiedID,
		PrimaryID:      primaryID,
		SecondaryID:    secondaryID,
		Source:         source,
		Action:         "manual",
		OriginalValues: string(original),
		Reason:         reason,
		OperatorName:   operatorName,
	}

	if err := s.customers.WithTransaction(ctx, func(txCtx context.Context) error {
		svc := NewCustomerService()
		if err := svc.MergeCustomers(txCtx, primaryID, secondaryID); err != nil {
			return err
		}
		return s.mergeRepo.Create(txCtx, audit)
	}); err != nil {
		return nil, fmt.Errorf("合并客户失败: %w", err)
	}

	s.writeAudit(ctx, primaryID, secondaryID, operatorName, reason)
	return primary, nil
}

// ListMergeAudit 查询某客户的合并审计。
func (s *CustomerDedupeService) ListMergeAudit(ctx context.Context, customerID string) ([]*model.CustomerMerge, error) {
	return s.mergeRepo.ListByCustomer(ctx, customerID, 100)
}

func (s *CustomerDedupeService) writeAudit(ctx context.Context, primaryID, secondaryID, operatorName, reason string) {
	if s.auditRepo == nil {
		return
	}
	detail, _ := json.Marshal(map[string]any{
		"primary_id": primaryID, "secondary_id": secondaryID, "reason": reason,
	})
	_ = s.auditRepo.Create(ctx, &model.OperationLog{
		UserID:     0,
		Username:   operatorName,
		Action:     "customer_merge",
		Module:     "crm",
		Resource:   "customer",
		ResourceID: primaryID,
		Detail:     string(detail),
	})
}

// NameSimilarityScore 名称相似度（0-100）。
// 归一化：小写 + 去空白/标点；再用编辑距离比率，中文按 rune 计。
func NameSimilarityScore(a, b string) int {
	na, nb := normalizeCustomerName(a), normalizeCustomerName(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 100
	}
	dist := levenshtein([]rune(na), []rune(nb))
	maxLen := len([]rune(na))
	if len([]rune(nb)) > maxLen {
		maxLen = len([]rune(nb))
	}
	if maxLen == 0 {
		return 0
	}
	score := (maxLen - dist) * 100 / maxLen
	if score < 0 {
		return 0
	}
	return score
}

func normalizeCustomerName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r == ' ' || r == '-' || r == '_' || r == '.' || r == ',' || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			v := prev[j] + 1
			if cur[j-1]+1 < v {
				v = cur[j-1] + 1
			}
			if prev[j-1]+cost < v {
				v = prev[j-1] + cost
			}
			cur[j] = v
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
