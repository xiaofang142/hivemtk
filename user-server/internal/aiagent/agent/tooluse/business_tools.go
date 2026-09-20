package tooluse

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/portcontract"
)

// BusinessToolDeps 业务工具依赖
//
// 方法统一走 portcontract 各 Port；未注入的 Port 对应工具返回 "port not injected"。
type BusinessToolDeps struct {
	FollowUp  portcontract.FollowUpPort
	Order     portcontract.OrderPort
	AfterSale portcontract.AfterSalePort
	Logistics portcontract.LogisticsPort
}

// NewBusinessToolDeps 创建业务工具依赖（各 Port 均未注入，需装配层补齐）
func NewBusinessToolDeps() BusinessToolDeps {
	return BusinessToolDeps{}
}

// NewBusinessToolDepsWithDB 创建业务工具依赖（带 DB，用于测试；各 Port 需装配层注入）
func NewBusinessToolDepsWithDB(gdb *gorm.DB) BusinessToolDeps {
	_ = gdb
	return BusinessToolDeps{}
}

// NewBusinessToolDepsWithPorts 创建带 Port 注入的业务工具依赖（推荐）。
//
// 在 main/cmd/api 启动期或测试装配期调用：
//
//	deps := tooluse.NewBusinessToolDepsWithPorts(followUpPort, orderPort, afterSalePort, db.GetDB())
func NewBusinessToolDepsWithPorts(followUp portcontract.FollowUpPort, order portcontract.OrderPort, afterSale portcontract.AfterSalePort, gdb *gorm.DB) BusinessToolDeps {
	d := NewBusinessToolDepsWithDB(gdb)
	d.FollowUp = followUp
	d.Order = order
	d.AfterSale = afterSale
	return d
}

// NewBusinessToolDepsWithLogistics 在 NewBusinessToolDepsWithPorts 基础上注入物流端口。
//
// 调用方：router.Setup() 的 registerAgentBusinessTools（带 LogisticsPortAdapter + CourierClient）。
func NewBusinessToolDepsWithLogistics(followUp portcontract.FollowUpPort, order portcontract.OrderPort, afterSale portcontract.AfterSalePort, logistics portcontract.LogisticsPort, gdb *gorm.DB) BusinessToolDeps {
	d := NewBusinessToolDepsWithPorts(followUp, order, afterSale, gdb)
	d.Logistics = logistics
	return d
}

func (d BusinessToolDeps) followUpPort() portcontract.FollowUpPort {
	return d.FollowUp
}

func (d BusinessToolDeps) orderPort() portcontract.OrderPort {
	return d.Order
}

func (d BusinessToolDeps) afterSalePort() portcontract.AfterSalePort {
	return d.AfterSale
}

func (d BusinessToolDeps) logisticsOrFallback() portcontract.LogisticsPort {
	if d.Logistics != nil {
		return d.Logistics
	}
	return portcontract.NewNoopLogisticsPort()
}

func reminderAnyToMap(v any) map[string]any {
	out := map[string]any{
		"message": "跟进任务已创建",
	}
	if v == nil {
		return out
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return out
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		out["raw"] = v
		return out
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		key := snakeCaseFieldName(field.Name)
		val := rv.Field(i).Interface()
		switch x := val.(type) {
		case time.Time:
			if !x.IsZero() {
				out[key] = x.Format(time.RFC3339)
			}
		case *time.Time:
			if x != nil && !x.IsZero() {
				out[key] = x.Format(time.RFC3339)
			}
		default:
			rvField := rv.Field(i)
			if rvField.Kind() == reflect.String && rvField.Type() != reflect.TypeOf("") {
				out[key] = rvField.String()
				continue
			}
			if s, ok := val.(fmt.Stringer); ok {
				switch rvField.Kind() {
				case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
					if rvField.IsNil() {
						continue
					}
				}
				out[key] = s.String()
				continue
			}
			out[key] = val
		}
		if key == "id" {
			rvField := rv.Field(i)
			if rvField.Kind() == reflect.String {
				out["reminder_id"] = rvField.String()
			} else {
				out["reminder_id"] = val
			}
		}
	}
	return out
}

func snakeCaseFieldName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(name[i-1])
			next := rune(0)
			if i+1 < len(name) {
				next = rune(name[i+1])
			}
			if (prev >= 'a' && prev <= 'z') || (next >= 'a' && next <= 'z') {
				b.WriteByte('_')
			}
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BuildBusinessTools 构造全部 5 个业务工具（不注册到 Registry）
//
// 调用方：BusinessToolProvider.Provide()
func BuildBusinessTools(deps BusinessToolDeps) []Tool {
	return []Tool{
		NewFollowTaskCreateTool(deps),
		NewFollowTaskUpdateTool(deps),
		NewOrderLookupTool(deps),
		NewAfterSaleCreateTool(deps),
		NewAfterSaleQueryTool(deps),
		NewLogisticsTrackTool(deps),
	}
}

// RegisterBusinessTools 注册业务工具到 registry
func RegisterBusinessTools(registry *ToolRegistry, deps BusinessToolDeps) error {
	tools := BuildBusinessTools(deps)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("注册业务工具 %s 失败：%w", t.Name(), err)
		}
	}
	return nil
}

// MustRegisterBusinessTools 注册所有业务工具，出错 panic
func MustRegisterBusinessTools(registry *ToolRegistry, deps BusinessToolDeps) {
	if err := RegisterBusinessTools(registry, deps); err != nil {
		panic(err)
	}
}

// FollowTaskCreateTool 创建跟进任务工具
type FollowTaskCreateTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewFollowTaskCreateTool 创建跟进任务创建工具
func NewFollowTaskCreateTool(deps BusinessToolDeps) *FollowTaskCreateTool {
	return &FollowTaskCreateTool{
		BaseTool: BaseTool{
			NameVal:        "follow_task.create",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "创建客户跟进任务（提醒）。联动客户旅程阶段自动推进 + 销售仪表盘。用于智能体识别意向后自动排程跟进、销售手动安排回访。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"customer_id":    {Type: "string", Description: "客户 ID（必填）"},
					"owner_id":       {Type: "string", Description: "负责销售 ID（可选，默认 'ai_agent'）"},
					"type":           {Type: "string", Description: "跟进类型：first_contact（首次跟进）/ quote_followup（报价后跟进）/ after_sale_care（售后回访）/ repurchase（复购提醒）/ reactivation（沉睡激活）/ birthday（生日祝福）/ custom（自定义）", Enum: []string{"first_contact", "quote_followup", "after_sale_care", "repurchase", "reactivation", "birthday", "custom"}, Default: "custom"},
					"due_in_minutes": {Type: "integer", Description: "多少分钟后到期（默认 1440=24小时）", Default: 1440},
					"title":          {Type: "string", Description: "跟进标题（可选，默认使用 type）"},
					"description":    {Type: "string", Description: "跟进描述（可选）"},
					"priority":       {Type: "integer", Description: "优先级：0=低 / 1=普通 / 2=高 / 3=紧急", Default: 1},
					"sop_name":       {Type: "string", Description: "关联 SOP 名称（可选）"},
					"auto_handle":    {Type: "boolean", Description: "是否由 AI 自动处理（默认 false）", Default: false},
					"channel":        {Type: "string", Description: "触达渠道（可选，如 wecom/sms/email）"},
				},
				Required: []string{"customer_id"},
			},
		},
		deps: deps,
	}
}

// Execute 执行创建跟进任务
//
// 走 portcontract.FollowUpPort；未注入返回 "port not injected"。
func (t *FollowTaskCreateTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"customer_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	followUpPort := t.deps.followUpPort()
	if followUpPort == nil {
		return ErrorResult(t.Name(), errors.New("follow_task.create 工具需要 FollowUpPort 依赖（未装配）")), errors.New("follow_task.create 工具需要 FollowUpPort 依赖（未装配）")
	}

	customerID := getArgString(args, "customer_id")
	ownerID := getArgString(args, "owner_id")
	if ownerID == "" {
		ownerID = "ai_agent"
	}
	rTypeStr := getArgString(args, "type")
	if rTypeStr == "" {
		rTypeStr = "custom"
	}
	switch rTypeStr {
	case "first_contact", "quote_followup", "after_sale_care", "repurchase", "reactivation", "birthday", "custom":
	default:
		return ErrorResult(t.Name(), fmt.Errorf("type 必须是 first_contact/quote_followup/after_sale_care/repurchase/reactivation/birthday/custom，实际：%s", rTypeStr)), fmt.Errorf("type 非法：%s", rTypeStr)
	}

	dueInMinutes, _ := GetIntArg(args, "due_in_minutes")
	if dueInMinutes <= 0 {
		dueInMinutes = 1440
	}
	dueIn := time.Duration(dueInMinutes) * time.Minute

	title := getArgString(args, "title")
	description := getArgString(args, "description")
	_ = getArgString(args, "sop_name")
	_ = getArgString(args, "channel")
	_ = args["auto_handle"]

	priorityVal, priorityOk := GetIntArgSafe(args, "priority")
	priorityStr := "normal"
	if priorityOk {
		switch priorityVal {
		case 0:
			priorityStr = "low"
		case 1:
			priorityStr = "normal"
		case 2:
			priorityStr = "high"
		case 3:
			priorityStr = "urgent"
		default:
			priorityStr = "normal"
		}
	}

	opts := &portcontract.FollowUpScheduleOptions{
		Title:    title,
		Note:     description,
		Priority: priorityStr,
	}

	reminder, err := followUpPort.Schedule(ctx, customerID, ownerID, rTypeStr, dueIn, opts)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}

	reminderMap := reminderAnyToMap(reminder)
	return SuccessResult(t.Name(), reminderMap), nil
}

// FollowTaskUpdateTool 更新跟进任务工具
type FollowTaskUpdateTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewFollowTaskUpdateTool 创建跟进任务更新工具
func NewFollowTaskUpdateTool(deps BusinessToolDeps) *FollowTaskUpdateTool {
	return &FollowTaskUpdateTool{
		BaseTool: BaseTool{
			NameVal:        "follow_task.update",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "更新跟进任务状态：完成（带结果，自动推进客户旅程）/ 取消。完成时必须提供 result 参数，系统将自动推进客户旅程阶段并更新销售仪表盘。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"reminder_id": {Type: "string", Description: "跟进任务 ID（必填）"},
					"action":      {Type: "string", Description: "操作：complete（完成）/ cancel（取消）", Enum: []string{"complete", "cancel"}, Default: "complete"},
					"result":      {Type: "string", Description: "跟进结果（action=complete 时必填）：contacted（已联系）/ interested（有兴趣）/ quoted（已报价）/ converted（已成交）/ rejected（已拒绝）/ lost（已流失）/ no_response（未响应）", Enum: []string{"contacted", "interested", "quoted", "converted", "rejected", "lost", "no_response"}},
					"note":        {Type: "string", Description: "跟进备注（可选）"},
				},
				Required: []string{"reminder_id", "action"},
			},
		},
		deps: deps,
	}
}

// Execute 执行更新跟进任务
//
// 走 portcontract.FollowUpPort；未注入返回 "port not injected"。
func (t *FollowTaskUpdateTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"reminder_id", "action"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	followUpPort := t.deps.followUpPort()
	if followUpPort == nil {
		return ErrorResult(t.Name(), errors.New("follow_task.update 工具需要 FollowUpPort 依赖（未装配）")), errors.New("follow_task.update 工具需要 FollowUpPort 依赖（未装配）")
	}

	reminderID := getArgString(args, "reminder_id")
	if reminderID == "" {
		return ErrorResult(t.Name(), errors.New("reminder_id 不能为空")), errors.New("reminder_id 不能为空")
	}
	action := getArgString(args, "action")
	note := getArgString(args, "note")

	switch action {
	case "complete":
		resultStr := getArgString(args, "result")
		if resultStr == "" {
			return ErrorResult(t.Name(), errors.New("action=complete 时 result 必填")), errors.New("action=complete 时 result 必填")
		}
		switch resultStr {
		case "contacted", "interested", "quoted", "converted", "rejected", "lost", "no_response":
		default:
			return ErrorResult(t.Name(), fmt.Errorf("result 必须是 contacted/interested/quoted/converted/rejected/lost/no_response，实际：%s", resultStr)), fmt.Errorf("result 非法")
		}
		if err := followUpPort.CompleteWithResult(reminderID, resultStr, note); err != nil {
			return ErrorResult(t.Name(), err), err
		}
		targetStage, ok := followUpPort.ResultInfo(resultStr)
		return SuccessResult(t.Name(), map[string]any{
			"reminder_id":    reminderID,
			"action":         "complete",
			"result":         resultStr,
			"note":           note,
			"target_stage":   targetStage,
			"is_positive":    ok,
			"journey_pushed": ok,
			"message":        "跟进已完成，客户旅程已自动推进",
		}), nil

	case "cancel":
		if err := followUpPort.Cancel(reminderID); err != nil {
			return ErrorResult(t.Name(), err), err
		}
		return SuccessResult(t.Name(), map[string]any{
			"reminder_id": reminderID,
			"action":      "cancel",
			"status":      "canceled",
			"message":     "跟进任务已取消",
		}), nil

	default:
		return ErrorResult(t.Name(), fmt.Errorf("action 必须是 complete/cancel，实际：%s", action)), fmt.Errorf("action 必须是 complete/cancel")
	}
}

// OrderLookupTool 订单查询工具
type OrderLookupTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewOrderLookupTool 创建订单查询工具
func NewOrderLookupTool(deps BusinessToolDeps) *OrderLookupTool {
	return &OrderLookupTool{
		BaseTool: BaseTool{
			NameVal:        "order.lookup",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "查询客户订单（只读）。支持按订单号查单笔，或按客户手机/姓名查近期订单，用于回答\"我的订单到哪了/什么状态/物流\"等高频客服问题。订单数据来自外部电商同步镜像，客服系统不创建或变更订单。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"platform": {Type: "string", Description: "电商平台标识（如 taobao/jd），按订单号查时建议提供以缩小范围"},
					"order_id": {Type: "string", Description: "订单号（与 phone/name 二选一；提供则优先按单号精确查）"},
					"phone":    {Type: "string", Description: "客户手机号（不提供 order_id 时按手机查近期订单）"},
					"name":     {Type: "string", Description: "客户姓名（phone 为空时按姓名查）"},
					"limit":    {Type: "integer", Description: "返回订单条数上限（默认 10）", Default: 10},
				},
				Required: []string{},
			},
		},
		deps: deps,
	}
}

// Execute 执行订单查询
func (t *OrderLookupTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	orderPort := t.deps.orderPort()
	if orderPort == nil {
		return ErrorResult(t.Name(), errors.New("order.lookup 工具需要 OrderPort 依赖")), errors.New("order.lookup 工具需要 OrderPort 依赖")
	}

	orderID := getArgString(args, "order_id")
	phone := getArgString(args, "phone")
	name := getArgString(args, "name")
	platform := getArgString(args, "platform")

	if orderID != "" {
		view, err := orderPort.LookupByOrderID(ctx, platform, orderID)
		if err != nil {
			return ErrorResult(t.Name(), err), err
		}
		if view == nil {
			return SuccessResult(t.Name(), map[string]any{"found": false, "message": "未找到该订单"}), nil
		}
		return SuccessResult(t.Name(), map[string]any{"found": true, "order": view}), nil
	}

	if phone == "" && name == "" {
		return ErrorResult(t.Name(), errors.New("order_id 与 phone/name 至少提供一个")), errors.New("order_id 与 phone/name 至少提供一个")
	}
	views, err := orderPort.LookupByCustomer(ctx, phone, name)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	limit, _ := GetIntArg(args, "limit")
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if len(views) > limit {
		views = views[:limit]
	}
	return SuccessResult(t.Name(), map[string]any{
		"count":  len(views),
		"orders": views,
	}), nil
}

// AfterSaleCreateTool 创建售后工具
type AfterSaleCreateTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewAfterSaleCreateTool 创建售后创建工具
func NewAfterSaleCreateTool(deps BusinessToolDeps) *AfterSaleCreateTool {
	return &AfterSaleCreateTool{
		BaseTool: BaseTool{
			NameVal:        "aftersale.create",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "为客户发起售后（退款/退货退款/换货）。这是客服系统对订单唯一允许写入的操作：创建售后请求并回写电商，由电商执行落地，本系统只记录售后单与状态。下单、支付、履约不属于客服职责。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"platform":       {Type: "string", Description: "电商平台标识（如 taobao/jd）"},
					"order_id":       {Type: "string", Description: "关联订单号（必填）"},
					"customer_phone": {Type: "string", Description: "客户手机号（用于回写电商与跟踪）"},
					"customer_name":  {Type: "string", Description: "客户姓名（可选）"},
					"type":           {Type: "string", Description: "售后类型：refund（仅退款）/ return（退货退款）/ exchange（换货）", Enum: []string{"refund", "return", "exchange"}, Default: "refund"},
					"reason":         {Type: "string", Description: "售后原因（可选）"},
					"amount":         {Type: "integer", Description: "售后金额（分），退款/退货时建议提供"},
				},
				Required: []string{"order_id"},
			},
		},
		deps: deps,
	}
}

// Execute 执行创建售后
func (t *AfterSaleCreateTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if err := ValidateRequired(args, []string{"order_id"}); err != nil {
		return ErrorResult(t.Name(), err), err
	}
	afterSalePort := t.deps.afterSalePort()
	if afterSalePort == nil {
		return ErrorResult(t.Name(), errors.New("aftersale.create 工具需要 AfterSalePort 依赖")), errors.New("aftersale.create 工具需要 AfterSalePort 依赖")
	}

	req := &portcontract.AfterSaleRequest{
		Platform:      getArgString(args, "platform"),
		OrderID:       getArgString(args, "order_id"),
		CustomerPhone: getArgString(args, "customer_phone"),
		CustomerName:  getArgString(args, "customer_name"),
		Type:          getArgString(args, "type"),
		Reason:        getArgString(args, "reason"),
	}
	if amount, ok := GetIntArgSafe(args, "amount"); ok {
		req.Amount = int64(amount)
	}
	if req.Type == "" {
		req.Type = portcontract.AfterSaleRefund
	}

	view, err := afterSalePort.Create(ctx, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"after_sale_id": view.ID,
		"order_id":      view.OrderID,
		"type":          view.Type,
		"status":        view.Status,
		"message":       "售后请求已发起，等待电商处理（状态将通过回写刷新）",
	}), nil
}

// AfterSaleQueryTool 查询售后工具
type AfterSaleQueryTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewAfterSaleQueryTool 创建售后查询工具
func NewAfterSaleQueryTool(deps BusinessToolDeps) *AfterSaleQueryTool {
	return &AfterSaleQueryTool{
		BaseTool: BaseTool{
			NameVal:        "aftersale.query",
			RiskVal:        RiskReadonly,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "查询客户售后单进度（按 平台+订单号 或 客户手机）。用于回答\"我的退款到哪了/退货收了吗\"等售后跟进问题。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"platform":       {Type: "string", Description: "电商平台标识（与 order_id 配合时提供）"},
					"order_id":       {Type: "string", Description: "订单号（与 customer_phone 二选一）"},
					"customer_phone": {Type: "string", Description: "客户手机号（不提供 order_id 时按手机查）"},
				},
				Required: []string{},
			},
		},
		deps: deps,
	}
}

// Execute 执行查询售后
func (t *AfterSaleQueryTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	afterSalePort := t.deps.afterSalePort()
	if afterSalePort == nil {
		return ErrorResult(t.Name(), errors.New("aftersale.query 工具需要 AfterSalePort 依赖")), errors.New("aftersale.query 工具需要 AfterSalePort 依赖")
	}
	platform := getArgString(args, "platform")
	orderID := getArgString(args, "order_id")
	phone := getArgString(args, "customer_phone")
	if orderID == "" && phone == "" {
		return ErrorResult(t.Name(), errors.New("order_id 与 customer_phone 至少提供一个")), errors.New("order_id 与 customer_phone 至少提供一个")
	}
	views, err := afterSalePort.Query(ctx, platform, orderID, phone)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), map[string]any{
		"count":       len(views),
		"after_sales": views,
	}), nil
}

// LogisticsTrackTool 物流轨迹查询工具
type LogisticsTrackTool struct {
	BaseTool
	deps BusinessToolDeps
}

// NewLogisticsTrackTool 创建物流轨迹查询工具
func NewLogisticsTrackTool(deps BusinessToolDeps) *LogisticsTrackTool {
	return &LogisticsTrackTool{
		BaseTool: BaseTool{
			NameVal:     "logistics.track",
			RiskVal:     RiskReadonly,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "查询快递/物流轨迹：用于回答“我的快递到哪了 / 什么时候发货 / 物流停在哪了”。" +
				"优先用运单号 tracking_no + 快递公司 carrier 查实时轨迹（凭证需在后台「工具集成配置」填写物流接口 base_url）；" +
				"无运单号时可用 平台 platform + 订单号 order_id 关联本地订单发货状态兜底。" +
				"实时接口未配置时返回本地订单的发货状态与提示，不会报错。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"tracking_no": {Type: "string", Description: "运单号（主查询键，有则优先查实时轨迹）"},
					"carrier":     {Type: "string", Description: "快递公司编码（可选，如 SF/ZTO/YTO/EMS/JD），配合 tracking_no 使用"},
					"platform":    {Type: "string", Description: "电商平台标识（可选，配合 order_id 关联本地订单镜像）"},
					"order_id":    {Type: "string", Description: "订单号（可选，无运单号时按此查本地发货状态）"},
				},
				Required: []string{},
			},
		},
		deps: deps,
	}
}

// Execute 执行物流轨迹查询
func (t *LogisticsTrackTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	logisticsPort := t.deps.logisticsOrFallback()
	if logisticsPort == nil {
		return ErrorResult(t.Name(), errors.New("logistics.track 工具需要 LogisticsPort 依赖")), errors.New("logistics.track 工具需要 LogisticsPort 依赖")
	}
	req := &portcontract.LogisticsTrackRequest{
		TrackingNo: getArgString(args, "tracking_no"),
		Carrier:    getArgString(args, "carrier"),
		Platform:   getArgString(args, "platform"),
		OrderID:    getArgString(args, "order_id"),
	}
	if req.TrackingNo == "" && req.OrderID == "" {
		return ErrorResult(t.Name(), errors.New("tracking_no 与 order_id 至少提供一个")), errors.New("tracking_no 与 order_id 至少提供一个")
	}
	res, err := logisticsPort.Track(ctx, req)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), res), nil
}
