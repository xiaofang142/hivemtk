package service

import "hivemtk-user/internal/dto"

const commerceContractVersion = "2026-11-20"
const commerceAPI = "api_adapter"
const commerceWorkbench = "official_ui"
const commerceManaged = "platform_managed"

func commerceProfiles() []dto.CommerceProfileResponse {
	type seed struct{ id, product, region, mode, role string }
	seeds := []seed{
		{"taobao_cn", "taobao", "CN", commerceAPI, "店铺售后"}, {"tmall_cn", "tmall", "CN", commerceAPI, "天猫售后"}, {"pdd_cn", "pdd", "CN", commerceAPI, "售后与费用审批"}, {"douyin_shop_cn", "douyin_shop", "CN", commerceAPI, "抖店订单与售后"},
		{"jd_pop_cn", "jd_pop", "CN", commerceWorkbench, "京麦POP售后"}, {"kuaishou_shop_cn", "kuaishou_shop", "CN", commerceAPI, "快手店铺售后"}, {"xiaohongshu_shop_cn", "xiaohongshu_shop", "CN", commerceWorkbench, "千帆店铺售后"}, {"wechat_shop_cn", "wechat_shop", "CN", commerceAPI, "微信小店售后"}, {"youzan_cn", "youzan", "CN", commerceAPI, "有赞逆向交易"},
		{"1688_cn", "1688", "CN", commerceWorkbench, "1688卖家售后"}, {"meituan_food_cn", "meituan_food", "CN", commerceAPI, "外卖商家"}, {"meituan_retail_cn", "meituan_retail", "CN", commerceWorkbench, "零售售后"}, {"meituan_pharmacy_cn", "meituan_pharmacy", "CN", commerceWorkbench, "医药专员"}, {"eleme_food_cn", "eleme_food", "CN", commerceAPI, "餐饮商家"},
		{"dewu_cn", "dewu", "CN", commerceWorkbench, "得物商家售后"}, {"vip_cn", "vip", "CN", commerceManaged, "唯品会材料与争议"}, {"suning_sop_cn", "suning_sop", "CN", commerceWorkbench, "苏宁SOP售后"}, {"suning_factory_cn", "suning_factory", "CN", commerceWorkbench, "苏宁厂送售后"},
		{"shopify", "shopify", "merchant_configured", commerceAPI, "订单与受保护客户数据"}, {"woocommerce", "woocommerce", "merchant_configured", commerceAPI, "店主与支付网关财务"}, {"tiktok_shop", "tiktok_shop", "shop_configured", commerceAPI, "本地区店铺售后"}, {"shopee", "shopee", "shop_configured", commerceAPI, "当地卖家售后"}, {"lazada", "lazada", "site_configured", commerceAPI, "站点卖家售后"},
		{"amazon_fba", "amazon_fba", "marketplace_configured", commerceManaged, "Seller Central争议"}, {"amazon_mfn", "amazon_mfn", "marketplace_configured", commerceWorkbench, "卖家自履约售后"}, {"ebay", "ebay", "marketplace_configured", commerceAPI, "卖家退款与数字签名"}, {"walmart_us", "walmart_us", "US", commerceAPI, "Marketplace卖家"}, {"aliexpress", "aliexpress", "shop_configured", commerceWorkbench, "卖家纠纷处理"}, {"etsy", "etsy", "shop_configured", commerceWorkbench, "Shop Manager退款"},
	}
	out := make([]dto.CommerceProfileResponse, 0, len(seeds))
	for _, s := range seeds {
		p := dto.CommerceProfileResponse{ID: s.id, Product: s.product, Region: s.region, Mode: s.mode, Role: s.role, Version: commerceContractVersion, LiveStatus: "pending_live_validation", Steps: []string{"核对正确店铺、订单与原生案件", "核对最新状态和已有资金或履约记录", "检查冻结批准范围和当前执行许可", "按官方可操作项完成确认", "登记原生结果、费用与外部引用", "独立复核证据后回告"}}
		actions := []string{"refund", "return.approve", "exchange.ship"}
		switch s.product {
		case "taobao", "tmall", "douyin_shop", "kuaishou_shop", "shopify", "woocommerce":
			actions = append([]string{"address.change"}, actions...)
		case "wechat_shop":
			actions = append(actions, "reship", "refund.difference")
		case "meituan_food", "eleme_food":
			actions = []string{"refund.partial"}
		case "amazon_fba", "vip":
			actions = []string{"managed.observe"}
		case "shopee":
			actions = []string{"return.solution", "refund"}
		case "lazada":
			actions = append(actions, "cancel")
		case "etsy":
			actions = []string{"refund"}
			p.Steps = []string{"Shop Manager → Orders → 选定订单", "订单菜单 Refund → 原因与全额或行金额", "核对 Etsy Payments 或外部支付路径，买家决定 credit/原路", "Review refund → 按批准范围核对金额", "Submit → 登记原生退款引用", "财务复核资金，退款不自动取消订单"}
		case "amazon_mfn":
			p.Steps = []string{"Seller Central → Orders → Manage Returns → Pending Actions", "先核平台自动处理和已发生退款", "按批准案件 Authorize Request 或 Issue Refund", "分别登记 RMA/标签、新履约与退款引用", "复核资金或物流，保留原生证据"}
		}
		for _, act := range actions {
			a := dto.CommerceActionResponse{Action: act, Mode: s.mode, Scope: "commerce." + act, Postcondition: "原生动作状态与独立权威证据一致", RequiredFields: []string{}, Obligations: []string{}}
			switch act {
			case "address.change":
				a.RequiredFields = []string{"recipient", "phone", "address", "province", "city", "district"}
				a.Postcondition = "新地址摘要一致且不再待买家确认"
			case "refund", "refund.partial", "refund.difference":
				a.RequiredFields = []string{"amount_minor", "currency", "case_ref", "payment_path"}
				a.Postcondition = "权威资金交易成功且原币金额一致"
			case "return.approve":
				a.RequiredFields = []string{"case_ref", "warehouse_ref", "line_id", "quantity"}
				a.Postcondition = "原生案件已同意退货，未宣称退款"
			case "exchange.ship", "reship":
				a.RequiredFields = []string{"case_ref", "line_id", "quantity", "sku", "logistics_ref"}
				a.Postcondition = "原生新履约引用成立，未宣称送达"
			case "return.solution":
				a.RequiredFields = []string{"case_ref", "reason"}
				a.Obligations = []string{"available_solution_current"}
			case "cancel":
				a.RequiredFields = []string{"reason"}
				a.Obligations = []string{"cancel_validated"}
				a.Postcondition = "原生取消已核验，资金结果另列"
			case "managed.observe":
				a.RequiredFields = []string{"case_ref"}
				a.Postcondition = "关联平台既有案件与资金或履约，不产生第二笔退款"
			}
			switch s.product {
			case "pdd":
				if act != "address.change" {
					a.Obligations = append(a.Obligations, "pickup_fee_acknowledged", "compensation_acknowledged")
				}
			case "shopify":
				a.Obligations = append(a.Obligations, "tax_impact_reviewed", "fixed_api_version", "refund_idempotency_directive")
			case "woocommerce":
				a.Obligations = append(a.Obligations, "gateway_mode_verified")
			case "ebay":
				a.Obligations = append(a.Obligations, "existing_case_route_verified", "region_signature_verified")
			case "meituan_pharmacy":
				a.Obligations = append(a.Obligations, "pharmacy_specialist", "minimal_health_data")
			case "1688":
				a.Obligations = append(a.Obligations, "seller_view_verified", "credit_terms_reviewed")
			case "kuaishou_shop":
				a.Obligations = append(a.Obligations, "presale_state_verified")
			case "etsy":
				a.Obligations = append(a.Obligations, "buyer_payment_choice_preserved", "refund_not_cancellation")
			}
			if s.product == "douyin_shop" {
				if act == "address.change" {
					a.Method = "order.addressModify"
				} else {
					a.Method = "afterSale.operate"
				}
			}
			if s.product == "pdd" && act == "refund" {
				a.Method = "pdd.refund.agree"
			}
			if s.product == "kuaishou_shop" && act == "address.change" {
				a.Method = "open.order.address.update"
			}
			p.Actions = append(p.Actions, a)
		}
		out = append(out, p)
	}
	return out
}
func commerceProfile(id string) (dto.CommerceProfileResponse, bool) {
	for _, p := range commerceProfiles() {
		if p.ID == id {
			return p, true
		}
	}
	return dto.CommerceProfileResponse{}, false
}
func commerceAction(p dto.CommerceProfileResponse, action string) (dto.CommerceActionResponse, bool) {
	for _, a := range p.Actions {
		if a.Action == action {
			return a, true
		}
	}
	return dto.CommerceActionResponse{}, false
}
