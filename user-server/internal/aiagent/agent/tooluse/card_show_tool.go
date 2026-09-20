package tooluse

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"
)

// CategoryCard 会话内富卡片工具分类
const CategoryCard ToolCategory = "card"

// CardShowTool 会话内结构化富卡片工具。
// 智能体在对话中通过本工具向用户呈现商品卡 / 订单卡 / 优惠卡 / 通用卡，
// 卡片随最终回复一并下发到 web_embed / Telegram 等渠道，提升信息可读性。
type CardShowTool struct {
	BaseTool
}

// NewCardShowTool 构造会话内卡片工具
func NewCardShowTool() *CardShowTool {
	params := ToolParameters{
		Type: "object",
		Properties: map[string]ToolParam{
			"type": {
				Type:        "string",
				Description: "卡片类型：product(商品卡) / order(订单卡) / promo(优惠活动卡) / generic(通用卡)，缺省 generic",
				Enum:        []string{"product", "order", "promo", "generic"},
			},
			"title": {
				Type:        "string",
				Description: "卡片主标题（必填），如商品名、订单号、活动名称",
			},
			"subtitle": {
				Type:        "string",
				Description: "卡片副标题",
			},
			"description": {
				Type:        "string",
				Description: "卡片描述 / 卖点 / 补充说明",
			},
			"image_url": {
				Type:        "string",
				Description: "卡片主图 URL（商品图 / 活动海报）",
			},
			"thumb_url": {
				Type:        "string",
				Description: "卡片缩略图 URL",
			},
			"fields": {
				Type:        "object",
				Description: "结构化键值对，如价格、规格、物流状态等（值会被转为字符串）",
				Properties: map[string]ToolParam{
					"price":  {Type: "string", Description: "价格"},
					"spec":   {Type: "string", Description: "规格"},
					"status": {Type: "string", Description: "状态/物流进度"},
				},
			},
			"buttons": {
				Type:        "array",
				Description: "卡片动作按钮列表",
				Items: &ToolParam{
					Type: "object",
					Properties: map[string]ToolParam{
						"text":   {Type: "string", Description: "按钮文案（必填）"},
						"url":    {Type: "string", Description: "跳转链接（外链 / 小程序等）"},
						"action": {Type: "string", Description: "前端自定义动作标识，如 copy / buy / detail"},
					},
				},
			},
		},
		Required: []string{"title"},
	}

	return &CardShowTool{
		BaseTool: BaseTool{
			NameVal:        "card.show",
			RiskVal:        RiskLowWrite,
			CategoryVal:    CategoryCard,
			DescriptionVal: "向用户展示一张结构化富卡片（商品卡/订单卡/优惠卡/通用卡）。当需要在对话中以更直观的方式呈现商品、订单、优惠活动或任何结构化信息时调用，提升可读性。需提供 title(必填)，可选项包括 type/subtitle/description/image_url/fields(键值对)/buttons(按钮列表)。",
			ParamsVal:      params,
		},
	}
}

// Execute 构建并返回一张结构化富卡片
func (t *CardShowTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	start := time.Now()
	title := getArgString(args, "title")
	if title == "" {
		return ErrorResult(t.Name(), errors.New("title 为必填项")).withTiming(t.Name(), start),
			errors.New("title 为必填项")
	}

	cardType := getArgString(args, "type")
	if cardType == "" {
		cardType = string(model.CardTypeGeneric)
	}
	switch model.RichCardType(cardType) {
	case model.CardTypeProduct, model.CardTypeOrder, model.CardTypePromo, model.CardTypeGeneric:
	default:
		return ErrorResult(t.Name(), fmt.Errorf("type 非法：%q，必须是 product/order/promo/generic", cardType)).withTiming(t.Name(), start),
			fmt.Errorf("type 非法：%q", cardType)
	}

	card := &model.RichCard{
		Type:        model.RichCardType(cardType),
		Title:       title,
		Subtitle:    getArgString(args, "subtitle"),
		Description: getArgString(args, "description"),
		ImageURL:    getArgString(args, "image_url"),
		ThumbURL:    getArgString(args, "thumb_url"),
	}

	if f, ok := args["fields"].(map[string]any); ok && len(f) > 0 {
		fields := make(map[string]string, len(f))
		for k, v := range f {
			fields[k] = fmt.Sprintf("%v", v)
		}
		card.Fields = fields
	}

	if btns, ok := args["buttons"].([]any); ok {
		for _, b := range btns {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			btn := model.CardButton{
				Text:   getArgString(bm, "text"),
				URL:    getArgString(bm, "url"),
				Action: getArgString(bm, "action"),
			}
			if btn.Text == "" {
				continue
			}
			card.Buttons = append(card.Buttons, btn)
		}
	}

	res := SuccessResult(t.Name(), card)
	res.Card = card
	return res.withTiming(t.Name(), start), nil
}

// BuildCardTools 构造会话内卡片工具集
func BuildCardTools() []Tool {
	return []Tool{
		NewCardShowTool(),
	}
}

// RegisterCardTools 将会话内卡片工具注册到全局工具注册表
func RegisterCardTools(registry *ToolRegistry) {
	for _, tool := range BuildCardTools() {
		if err := registry.Register(tool); err != nil {
			log.Printf("[WARN] register card tool %s failed: %v", tool.Name(), err)
		}
	}
}
