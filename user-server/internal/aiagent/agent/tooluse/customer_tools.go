package tooluse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
)

var errInvalidCustomer = errors.New("至少需要提供一种身份标识（phone/email/wechat_open_id/douyin_open_id/xiaohongshu_id）")

// CustomerToolDeps 客户工具依赖
//
// 仅依赖 portcontract.CustomerPort + CustomerDataStore 端口 + *gorm.DB。
// 工具层不 import service 包以避免反向依赖；错误码用 portcontract.ErrCustomerNotFound sentinel。
type CustomerToolDeps struct {
	Customer     portcontract.CustomerPort
	CustomerRepo CustomerDataStore
	DB           *gorm.DB
}

// NewCustomerToolDeps 创建客户工具依赖（无 Port 注入）
//
// 返回的 deps.Customer = nil；调用方必须在执行工具前通过 NewCustomerToolDepsWithPort
// 注入 Customer Port；否则依赖 Port 的工具会返回 "port not injected" 错误。
func NewCustomerToolDeps() CustomerToolDeps {
	return CustomerToolDeps{}
}

// NewCustomerToolDepsWithDB 创建客户工具依赖（带 DB，用于测试/装配）
//
// 返回的 deps.Customer = nil；如需 Port 注入，请改用 NewCustomerToolDepsWithPort。
func NewCustomerToolDepsWithDB(db *gorm.DB) CustomerToolDeps {
	return CustomerToolDeps{
		DB: db,
	}
}

// NewCustomerToolDepsWithPort 创建带 Customer Port 注入的客户工具依赖（生产装配推荐）。
//
// 在 main/cmd/api 启动期或测试装配期调用：
//
//	customerPort := service.NewCustomerPortAdapter(service.NewCustomerService())
//	deps := tooluse.NewCustomerToolDepsWithPort(customerPort, db.GetDB())
//
// 注意：装配方仍可持有 *service.CustomerService 实例用于构造 Port Adapter，
// 但工具层自身不再直接 import service 包。
func NewCustomerToolDepsWithPort(customer portcontract.CustomerPort, db *gorm.DB, store CustomerDataStore) CustomerToolDeps {
	d := NewCustomerToolDepsWithDB(db)
	d.Customer = customer
	d.CustomerRepo = store
	return d
}

// BuildCustomerTools 构造全部 8 个客户工具（不注册到 Registry）
//
// 调用方：CustomerToolProvider.Provide()
func BuildCustomerTools(deps CustomerToolDeps) []Tool {
	return []Tool{
		NewCustomerSearchTool(deps),
		NewCustomerGetTool(deps),
		NewCustomerCreateTool(deps),
		NewCustomerUpdateTool(deps),
		NewCustomerMergeTool(deps),
		NewCustomerAddTagTool(deps),
		NewCustomerRemoveTagTool(deps),
		NewCustomerSegmentTool(deps),
	}
}

// RegisterCustomerTools 注册所有 8 个客户工具到 registry
func RegisterCustomerTools(registry *ToolRegistry, deps CustomerToolDeps) error {
	tools := BuildCustomerTools(deps)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("注册客户工具 %s 失败：%w", t.Name(), err)
		}
	}
	return nil
}

// MustRegisterCustomerTools 注册所有客户工具，出错 panic
func MustRegisterCustomerTools(registry *ToolRegistry, deps CustomerToolDeps) {
	if err := RegisterCustomerTools(registry, deps); err != nil {
		panic(err)
	}
}

// CustomerSearchTool 按身份标识搜索客户
type CustomerSearchTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerSearchTool 创建搜索客户工具
func NewCustomerSearchTool(deps CustomerToolDeps) *CustomerSearchTool {
	return &CustomerSearchTool{
		BaseTool: BaseTool{
			NameVal:        "customer.search",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "按身份标识（phone/email/wechat_open_id/douyin_open_id/xiaohongshu_id）搜索客户。返回匹配的客户列表。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"phone":          {Type: "string", Description: "手机号"},
					"email":          {Type: "string", Description: "邮箱"},
					"wechat_open_id": {Type: "string", Description: "微信 OpenID"},
					"douyin_open_id": {Type: "string", Description: "抖音 OpenID"},
					"xiaohongshu_id": {Type: "string", Description: "小红书 ID"},
					"limit":          {Type: "integer", Description: "返回数量上限（默认 20，最大 100）", Default: 20},
				},
			},
		},
		deps: deps,
	}
}

// Execute 执行搜索
func (t *CustomerSearchTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	phone, _ := GetStringArg(args, "phone")
	email, _ := GetStringArg(args, "email")
	wechat, _ := GetStringArg(args, "wechat_open_id")
	douyin, _ := GetStringArg(args, "douyin_open_id")
	xhs, _ := GetStringArg(args, "xiaohongshu_id")
	limit, _ := GetIntArg(args, "limit")
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	if phone == "" && email == "" && wechat == "" && douyin == "" && xhs == "" {
		return ErrorResult(t.Name(), errors.New("至少需要提供一个身份标识参数")), errors.New("至少需要提供一个身份标识参数")
	}

	customer, err := t.deps.CustomerRepo.FindByIdentity(ctx, phone, email, wechat, douyin)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	results := make([]*model.Customer, 0, 1)
	if customer != nil && customer.ID != "" {
		results = append(results, customer)
	}

	if xhs != "" && t.deps.CustomerRepo != nil {
		xhsCustomer, err := t.deps.CustomerRepo.GetByXiaohongshuID(ctx, xhs)
		if err == nil && xhsCustomer != nil && xhsCustomer.ID != "" {
			dupe := false
			for _, c := range results {
				if c.ID == xhsCustomer.ID {
					dupe = true
					break
				}
			}
			if !dupe {
				results = append(results, xhsCustomer)
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return SuccessResult(t.Name(), map[string]any{
		"customers": results,
		"total":     len(results),
	}), nil
}

// CustomerGetTool 获取客户详情（含 360 视图）
type CustomerGetTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerGetTool 创建获取客户详情工具
func NewCustomerGetTool(deps CustomerToolDeps) *CustomerGetTool {
	return &CustomerGetTool{
		BaseTool: BaseTool{
			NameVal:        "customer.get",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "按客户 ID 获取客户详情，包含基本信息、最近事件和标签。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"customer_id": {Type: "string", Description: "客户 ID"},
					"include_360": {Type: "boolean", Description: "是否包含 360 视图数据（事件、标签）", Default: true},
				},
				Required: []string{"customer_id"},
			},
		},
		deps: deps,
	}
}

// Execute 执行获取
//
// 仅走 portcontract.CustomerPort。
func (t *CustomerGetTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"customer_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	customerID, _ := GetStringArg(args, "customer_id")
	include360 := true
	if v, ok := args["include_360"].(bool); ok {
		include360 = v
	}

	if include360 {
		profile, err := t.fetchCustomerProfile(ctx, customerID)
		if err != nil {
			if errors.Is(err, portcontract.ErrCustomerNotFound) {
				return ErrorResult(t.Name(), err), err
			}
			return ErrorResult(t.Name(), err), err
		}
		return SuccessResult(t.Name(), profile), nil
	}

	customer, err := t.deps.CustomerRepo.GetByID(ctx, customerID)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	if customer == nil || customer.ID == "" {
		return ErrorResult(t.Name(), portcontract.ErrCustomerNotFound), portcontract.ErrCustomerNotFound
	}
	return SuccessResult(t.Name(), customer), nil
}

func (t *CustomerGetTool) fetchCustomerProfile(ctx context.Context, customerID string) (*portcontract.CustomerProfileView, error) {
	_ = ctx
	if t.deps.Customer == nil {
		return nil, errors.New("CustomerPort not injected")
	}
	return t.deps.Customer.GetCustomerProfile(customerID)
}

// CustomerCreateTool 创建客户
type CustomerCreateTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerCreateTool 创建客户工具
func NewCustomerCreateTool(deps CustomerToolDeps) *CustomerCreateTool {
	return &CustomerCreateTool{
		BaseTool: BaseTool{
			NameVal:        "customer.create",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "创建新客户（如果身份标识已存在则更新）。至少需要提供一种身份标识。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"phone":          {Type: "string", Description: "手机号"},
					"email":          {Type: "string", Description: "邮箱"},
					"wechat_open_id": {Type: "string", Description: "微信 OpenID"},
					"douyin_open_id": {Type: "string", Description: "抖音 OpenID"},
					"xiaohongshu_id": {Type: "string", Description: "小红书 ID"},
				},
			},
		},
		deps: deps,
	}
}

// Execute 执行创建
func (t *CustomerCreateTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	identity := &CustomerIdentity{
		Phone:         getArgString(args, "phone"),
		Email:         getArgString(args, "email"),
		WechatOpenID:  getArgString(args, "wechat_open_id"),
		DouyinOpenID:  getArgString(args, "douyin_open_id"),
		XiaohongshuID: getArgString(args, "xiaohongshu_id"),
	}
	if identity.Phone == "" && identity.Email == "" && identity.WechatOpenID == "" && identity.DouyinOpenID == "" && identity.XiaohongshuID == "" {
		return ErrorResult(t.Name(), errInvalidCustomer), errInvalidCustomer
	}
	if t.deps.Customer == nil {
		return ErrorResult(t.Name(), errors.New("CustomerPort not injected")), errors.New("CustomerPort not injected")
	}

	customer, err := t.deps.Customer.CreateOrUpdate(identity)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), customer), nil
}

// CustomerUpdateTool 更新客户基本信息
type CustomerUpdateTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerUpdateTool 创建更新客户工具
func NewCustomerUpdateTool(deps CustomerToolDeps) *CustomerUpdateTool {
	return &CustomerUpdateTool{
		BaseTool: BaseTool{
			NameVal:        "customer.update",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "更新客户基本信息（phone/email/wechat/douyin/xiaohongshu）。仅更新非空字段。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"customer_id":    {Type: "string", Description: "客户 ID"},
					"phone":          {Type: "string", Description: "新手机号（可选）"},
					"email":          {Type: "string", Description: "新邮箱（可选）"},
					"wechat_open_id": {Type: "string", Description: "新微信 OpenID（可选）"},
					"douyin_open_id": {Type: "string", Description: "新抖音 OpenID（可选）"},
					"xiaohongshu_id": {Type: "string", Description: "新小红书 ID（可选）"},
				},
				Required: []string{"customer_id"},
			},
		},
		deps: deps,
	}
}

// Execute 执行更新
func (t *CustomerUpdateTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"customer_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	customerID, _ := GetStringArg(args, "customer_id")

	customer, err := t.deps.CustomerRepo.GetByID(ctx, customerID)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	if customer == nil || customer.ID == "" {
		return ErrorResult(t.Name(), portcontract.ErrCustomerNotFound), portcontract.ErrCustomerNotFound
	}

	if v := getArgString(args, "phone"); v != "" {
		customer.Phone = v
	}
	if v := getArgString(args, "email"); v != "" {
		customer.Email = v
	}
	if v := getArgString(args, "wechat_open_id"); v != "" {
		customer.WechatOpenID = v
	}
	if v := getArgString(args, "douyin_open_id"); v != "" {
		customer.DouyinOpenID = v
	}
	if v := getArgString(args, "xiaohongshu_id"); v != "" {
		customer.XiaohongshuID = v
	}

	customer.UnifiedID = model.GenerateCustomerUnifiedID(customer)

	if err := t.deps.CustomerRepo.Update(ctx, customer); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), customer), nil
}

// CustomerMergeTool 合并两个客户（OneID）
type CustomerMergeTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerMergeTool 创建合并客户工具
func NewCustomerMergeTool(deps CustomerToolDeps) *CustomerMergeTool {
	return &CustomerMergeTool{
		BaseTool: BaseTool{
			NameVal:        "customer.merge",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "合并两个客户（将 secondary 合并到 primary）。secondary 的身份标识和标签会合并到 primary，secondary 会被删除。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"primary_id":   {Type: "string", Description: "主要客户 ID（保留）"},
					"secondary_id": {Type: "string", Description: "次要客户 ID（合并后被删除）"},
				},
				Required: []string{"primary_id", "secondary_id"},
			},
		},
		deps: deps,
	}
}

// Execute 执行合并
//
// 仅走 portcontract.CustomerPort。
func (t *CustomerMergeTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"primary_id", "secondary_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	primaryID, _ := GetStringArg(args, "primary_id")
	secondaryID, _ := GetStringArg(args, "secondary_id")

	if t.deps.Customer == nil {
		return ErrorResult(t.Name(), errors.New("CustomerPort not injected")), errors.New("CustomerPort not injected")
	}
	if err := t.deps.Customer.MergeCustomers(primaryID, secondaryID); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"primary_id":   primaryID,
		"secondary_id": secondaryID,
		"merged":       true,
	}), nil
}

// CustomerAddTagTool 给客户添加标签
type CustomerAddTagTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerAddTagTool 创建添加标签工具
func NewCustomerAddTagTool(deps CustomerToolDeps) *CustomerAddTagTool {
	return &CustomerAddTagTool{
		BaseTool: BaseTool{
			NameVal:        "customer.add_tag",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "给客户添加一个或多个标签。已存在的标签会被自动去重。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"customer_id": {
						Type:        "string",
						Description: "客户 ID",
					},
					"tags": {
						Type:        "array",
						Description: "要添加的标签数组",
						Items:       &ToolParam{Type: "string"},
					},
				},
				Required: []string{"customer_id", "tags"},
			},
		},
		deps: deps,
	}
}

// Execute 执行添加标签
//
// 仅走 portcontract.CustomerPort。
func (t *CustomerAddTagTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"customer_id", "tags"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	customerID, _ := GetStringArg(args, "customer_id")
	tags := getArgStringSlice(args, "tags")
	if len(tags) == 0 {
		return ErrorResult(t.Name(), errors.New("tags 不能为空")), errors.New("tags 不能为空")
	}

	if t.deps.Customer == nil {
		return ErrorResult(t.Name(), errors.New("CustomerPort not injected")), errors.New("CustomerPort not injected")
	}
	if err := t.deps.Customer.AddTags(customerID, tags); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	customer, _ := t.deps.CustomerRepo.GetByID(ctx, customerID)
	var currentTags []string
	if customer != nil {
		currentTags = model.GetCustomerTags(customer)
	}
	return SuccessResult(t.Name(), map[string]any{
		"customer_id":  customerID,
		"added_tags":   tags,
		"current_tags": currentTags,
	}), nil
}

// CustomerRemoveTagTool 移除客户标签
type CustomerRemoveTagTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerRemoveTagTool 创建移除标签工具
func NewCustomerRemoveTagTool(deps CustomerToolDeps) *CustomerRemoveTagTool {
	return &CustomerRemoveTagTool{
		BaseTool: BaseTool{
			NameVal:        "customer.remove_tag",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "从客户身上移除一个或多个标签。不存在的标签会被忽略。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"customer_id": {
						Type:        "string",
						Description: "客户 ID",
					},
					"tags": {
						Type:        "array",
						Description: "要移除的标签数组",
						Items:       &ToolParam{Type: "string"},
					},
				},
				Required: []string{"customer_id", "tags"},
			},
		},
		deps: deps,
	}
}

// Execute 执行移除标签
//
// 仅走 portcontract.CustomerPort。
func (t *CustomerRemoveTagTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"customer_id", "tags"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	customerID, _ := GetStringArg(args, "customer_id")
	tags := getArgStringSlice(args, "tags")
	if len(tags) == 0 {
		return ErrorResult(t.Name(), errors.New("tags 不能为空")), errors.New("tags 不能为空")
	}

	if t.deps.Customer == nil {
		return ErrorResult(t.Name(), errors.New("CustomerPort not injected")), errors.New("CustomerPort not injected")
	}
	if err := t.deps.Customer.RemoveTags(customerID, tags); err != nil {
		return ErrorResult(t.Name(), err), err
	}

	customer, _ := t.deps.CustomerRepo.GetByID(ctx, customerID)
	var currentTags []string
	if customer != nil {
		currentTags = model.GetCustomerTags(customer)
	}
	return SuccessResult(t.Name(), map[string]any{
		"customer_id":  customerID,
		"removed_tags": tags,
		"current_tags": currentTags,
	}), nil
}

// CustomerSegmentTool 按条件分群客户
type CustomerSegmentTool struct {
	BaseTool
	deps CustomerToolDeps
}

// NewCustomerSegmentTool 创建客户分群工具
func NewCustomerSegmentTool(deps CustomerToolDeps) *CustomerSegmentTool {
	return &CustomerSegmentTool{
		BaseTool: BaseTool{
			NameVal:        "customer.segment",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryCustomer,
			DescriptionVal: "按标签、RFM 分数、流失风险等条件筛选客户。返回匹配的客户列表和总数。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"tag": {
						Type:        "string",
						Description: "标签筛选（包含此标签的客户）",
					},
					"rfm_min": {
						Type:        "integer",
						Description: "RFM 分数下限（含）",
					},
					"rfm_max": {
						Type:        "integer",
						Description: "RFM 分数上限（含）",
					},
					"churn_risk": {
						Type:        "string",
						Description: "流失风险等级",
						Enum:        []string{"low", "medium", "high"},
					},
					"created_after": {
						Type:        "string",
						Description: "创建时间下限（RFC3339 格式，如 2026-01-01T00:00:00Z）",
					},
					"created_before": {
						Type:        "string",
						Description: "创建时间上限（RFC3339 格式）",
					},
					"page": {
						Type:        "integer",
						Description: "页码（默认 1）",
						Default:     1,
					},
					"page_size": {
						Type:        "integer",
						Description: "每页数量（默认 20，最大 100）",
						Default:     20,
					},
				},
			},
		},
		deps: deps,
	}
}

// Execute 执行分群查询
//
// 通过 repository.CustomerRepository.SearchByFilter 实现分群查询。
// t.deps.DB 字段保留仅为向后兼容（不再使用），新代码应使用 CustomerRepo。
func (t *CustomerSegmentTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.deps.CustomerRepo == nil {
		return ErrorResult(t.Name(), errors.New("segment 工具需要 CustomerRepo 依赖")), errors.New("segment 工具需要 CustomerRepo 依赖")
	}

	page, _ := GetIntArg(args, "page")
	if page <= 0 {
		page = 1
	}
	pageSize, _ := GetIntArg(args, "page_size")
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	filter := CustomerSegmentFilter{
		Page:     page,
		PageSize: pageSize,
	}
	if tag := getArgString(args, "tag"); tag != "" {
		filter.Tag = tag
	}
	if rfmMin, ok := GetIntArgSafe(args, "rfm_min"); ok {
		filter.RFMMin = rfmMin
		filter.HasRFMMin = true
	}
	if rfmMax, ok := GetIntArgSafe(args, "rfm_max"); ok {
		filter.RFMMax = rfmMax
		filter.HasRFMMax = true
	}
	if risk := getArgString(args, "churn_risk"); risk != "" {
		if risk != "low" && risk != "medium" && risk != "high" {
			return ErrorResult(t.Name(), errors.New("churn_risk 必须是 low/medium/high")), errors.New("churn_risk 必须是 low/medium/high")
		}
		filter.ChurnRisk = risk
	}
	if createdAfter := getArgString(args, "created_after"); createdAfter != "" {
		filter.CreatedAfter = createdAfter
	}
	if createdBefore := getArgString(args, "created_before"); createdBefore != "" {
		filter.CreatedBefore = createdBefore
	}

	customers, total, err := t.deps.CustomerRepo.SearchByFilter(ctx, filter)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	return SuccessResult(t.Name(), map[string]any{
		"customers":   customers,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	}), nil
}

func getArgString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getArgStringSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if arr, ok := v.([]string); ok {
		return arr
	}
	if s, ok := v.(string); ok && s != "" {
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return arr
		}
	}
	return nil
}

// GetIntArgSafe 安全获取 int 参数（不存在返回 0, false）
// 与 GetIntArg 的区别：不存在时不报错
func GetIntArgSafe(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
