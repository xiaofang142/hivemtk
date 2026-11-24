// Package capregistry 平台能力注册表（借鉴 catbus registry/vocab 模式的试点）：
// 「渠道 × 能力」单一事实源，从它生成能力矩阵文档，文档漂移被测试钉死。
package capregistry

import (
	"fmt"
	"sort"
	"strings"
)

// Capability 渠道上的一种能力。
type Capability string

const (
	CapDMReceive Capability = "dm.receive" // 收私信（入站）
	CapDMSend    Capability = "dm.send"    // 发私信（出站）
	CapWebhook   Capability = "webhook"    // 官方回调接入
	CapLeadMine  Capability = "lead.mine"  // 线索挖掘
	CapKBSync    Capability = "kb.sync"    // 知识库外部同步
	CapSMS       Capability = "sms"        // 短信触达
	CapEmail     Capability = "email"      // 邮件触达
)

// Entry 一条注册：平台 + 能力 + 接入方式说明。
type Entry struct {
	Platform string
	Cap      Capability
	Via      string // 接入方式（bridge / webhook / smtp / imasdk…）
	Note     string
	// Cells are action-level defaults associated with this existing registry entry.
	// They describe possible cells; only matching shop evidence can enable a cell.
	Cells []CellDefinition
}

var registry []Entry

// Register 登记（启动/包初始化时调用；重复登记 panic——注册表是契约）。
func Register(e Entry) {
	for _, old := range registry {
		if old.Platform == e.Platform && old.Cap == e.Cap {
			panic(fmt.Sprintf("capregistry: %s/%s 重复登记", e.Platform, e.Cap))
		}
	}
	cells := e.Cells
	e.Cells = make([]CellDefinition, len(cells))
	for i, cell := range cells {
		e.Cells[i] = cloneCellDefinition(cell)
	}
	registry = append(registry, e)
}

// All 返回全部登记（平台排序稳定）。
func All() []Entry {
	out := append([]Entry(nil), registry...)
	for i := range out {
		out[i].Cells = make([]CellDefinition, len(registry[i].Cells))
		for j, cell := range registry[i].Cells {
			out[i].Cells[j] = cloneCellDefinition(cell)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Cap < out[j].Cap
	})
	return out
}

// MatrixMarkdown 生成能力矩阵文档（文档 = 注册表的渲染，禁止手改）。
func MatrixMarkdown() string {
	var b strings.Builder
	b.WriteString("# 渠道能力矩阵\n\n> 本文件由 `internal/capregistry` 生成（capregistry.MatrixMarkdown），")
	b.WriteString("测试钉死与注册表一致，**请勿手改**。改能力先改注册表。\n\n")
	b.WriteString("| 平台 | 能力 | 接入方式 | 说明 |\n|---|---|---|---|\n")
	for _, e := range All() {
		note := strings.ReplaceAll(e.Note, "|", "\\|")
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", e.Platform, e.Cap, e.Via, note)
	}
	b.WriteString("\n## 动作级能力格默认声明\n\n")
	b.WriteString("> Experimental 及其他静态声明只描述候选范围，不授予店铺权限；生产放行需匹配租户/店铺/动作证据。\n\n")
	b.WriteString("| 产品 | 地区 | 动作 | 载体 | 执行模式 | 必需 scope | 状态限制 | 接口版本 | 文档版本 | 默认实验性 |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, cell := range CellDefinitions() {
		scopes := strings.Join(cell.RequiredScopes, ", ")
		limits := strings.Join(cell.StateLimits, ", ")
		experimental := "否"
		if cell.Experimental {
			experimental = "是"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | %s | %s | %s | %s | %s | %s | %s |\n",
			cell.Product, cell.Region, cell.Action, cell.Carrier, cell.ExecutionMode,
			scopes, limits, cell.ContractVersion, cell.DocumentationVersion, experimental)
	}
	b.WriteString("\n## EC-01 提交边界与生产阻塞\n\n")
	b.WriteString("- connector bridge 与 Executor 的写端点验证精确能力格及可信上下文证据；消息生产者 `service.DeliverBridgeOutboundMedia`、AI 回复 `WebhookService.sendOutbound`（延迟入队前）与独立 bridge sender 的 `outboundBridge`（message_hub 写入前）共用 `capregistry.AuthorizeBridgeSubmission` 精确裁决。回复拒绝带不可重试的机器可读原因，零 outbox、quiet-hours 或 send_retry 入队。\n")
	b.WriteString("- 原始 `InboxIngressService.DeliverOutbound` 的 outbound/default 分支写前复核，inbound 保留原逻辑。历史已入队记录在轮询 `ClaimPendingOutbound`、SSE 欠交付查询及 `OutboundDeliveryService.ClaimOutboundForPush`、显式回收 `OutboundReclaimTask.RunOnce` 中重新裁决库中租户、平台、账号与消息类型。文本与图片分别使用 message.text.send / message.image.send，事件平台/账号/类型与库行不一致返回 delivery_binding_mismatch。候选行按同租户 ID 有序分页，每页最多锁定 64 行。写入仅作用于该页已锁定且获准的行。\n")
	b.WriteString("- 消费缺可信映射/证据、撤权、到期、scope、版本、D/I/R 或状态预检失败时保留历史状态、租约、重试预算与成功事实。轮询、SSE 欠交付查询及显式回收可继续处理其他获准行，轮询后续页失败时仍返回已提交的认领。轮询全拒绝返回 HTTP 403、status=blocked 与 reason_code，SSE 记录机器可读原因。SSE 欠交付查询可在较高 ID 已发送后恢复旧行，仅游标兼容路径在拒绝处停止并关闭即时出站推送。系统范围/无租户返回 tenant_scope_invalid。connector 即使有匹配证据也不得用系统范围发起写请求，请求构造与 HTTP 出站均为零，只读路径不变。\n")
	b.WriteString("- `AfterSaleService.Create` 在已配置外部客户端时先于本地售后落库裁决；`HTTPAfterSaleClient.Create` 在 HTTP Do 前独立复核同一策略。退款与退货/换货分别使用 `refund.write`、`aftersale.write`，订单、平台、租户与可信店铺绑定必须一致；未配置外部客户端的本地记录模式保留。\n")
	b.WriteString("- 所有 `bridgeOutboxCapable` 渠道（包括未来新增渠道）均受此门禁约束：精确产品/地区/动作/载体无登记格返回 `unknown_capability`，缺可信映射返回 `capability_mapping_missing`，不沿用旧放行。当前仅 douyin/douyin_shop 的 CN 文本和图片有候选桥接格，彼此不继承；其他桥接产品保持阻塞/待补精确登记与实证。\n")
	b.WriteString("- 当前没有持久化的权威店铺映射/授权证据加载源；`bridge_accounts` 只能证明租户账号归属，`account_id` 不能当 shop_id，售后全局 URL/key/secret 也不证明订单所属店铺。生产代码没有装载 SubmissionBinding 或 CellEvidence，因此这些提交边界当前不开放生产发送；合成证据仅用于本地测试，带 Simulated 的 D/I/R 一律不能生产放行，无用户 HTTP body 注入入口。每次提交重算撤权、到期、scope、版本、D/I/R 与状态预检，不缓存旧许可。\n")
	b.WriteString("- 非桥接直发 sender（wecom/telegram/whatsapp/qq/feishu/dingtalk/weixin，以及 slack/onebot/wecomcs/wecombot/custom）属于不同产品/载体，不会路由到上述 douyin/douyin_shop cells。本切片保留其现有账号/集成配置检查、钉钉 sessionWebhook 域名与到期检查、WhatsApp 会话窗口等渠道防护，不改造这些渠道，也不声称它们已接入 EC-01 动作台账。\n")
	b.WriteString("- 范围限制：本次包含 01-C 历史待发记录的消费时阻塞，不执行历史迁移、不构造生产授权。可信连接/授权加载源、连接续期及真实店铺接通仍是生产依赖。服务启动时停用自动出站回收，记录一次 outbound_reclaim_evidence_source_missing。改单 `direct_api`/`via_erp` 仍由 `OrderChangeService.Execute` 明确拒绝。本矩阵不代表“所有 Submit 已受保护”或 M01 生产验收已完成。\n")
	b.WriteString("\n## EC-01 受控采证与执行绑定\n\n")
	b.WriteString("- CapabilityCollectionService 只接受构造注入的服务端采集器，采证前后检查当前提供方契约，精确匹配来源、动作、scope、连接及凭证修订；连接快照隔离且不含密钥。未配置采集器拒绝，无用户 HTTP 证据上传入口。\n")
	b.WriteString("- CapabilityObservationRepository 事务锁定当前连接并重查撤权、版本、到期和授权/刷新状态，原子保存来源映射与动作证据；来源换店、已撤销映射和写入失败拒绝并回滚；并发同来源只允许一个归属。\n")
	b.WriteString("- LocalCapabilityProofVerifier 按产品与 provenance 精确读取独立认证的本地证明，检查当前提供方契约、身份、模拟标记及有效期；数据库引用本身不构成运行证明。队列锁内不访问外部平台，默认生产不注册证明源。\n")
	b.WriteString("- NewExecutorWithCapabilityBinding 为服务端适配器提供凭证绑定写请求接缝；先裁决、构造请求、校验目的同源与 SSRF、重核实际连接修订后出站，禁止跟随重定向。默认执行器未装配适配器时维持拒绝；本地 fake 测试不代表真实平台授权。\n")
	b.WriteString("\n## EC-01 租户管理员只读摘要\n\n")
	b.WriteString("- `/cap/diagnostics` 提供店铺能力摘要。租户管理员通过 `GET /api/v1/capabilities/cells` 获取登记组合，通过 `GET /api/v1/capabilities/resolve` 按产品、地区、动作、载体及独立填写的店铺 ID 查询。服务端使用登录身份中的租户。\n")
	b.WriteString("- JSON 下载与当前界面使用同一份服务端字段白名单。摘要包含声明与观察到的 scope/版本、连接状态、动作就绪度、状态限制、阻塞原因、负责人和下一步。摘要有效期最长五分钟，并受尚有效的证据到期时间限制。提交时仍重新裁决。\n")
	b.WriteString("- §7.3.5 的京东 POP 售后由商户操作员在京麦官方工作台处理。唯品会由平台处理原生售后和退款，系统观察原案件的处理结果。两个声明格始终拒绝自动提交。\n")
	return b.String()
}
