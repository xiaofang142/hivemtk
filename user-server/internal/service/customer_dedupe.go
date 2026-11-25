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
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/repository"
)

// DuplicateCandidate 一对疑似重复客户/实体及命中依据。
// 实体类型为空串表示既有 Customer 候选；其余取值见 DedupeEntity* 常量。
// Unsupported/Field 用于把规则不支持的实体与命中字段显式呈现给人工，不假报已完成。
type DuplicateCandidate struct {
	LeftID      string `json:"left_id"`
	RightID     string `json:"right_id"`
	LeftName    string `json:"left_name"`
	RightName   string `json:"right_name"`
	Score       int    `json:"score"`
	Rule        string `json:"rule"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	EntityType  string `json:"entity_type,omitempty"`
	Field       string `json:"field,omitempty"`
	Unsupported string `json:"unsupported,omitempty"`
}

// 多实体去重的实体类型标识（与前端选型一致）。
const (
	DedupeEntityContact         = "Contact"
	DedupeEntitySupplier        = "Supplier"
	DedupeEntityServiceProvider = "ServiceProvider"
)

// supportedDedupeEntities 明确列出有标识适配器的实体；其他实体不得静默跳过。
var supportedDedupeEntities = map[string]bool{
	DedupeEntityContact: true, DedupeEntitySupplier: true, DedupeEntityServiceProvider: true,
}

// DedupeEntityScanOptions 多实体去重扫描参数。EntityTypes 为空时扫描三类支持的实体。
type DedupeEntityScanOptions struct {
	Scope       CustomerDataScope
	EntityTypes []string
	// NameThreshold 名称相似度阈值（0 用默认 85）；强标识命中优先。
	NameThreshold int
}

// dedupeEntityRecord 统一后的实体记录：只含判定与输出所需字段。
type dedupeEntityRecord struct {
	ID         string
	Name       string
	Kind       string
	CustomerID string
	AccountID  *uint
	Role       string
	Identity   map[string]string
	HitField   string
}

// DedupeScanOptions 去重扫描参数。
type DedupeScanOptions struct {
	MaxCustomers    int  // 单次扫描最多读取客户数（0 用默认）
	NameThreshold   int  // 名称相似度阈值 0-100（0 用默认 85）
	SameAccountOnly bool // 仅在同一来源账号内比对（默认 true，缩小跨账号误合并面）
	// Scope 由调用方传入的数据权限范围；管理员（AllAccounts）才扫描租户全量。
	Scope CustomerDataScope
}

// CustomerDedupeService 客户去重合并（FR-003/005）。
//
// 勘察结论：本仓已有一套完整的 OneID 规则引擎（OneIDMergeRuleService：手机号/邮箱/
// 外部 ID/抖音/小红书）+ 执行器（CustomerIdentityService.MergeByIdentity →
// CustomerService.MergeCustomers）。本服务不重建那套能力，而是复用其规则判定与合并
// 执行，补齐「名称相似度」与「合并审计留痕」两块缺口，并对管理端暴露统一入口。
type CustomerDedupeService struct {
	customers   repository.CustomerRepository
	scoped      repository.CRMCustomerRepository
	permission  *CustomerPermissionService
	ruleSvc     *OneIDMergeRuleService
	mergeRepo   repository.CustomerMergeRepository
	auditRepo   repository.OperationLogRepository
	contacts    repository.ContactRepository
	suppliers   repository.SupplierRepository
	providers   repository.ServiceProviderRepository
	undoApplier MergeUndoApplier
	consentRepo *repository.ContactConsentRepository
}

func NewCustomerDedupeService() *CustomerDedupeService {
	return &CustomerDedupeService{
		customers:   repository.NewCustomerRepository(),
		scoped:      repository.NewCRMCustomerRepository(),
		permission:  NewCustomerPermissionService(),
		ruleSvc:     NewOneIDMergeRuleService(),
		mergeRepo:   repository.NewCustomerMergeRepository(),
		auditRepo:   repository.NewOperationLogRepository(),
		contacts:    repository.NewContactRepository(),
		suppliers:   repository.NewSupplierRepository(),
		providers:   repository.NewServiceProviderRepository(),
		consentRepo: repository.NewContactConsentRepository(dbstate.Get()),
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
	// 无登录身份（userID=0）不得扫描：避免零值 scope 绕过受限过滤读到全租户客户。
	if !opts.Scope.AllAccounts && opts.Scope.UserID == 0 && (s.permission != nil || s.scoped != nil) {
		return nil, ErrCRMCustomerForbidden
	}

	all := make([]*model.Customer, 0, dedupePageSize*2)
	for page := 1; len(all) < maxCustomers; page++ {
		var batch []*model.Customer
		var err error
		if !opts.Scope.AllAccounts && s.permission != nil && s.scoped != nil {
			// 数据权限收口：受限用户只能扫描自己范围内客户（FR-002/SC-002）。
			// RestrictedAccounts=true 且账号集合为空时，ListScoped 会生成 1=0（fail-closed）。
			filter := s.permission.ToFilter(opts.Scope, repository.CustomerScopeFilter{})
			filter.Page = page
			filter.PageSize = dedupePageSize
			batch, _, err = s.scoped.ListScoped(ctx, filter)
		} else {
			batch, _, err = s.customers.List(ctx, page, dedupePageSize, "")
		}
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

// FindEntitiesDuplicates 把去重预览扩展到联系人/供应商/服务商（CRM-06）。
//
// 判定口径：
//   - 实体身份从 ContactInfo(JSONB) 适配为 OneID 规则字段（phone/email/external_id…）；
//   - 候选只来自调用方授权范围内的客户（复用 ListScoped，零授权 fail-closed）；
//   - 同强标识但属于不同实体类型（例如同手机号的 Supplier 与 ServiceProvider）一律
//     review 并显式提示业务角色冲突，不自动抹去任一业务角色；
//   - 规则不支持的实体类型显式返回 Action=unsupported，不静默跳过、不假报已完成。
func (s *CustomerDedupeService) FindEntitiesDuplicates(ctx context.Context, opts DedupeEntityScanOptions) ([]DuplicateCandidate, error) {
	if !opts.Scope.AllAccounts && opts.Scope.UserID == 0 && s.permission != nil {
		return nil, ErrCRMCustomerForbidden
	}
	kinds := opts.EntityTypes
	if len(kinds) == 0 {
		kinds = []string{DedupeEntityContact, DedupeEntitySupplier, DedupeEntityServiceProvider}
	}
	candidates := make([]DuplicateCandidate, 0)
	seenUnsupported := make(map[string]bool)
	for _, kind := range kinds {
		if !supportedDedupeEntities[kind] {
			if !seenUnsupported[kind] {
				seenUnsupported[kind] = true
				candidates = append(candidates, DuplicateCandidate{
					Action: "unsupported", Unsupported: kind, EntityType: kind,
					Reason: "当前去重规则未适配该实体类型，需人工复核，不计入自动去重",
				})
			}
			continue
		}
	}

	// 可见客户范围：受限用户按权限读取，管理员全租户。空授权下 ListScoped 生成 1=0。
	visible, err := s.visibleDedupeCustomers(ctx, opts.Scope)
	if err != nil {
		return nil, err
	}
	rules, err := s.ruleSvc.GetRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取 OneID 合并规则失败: %w", err)
	}
	threshold := opts.NameThreshold
	if threshold <= 0 {
		threshold = defaultDedupeNameThreshold
	}

	records := make([]dedupeEntityRecord, 0)
	for _, kind := range kinds {
		if !supportedDedupeEntities[kind] {
			continue
		}
		for _, cust := range visible {
			rows, err := s.entityRowsForCustomer(ctx, kind, cust.ID)
			if err != nil {
				return nil, err
			}
			records = append(records, rows...)
		}
	}

	seen := make(map[string]bool)
	for i := 0; i < len(records); i++ {
		for j := i + 1; j < len(records); j++ {
			a, b := records[i], records[j]
			left, right := a, b
			if entityRank(right.Kind) < entityRank(left.Kind) ||
				(right.Kind == left.Kind && entityIDNumber(right.ID) < entityIDNumber(left.ID)) {
				left, right = right, left
			}
			hit, ruleName, action, score := matchDedupeRules(rules, a.Identity, b.Identity)
			field := ""
			if hit {
				field = entityHitField(rules, a.Identity, b.Identity)
			}
			ns := NameSimilarityScore(a.Name, b.Name)
			if !hit && ns < threshold {
				continue
			}
			if !hit {
				score, ruleName, action, field = ns, "名称相似度", "review", "name"
			}
			key := left.Kind + "|" + left.ID + "|" + right.Kind + "|" + right.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			// 跨实体角色并存：同一强标识可能代表同一人在不同业务角色，禁止自动抹去。
			// 同实体内的重复仍复用 OneID 既有动作，不重写客户合并规则。
			if a.Kind != b.Kind {
				action = "review"
				ruleName = "跨实体角色冲突"
				field = firstSharedIdentityField(a.Identity, b.Identity)
			}
			reason := "命中既有 OneID 合并规则"
			if a.Kind != b.Kind {
				reason = fmt.Sprintf("%s 与 %s 业务角色不同，手机号等标识相同不代表可合并，需人工确认", a.Kind, b.Kind)
			} else if !hit {
				reason = fmt.Sprintf("名称相似度 %d ≥ 阈值 %d，需人工复核", ns, threshold)
			}
			candidates = append(candidates, DuplicateCandidate{
				LeftID: left.ID, RightID: right.ID, LeftName: left.Name, RightName: right.Name,
				Score: score, Rule: ruleName, Action: action, Reason: reason,
				EntityType: left.Kind, Field: field,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].LeftID < candidates[j].LeftID
	})
	return candidates, nil
}

// visibleDedupeCustomers 读取去重扫描可见的客户 ID 列表；只用于筛选实体，不返回给前端。
func (s *CustomerDedupeService) visibleDedupeCustomers(ctx context.Context, scope CustomerDataScope) ([]*model.Customer, error) {
	all := make([]*model.Customer, 0, dedupePageSize)
	for page := 1; page <= 100; page++ {
		filter := repository.CustomerScopeFilter{Page: page, PageSize: dedupePageSize}
		batch, _, err := s.permission.ListScoped(ctx, scope, filter)
		if err != nil {
			return nil, fmt.Errorf("扫描实体可见客户失败 page=%d: %w", page, err)
		}
		all = append(all, batch...)
		if len(batch) < dedupePageSize {
			break
		}
	}
	return all, nil
}

// entityRowsForCustomer 把某客户下某类实体读成统一判定记录（只取授权客户下的实体）。
func (s *CustomerDedupeService) entityRowsForCustomer(ctx context.Context, kind, customerID string) ([]dedupeEntityRecord, error) {
	out := make([]dedupeEntityRecord, 0)
	for page := 1; page <= 100; page++ {
		switch kind {
		case DedupeEntityContact:
			rows, _, err := s.contacts.ListByCustomer(ctx, customerID, page, 100)
			if err != nil {
				return nil, fmt.Errorf("读取联系人去重候选失败: %w", err)
			}
			for _, row := range rows {
				out = append(out, dedupeEntityRecord{
					ID: fmt.Sprint(row.ID), Name: row.Name, Kind: kind, CustomerID: row.CustomerID,
					AccountID: row.ChannelAccountID, Role: row.Role, Identity: entityIdentityMap(row.ContactInfo),
				})
			}
			if len(rows) < 100 {
				return out, nil
			}
		case DedupeEntitySupplier:
			rows, _, err := s.suppliers.ListByCustomer(ctx, customerID, page, 100)
			if err != nil {
				return nil, fmt.Errorf("读取供应商去重候选失败: %w", err)
			}
			for _, row := range rows {
				out = append(out, dedupeEntityRecord{
					ID: fmt.Sprint(row.ID), Name: row.Name, Kind: kind, CustomerID: row.CustomerID,
					AccountID: row.ChannelAccountID, Role: row.Role, Identity: entityIdentityMap(row.ContactInfo),
				})
			}
			if len(rows) < 100 {
				return out, nil
			}
		case DedupeEntityServiceProvider:
			rows, _, err := s.providers.ListByCustomer(ctx, customerID, page, 100)
			if err != nil {
				return nil, fmt.Errorf("读取服务商去重候选失败: %w", err)
			}
			for _, row := range rows {
				out = append(out, dedupeEntityRecord{
					ID: fmt.Sprint(row.ID), Name: row.Name, Kind: kind, CustomerID: row.CustomerID,
					AccountID: row.ChannelAccountID, Role: row.Role, Identity: entityIdentityMap(row.ContactInfo),
				})
			}
			if len(rows) < 100 {
				return out, nil
			}
		}
	}
	return out, nil
}

// entityIdentityMap 将实体 ContactInfo(JSONB) 适配成 OneID 规则字段键。
// 无法识别的键忽略，未适配实体由 supportedDedupeEntities 显式标记。
func entityIdentityMap(contactInfo string) map[string]string {
	out := make(map[string]string)
	var raw map[string]any
	if err := json.Unmarshal([]byte(contactInfo), &raw); err != nil {
		return out
	}
	for key, value := range raw {
		text, ok := value.(string)
		if !ok || text == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "phone", "mobile", "手机号", "电话":
			out["phone"] = text
		case "email", "邮箱":
			out["email"] = text
		case "unionid", "union_id":
			out["unionid"] = text
		case "openid", "open_id":
			out["openid"] = text
		case "wechat_open_id", "wechatopenid":
			out["wechat_open_id"] = text
		case "douyin_open_id", "douyin_id":
			out["douyin_open_id"] = text
		case "xiaohongshu_id":
			out["xiaohongshu_id"] = text
		case "external_id", "externalid":
			out["external_id"] = text
		}
	}
	return out
}

func entityRank(kind string) int {
	switch kind {
	case DedupeEntityContact:
		return 1
	case DedupeEntitySupplier:
		return 2
	case DedupeEntityServiceProvider:
		return 3
	}
	return 9
}

func entityIDNumber(id string) int64 {
	var n int64
	for _, r := range id {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// entityHitField 返回命中的具体标识字段，供界面显示「命中字段」。
func entityHitField(set *MergeRuleSet, a, b map[string]string) string {
	if set == nil {
		return ""
	}
	for _, group := range [][]MergeRule{set.BuiltIn, set.Custom} {
		for _, rule := range group {
			if !rule.Enabled {
				continue
			}
			fields := rule.Fields
			if len(fields) == 0 && rule.Field != "" {
				fields = []string{rule.Field}
			}
			op := rule.Op
			if op == "" {
				op = "eq"
			}
			for _, field := range fields {
				va, oka := a[field]
				vb, okb := b[field]
				if oka && okb && ruleScore(va, vb, op) >= 100 {
					return field
				}
			}
		}
	}
	return ""
}

func firstSharedIdentityField(a, b map[string]string) string {
	for _, field := range []string{"phone", "email", "unionid", "openid", "wechat_open_id", "douyin_open_id", "xiaohongshu_id", "external_id"} {
		if a[field] != "" && a[field] == b[field] {
			return field
		}
	}
	return ""
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
func (s *CustomerDedupeService) Merge(ctx context.Context, scope CustomerDataScope, primaryID, secondaryID, operatorName, reason, source string) (*model.Customer, error) {
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
	// FR-002：合并两侧都必须落在调用方的数据权限范围内，禁止越权合并。
	if s.permission != nil {
		if !s.permission.CanAccess(scope, primary) || !s.permission.CanAccess(scope, secondary) {
			return nil, ErrCRMCustomerForbidden
		}
	}

	mergeID := fmt.Sprintf("cm_%d", time.Now().UnixNano())
	// CRM-07：合并前先构建完整影响快照，构建失败即拒绝合并（禁止无快照的「裸合并」）。
	snap, err := s.buildMergeImpactSnapshot(ctx, primary, secondary, mergeID)
	if err != nil {
		return nil, fmt.Errorf("生成合并影响快照失败: %w", err)
	}
	audit := &model.CustomerMerge{
		MergeID:     mergeID,
		OneID:       primary.UnifiedID,
		PrimaryID:   primaryID,
		SecondaryID: secondaryID,
		Source:      source,
		Action:      "manual",
		// 并发版本在事务内合并后回填；此处只放预生成快照占位，事务内覆盖。
		OriginalValues: "",
		RuleName:       fmt.Sprintf("rule_v%d", snap.RuleVersion),
		Reason:         reason,
		OperatorName:   operatorName,
		OperatorID:     snap.Operator.UserID,
	}

	// 快照审计与合并结果同一事务：合并失败或快照写入失败均整体回滚。
	if err := s.customers.WithTransaction(ctx, func(txCtx context.Context) error {
		svc := NewCustomerService()
		if err := svc.MergeCustomers(txCtx, primaryID, secondaryID); err != nil {
			return err
		}
		// 合并会更新主对象 updated_at；以事务内合并后的版本作为并发基线回填，
		// 避免把合并自身的写入误判成「合并后新写入」。
		merged, err := s.customers.GetByID(txCtx, primaryID)
		if err != nil || merged == nil {
			return fmt.Errorf("回填并发版本失败: %w", err)
		}
		snap.ConcurrencyVersion = merged.UpdatedAt.UTC().Format(time.RFC3339Nano)
		original, err := json.Marshal(snap)
		if err != nil {
			return fmt.Errorf("序列化合并影响快照失败: %w", err)
		}
		audit.OriginalValues = string(original)
		return s.mergeRepo.Create(txCtx, audit)
	}); err != nil {
		return nil, fmt.Errorf("合并客户失败: %w", err)
	}

	s.writeAudit(ctx, primaryID, secondaryID, operatorName, reason)
	return primary, nil
}

// ListMergeAudit 查询某客户的合并审计。
func (s *CustomerDedupeService) ListMergeAudit(ctx context.Context, scope CustomerDataScope, customerID string) ([]*model.CustomerMerge, error) {
	// 审计泄露也是数据泄露：先校验目标客户是否在权限范围内。
	if s.permission != nil {
		cust, err := s.scoped.GetByIDScoped(ctx, customerID)
		if err != nil || cust == nil || !s.permission.CanAccess(scope, cust) {
			return nil, ErrCRMCustomerForbidden
		}
	}
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
