package service

import "hivemtk-user/internal/model"

// 字典种子。
//
// 这些就是 HARDCODE_INVENTORY §3「F 桶 44 个 Go 字典」里最先落地的一批：**状态域**
// 的状态机。它们的共同特征是「描述的是存量行正在用的值域」——历史数据里躺着的
// 状态码必须一直在册，所以种子是事实源而非默认值：DB 里没有这一批时，服务直接读这里。
//
// 与 config_params 的种子不同的一点：那边的 DefaultValue 是「没人配置时的行为」，
// 可以随代码演进调整；这边的种子是**存量数据的值域**，删一项等于让存量行变成
// 非法值。因此字典种子只增不改，要停用一项走 Enabled=false，不删行。
//
// SystemLocked 的含义：该项由代码种子锁定，管理端不能改名或停用（否则代码里的
// 常量与页面上的名字会分叉，而代码是权威的那一侧）。

type dictionarySeed struct {
	// label/description 是「域」这一层的元数据，不是某一项的。
	// 管理端侧栏要显示中文名；只有 parent_code 的话页面上就是一排
	// approval_status / payment_status，既不像给人看的，也无法说明这域管什么。
	label       string
	description string
	items       []model.Dictionary
	trans       []model.DictionaryTransition
}

// dictionarySeedIndex 域 → 种子。按域分层：项与跃迁分开存，
// 避免把「有哪几种状态」和「A 能不能到 B」混成一列 JSON。
var dictionarySeedIndex = map[string]dictionarySeed{}

func registerDictionarySeed(parent, label, description string, items []model.Dictionary, trans []model.DictionaryTransition) {
	for i := range items {
		items[i].ParentCode = parent
	}
	for i := range trans {
		trans[i].ParentCode = parent
	}
	dictionarySeedIndex[parent] = dictionarySeed{
		label: label, description: description, items: items, trans: trans,
	}
}

func lookupDictionarySeed(parent string) ([]model.Dictionary, []model.DictionaryTransition, bool) {
	seed, ok := dictionarySeedIndex[parent]
	if !ok {
		return nil, nil, false
	}
	items := make([]model.Dictionary, len(seed.items))
	copy(items, seed.items)
	trans := make([]model.DictionaryTransition, len(seed.trans))
	copy(trans, seed.trans)
	return items, trans, true
}

func lockedDict(code, name, desc string, sort int) model.Dictionary {
	return model.Dictionary{Code: code, Name: name, Description: desc,
		SortOrder: sort, Enabled: true, SystemLocked: true}
}

func dictTrans(from, to, desc string) model.DictionaryTransition {
	return model.DictionaryTransition{FromCode: from, ToCode: to, Description: desc}
}

func init() {
	// ── approval_status：审批流状态机 ──
	// 与 model/approval_request.go 的常量逐字对应（那是代码侧的事实源）。
	registerDictionarySeed("approval_status",
		"审批状态", "审批请求的生命周期：提交 → 裁决 → 终态。与代码里 model/approval_request.go 的状态常量逐字对应。",
		[]model.Dictionary{
			lockedDict("pending", "待审批", "提交后尚未有人裁决。过期由清扫任务改写，不由人点", 1),
			lockedDict("approved", "已批准", "终态。批准之后流程往哪跑由 checkpoint 侧负责，不在本表再记一份", 2),
			lockedDict("rejected", "已驳回", "终态。需带驳回理由，理由为空时后端返回 400", 3),
			lockedDict("expired", "已过期", "终态。超过 SLA 仍无人裁决，由清扫任务落定", 4),
		},
		[]model.DictionaryTransition{
			dictTrans("pending", "approved", "人裁决：批准"),
			dictTrans("pending", "rejected", "人裁决：驳回（必填理由）"),
			dictTrans("pending", "expired", "系统：超过挂起时限无人裁决"),
			// 终态之间不设跃迁：已裁决的记录不再被任何裁决或清扫改写。
		},
	)

	// ── bad_case_status：坏案例状态机 ──
	registerDictionarySeed("bad_case_status",
		"坏案例状态", "坏案例从发现到闭环的处理阶段。",
		[]model.Dictionary{
			lockedDict("pending", "待判定", "新收集到的坏例，尚未打标", 1),
			lockedDict("labeled", "已判定", "已打上类目与依据，可导出", 2),
			lockedDict("exported", "已导出", "终态。已进导出文件，不再变更", 3),
		},
		[]model.DictionaryTransition{
			dictTrans("pending", "labeled", "打标：同时给类目与依据"),
			dictTrans("labeled", "exported", "导出：批量生成训练数据"),
		},
	)

	// ── human_task_status：人工任务状态机 ──
	// HumanTaskOpenStatuses / HumanTaskTerminalStatuses 的分界也在这里表达：
	// 落在终态集合里的行不再被任何人改写。
	registerDictionarySeed("human_task_status",
		"转人工任务状态", "转人工工单的流转：待接单 → 已分派 → 处理中 → 完成。",
		[]model.Dictionary{
			lockedDict("open", "待处理", "开集。进队列等人领", 1),
			lockedDict("assigned", "已分派", "开集。已指定处理人，尚未完成", 2),
			lockedDict("in_progress", "处理中", "开集。处理人已开始", 3),
			lockedDict("completed", "已完成", "终态", 4),
			lockedDict("cancelled", "已取消", "终态", 5),
		},
		[]model.DictionaryTransition{
			dictTrans("open", "assigned", "分派"),
			dictTrans("open", "cancelled", "取消"),
			dictTrans("assigned", "in_progress", "开始处理"),
			dictTrans("assigned", "completed", "直接完成"),
			dictTrans("assigned", "cancelled", "取消"),
			dictTrans("in_progress", "completed", "完成"),
		},
	)

	// ── opportunity_stage：商机阶段 ──
	registerDictionarySeed("opportunity_stage",
		"商机阶段", "销售漏斗的阶段划分，lead 到 won/lost 为终态。",
		[]model.Dictionary{
			lockedDict("lead", "线索", "尚未确认意向", 1),
			lockedDict("qualified", "已验证", "需求与预算都确认过", 2),
			lockedDict("proposal", "方案中", "已出方案/报价", 3),
			lockedDict("negotiation", "商务谈判", "价格与条款在谈", 4),
			lockedDict("won", "赢单", "终态。成交", 5),
			lockedDict("lost", "丢单", "终态。需记丢单原因", 6),
		},
		[]model.DictionaryTransition{
			dictTrans("lead", "qualified", "确认意向"),
			dictTrans("qualified", "proposal", "出方案"),
			dictTrans("proposal", "negotiation", "进入商务"),
			dictTrans("negotiation", "won", "成交"),
			dictTrans("negotiation", "lost", "丢单"),
			dictTrans("proposal", "lost", "提前丢单"),
			dictTrans("qualified", "lost", "提前丢单"),
		},
	)

	// ── payment_status：支付状态机 ──
	registerDictionarySeed("payment_status",
		"支付状态", "订单收款生命周期：创建 → 支付 → 退款 / 关闭。",
		[]model.Dictionary{
			lockedDict("created", "已创建", "订单已发起，未收钱", 1),
			lockedDict("paid", "已支付", "收到钱，待履约", 2),
			lockedDict("refunded", "已退款", "终态", 3),
			lockedDict("closed", "已关闭", "终态。超时未付由清扫任务落定", 4),
		},
		[]model.DictionaryTransition{
			dictTrans("created", "paid", "支付回调"),
			dictTrans("created", "closed", "超时未付"),
			dictTrans("paid", "refunded", "退款"),
		},
	)

	// ── CRM-19：行业 / 业务类型 / 客户需求分类 ──
	// 与 CRM 编辑、筛选、导入预检共用同一受控选项。种子项非 SystemLocked：
	// 运营可在管理端增补行业/业务类型，停用旧项走 Enabled=false（历史行仍保留展示）。
	//
	// 注意语义：新写入只允许 Enabled 项；字典里完全没有的历史自由文本按
	// 「兼容归一」保留（不静默丢弃），只有命中明确停用码才在预检/写入被拒。
	registerDictionarySeed(CRMDictIndustry,
		"CRM行业", "CRM 客户/公司/线索共用的行业分类，编辑、筛选与导入预检共用。",
		[]model.Dictionary{
			dict("manufacturing", "制造", 1),
			dict("finance", "金融", 2),
			dict("medical", "医疗", 3),
			dict("ecommerce", "电商", 4),
			dict("beauty", "美妆", 5),
			dict("food", "食品", 6),
			dict("electronics", "电子", 7),
			// legacy_retail 为历史迁移码：保留在册但停用，新写入拒绝。
			{Code: "legacy_retail", Name: "零售(旧)", SortOrder: 8, Enabled: false},
		}, nil)

	registerDictionarySeed(CRMDictBusinessType,
		"CRM业务类型", "CRM 商机/客户的业务类型分类。",
		[]model.Dictionary{
			dict("direct_sales", "直销", 1),
			dict("channel_sales", "渠道销售", 2),
			dict("after_sales", "售后服务", 3),
		}, nil)

	registerDictionarySeed(CRMDictRequirementCategory,
		"CRM客户需求分类", "客户需求分类，供线索/商机/画像分层复用。",
		[]model.Dictionary{
			dict("quote", "报价咨询", 1),
			dict("sample", "索样", 2),
			dict("visit", "上门拜访", 3),
			dict("cooperation", "合作洽谈", 4),
		}, nil)
}

// dict 造一条非锁定的启用字典项（运营可增补/停用）。
func dict(code, name string, sort int) model.Dictionary {
	return model.Dictionary{Code: code, Name: name, SortOrder: sort, Enabled: true}
}
