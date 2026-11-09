package tooluse

import (
	"context"
	"errors"
	"fmt"
)

// OrderChangeRequestPort 改单申请单消费方接口（TB-05；实现方在 service/app 装配层）。
//
// 契约：AI 侧只能**创建申请单**（客服侧不写商业订单）；审批/执行是管理面动作，
// 不经 LLM 工具暴露。实现方负责订单镜像校验与能力路由冻结（不编造订单）。
type OrderChangeRequestPort interface {
	CreateChangeRequest(ctx context.Context, in OrderChangeRequestInput) (map[string]any, error)
}

// OrderChangeRequestInput 申请单入参（tooluse 本地形态，与 service 解耦）。
type OrderChangeRequestInput struct {
	Platform   string
	OrderID    string
	CustomerID string
	SessionID  string
	ChangeType string
	Payload    map[string]any
	Reason     string
}

// OrderModifyRequestTool 改单申请工具（高风险写动作，审批装饰器覆盖）。
type OrderModifyRequestTool struct {
	BaseTool
	port OrderChangeRequestPort
}

// NewOrderModifyRequestTool 创建改单申请工具。
func NewOrderModifyRequestTool(port OrderChangeRequestPort) *OrderModifyRequestTool {
	return &OrderModifyRequestTool{
		BaseTool: BaseTool{
			NameVal:     "order.modify_request",
			RiskVal:     RiskHighWrite,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "创建改单申请单（高危，走审批）。用于客户要求修改订单（改地址/换规格/改数量）时登记申请：客服侧只写申请单，" +
				"商业订单的变更发生在审批通过后的外部系统侧（平台 API/ERP/买家自助/人工坐席）。订单必须已同步进订单镜像，" +
				"查不到的订单会被拒绝（不编造）。审批与执行由管理面操作，AI 不直接改电商平台订单。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"platform":    {Type: "string", Description: "电商平台标识（如 taobao/jd/douyin/xianyu），需与订单镜像一致"},
					"order_id":    {Type: "string", Description: "订单号（订单镜像中的外部订单号）"},
					"change_type": {Type: "string", Description: "变更类别：address/sku/quantity/note/other"},
					"payload":     {Type: "object", Description: "变更载荷（如 {\"address\":\"新地址\"}），执行时原样交给获准的外部通道"},
					"reason":      {Type: "string", Description: "客户诉求摘要（不要带手机号等敏感原文）"},
				},
				Required: []string{"platform", "order_id", "change_type", "payload"},
			},
		},
		port: port,
	}
}

// Execute 执行创建申请单。
func (t *OrderModifyRequestTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if t.port == nil {
		return ErrorResult(t.Name(), errors.New("order.modify_request 工具需要 OrderChangeRequestPort 依赖")), errors.New("order.modify_request 工具需要 OrderChangeRequestPort 依赖")
	}
	changeType := getArgString(args, "change_type")
	payload, _ := args["payload"].(map[string]any)
	if changeType == "" || len(payload) == 0 {
		err := fmt.Errorf("change_type 与 payload 为必填")
		return ErrorResult(t.Name(), err), err
	}
	in := OrderChangeRequestInput{
		Platform:   getArgString(args, "platform"),
		OrderID:    getArgString(args, "order_id"),
		CustomerID: getToolContextCustomerID(ctx),
		SessionID:  getToolContextSessionID(ctx),
		ChangeType: changeType,
		Payload:    payload,
		Reason:     getArgString(args, "reason"),
	}
	out, err := t.port.CreateChangeRequest(ctx, in)
	if err != nil {
		return ErrorResult(t.Name(), err), err
	}
	return SuccessResult(t.Name(), out), nil
}

// getToolContextCustomerID / getToolContextSessionID 从工具上下文取会话身份（缺省空串）。
func getToolContextCustomerID(ctx context.Context) string {
	if tc := GetToolContext(ctx); tc != nil {
		return tc.CustomerID
	}
	return ""
}

func getToolContextSessionID(ctx context.Context) string {
	if tc := GetToolContext(ctx); tc != nil {
		return tc.SessionID
	}
	return ""
}
