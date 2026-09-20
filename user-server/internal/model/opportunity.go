// opportunity.go 商机领域模型（T-P4-01 / N-1 的第一层）
//
// 这张表今天**不存在**：全仓对商机的表达只有 `clue.is_opportunity` 一个布尔位
// （int64，0/1），既没有阶段、也没有金额与赢率 —— 三方调研 278 行把这条判成
// LTC 域的头号缺口（V-02 实测 model 层零命中）。所以本卡不是"给已有表补字段"，
// 而是把商机这件事第一次放进 schema。
//
// 建表登记走 internal/pkg/db 的 allModels()（本仓生产建表只跑 GORM AutoMigrate，
// 启动期版本化迁移固定 v1.0.0→v1.0.0 是空跑，同 T-P2-01/04/05/06 与 T-P3-01/03 六次的实测；
// 卡面写的 `v3_48_0_opportunity_migration.go` 因此不产出，理由与落点回灌在执行结果里）。
//
// 本卡只交付**列与值域**：跃迁表在 T-P4-03（业务规则随旗子与流程演进，不放 schema），
// 乐观锁版本列在 T-P4-02（它的下家是并发用例，本卡没有并发面）。
package model

import "time"

// Opportunity 商机行（表 opportunities）
//
// 列宽取向（与 approval_request.go / order_draft.go 同一套判据，但**结论不同**，
// 因为这三列的宽度是别的表替我们定下的）：
//
//   - 会被原样抄进 sales_events 的键（customer_id / owner_user_id）：与那里的
//     varchar(64) 同宽。本表比它宽 ⇒ 事件写入那一步才炸，而且是 CreateInBatches
//     整批回滚（T-P1-08 在 tool_call_audits.trace_id 上就是这个形状）；
//   - 长度由渠道/上游决定的标识（one_id：`phone:`/`telegram:` 前缀 + 外部 ID）：text；
//   - 指向本仓自生成主键的列（clue_id → clues.id 是 uuid varchar(36)）：取上游真实宽度；
//   - 取值集合可枚举的列（stage / status）：定宽。
type Opportunity struct {
	// ID 商机业务主键（T-P4-05 的构造器产出，形如 opp_<unixnano>_<seq>）。
	// 刻意不用自增 serial：它会被抄进 sales_events.opportunity_id 与
	// quote/bill 的 opportunity_id，作为跨表引用必须稳定且多实例不撞。
	// 宽度上限 64 由下游列决定，构造器须保证不超（那里是 varchar(64)）。
	ID string `gorm:"type:text;primaryKey" json:"id"`

	// Code 对外编号（人读、可口述、出现在工单与截图里），唯一。
	//
	// 与 ID 分开的理由不是方便，是**引用方向**：ID 被机器抄进别的表，
	// Code 被人抄进话术和 Excel。合成一个键的后果要么是让改编号变成全链路断链，
	// 要么是让机器键去满足人读格式（字符集与长度反过来卡住构造器）。
	//
	// 唯一索引**不带谓词**，与 approval_requests.resume_token 恰好相反：
	// 那里的空值是合法常态（auto-approve 行没有凭证），这里空值不是——
	// "没编号的商机"第二条就会撞死，这是刻意的：编号是对外承诺，不该悄悄积累。
	Code string `gorm:"type:varchar(32);uniqueIndex" json:"code"`

	// CustomerID / OneID 客户身份的两把钥匙，都不是外键。
	//
	// 不建外键的理由与 approval_request 的 subject 同族：本表要引用的 customers
	// 与统一身份来自不同的写路径，且 X3（单商户）之外不再有隔离层，
	// 一条外键只会把"客户还没落 CDP"变成"商机建不出来"。
	// 两列都存：one_id 是跨渠道归一后的稳定键，customer_id 是 CDP 行主键，
	// 二者不总是一一对应（先建商机后归并客户是真实路径）。
	//
	// 索引只给 customer_id（T-P4-02 点名的查询维度），one_id 不建 —— 判据见
	// internal/pkg/db/opportunity_migration_test.go 的索引用例。
	CustomerID string `gorm:"type:varchar(64);index" json:"customer_id"`
	OneID      string `gorm:"type:text" json:"one_id"`

	// ClueID 来源线索（T-P4-05 一键转商机时写入）。可空：手工建的商机没有线索。
	// 不建索引：今天没有"由线索反查商机"的读方；反查由 clues.is_opportunity 承担
	// （那一列的语义本卡不动，向后兼容判据在 T-P4-05 的 AC②）。
	ClueID string `gorm:"type:varchar(36)" json:"clue_id"`

	// Stage 推进位置（值域见 OpportunityStages）。
	//
	// AC① 的落点：本列**只回答"推进到哪一格"**，不回答"这单成了没有"。
	// 两族字面值互斥由用例钉住（OpportunityStageIndex 对任何 status 值都回 -1）。
	// 把 won/lost 塞进 stage 的后果不是难看，是漏斗最后一格与赢单率塌成同一个数
	// —— 那是 C5「禁止把两个条件概率合成一个分数」在字段层的翻版。
	Stage string `gorm:"type:varchar(32);index" json:"stage"`

	// Status 生命周期终局（值域见 OpportunityStatuses），open = 还在跑。
	//
	// 与 stage 分列的第二个理由：两张表的口径各自独立成数。
	// 北极星「闭环完成率 = 完成回款的商机数 / 新建商机数」按 status 算，
	// 漏斗各格计数按 stage 算（T-P4-06），两者必须能同时成立
	// （"停在 proposal 就成了"与"走到 negotiation 才成"是两种合法的商机）。
	//
	// **不建单列索引**（与 stage 相反）：四个取值的大表上，规划器多半仍走顺序扫；
	// 而"只看还在跑的商机"实际形状是 `WHERE owner_user_id = ? AND status = 'open'`
	// —— 由 owner/customer 两列的索引取行、status 只做过滤。
	// 真出现"全站按 status 捞"的读方时，该建的是复合索引，随那张卡一起改这里。
	Status string `gorm:"type:varchar(16)" json:"status"`

	// Amount 预计金额。NUMERIC(12,2)，与 sales_events.amount 同一先例：
	// Go 侧读写仍是 float64（GORM 自动转换），但落库是定点，杜绝累加时的二进制误差。
	// 量程 12,2 与订单草稿的 14,2 **刻意不同**：草稿装的是"单品合计"，
	// 商机装的是"这单值多少钱"；抄成同宽不会报错，只会让越界的那一侧静默截断。
	Amount float64 `gorm:"type:numeric(12,2)" json:"amount"`

	// Currency 币种（ISO 4217 三位码）。
	//
	// 本表**唯一**带 DB 默认值的列，且必须有：金额不带币种就没有语义
	// （12.50 既可能是人民币也可能是美元），而空串不可算。
	// 与 stage/status「不设默认值」并不矛盾 —— 那两列的空值是**可诊断的形状**
	// （值域校验会拒绝它），这一列的空值只会让报表悄悄算错。
	// 默认值同时保证：将来给存量表补这一列时，AutoMigrate 能填上老行（NOT NULL 无默认 = 补列直接失败）。
	Currency string `gorm:"type:varchar(3);default:'CNY'" json:"currency"`

	// WinProbability 赢单概率，C5 三评分里的第三套（"这个商机能不能成"）。
	//
	// 值域 0–1，与 ltc.config 的 `win_probability` 阈值同量程
	// （service.LTCThresholds.WinProbability 默认 0.50），所以比较时不需要换算 ——
	// 换算这一步迟早会有人在阈值侧和读值侧各做一遍，两次不一致就是静默失真。
	// 列型 numeric(5,2) 是卡面给的容器宽度，不是值域承诺；值域由 T-P4-03 的
	// 计算式与校验守住（本卡没有生产者，写一个 CHECK 约束等于替那张卡定实现）。
	//
	// 刻意**不叫** score、也不与 confidence / lead_score 同列共存：
	// 三套评分的命名空间分离是 C5 的硬要求，用例专门拦"往本表再加一列评分"。
	WinProbability float64 `gorm:"type:numeric(5,2)" json:"win_probability"`

	// OwnerUserID 负责销售的标识。
	//
	// 用 string 而不是 uint：仓内销售身份的真源是 SalesProfile.SalesID（字符串），
	// sales_events.owner_id 与 order_drafts.owner_id 也都按字符串存；
	// 换成 uint 会让商机与事件流在"按人聚合"这一步接不上。
	// feishu.go 里的 OwnerUserID uint 是另一族（飞书应用归属），不构成本列的先例。
	//
	// 它是**归属**不是**权限**：分配给谁 ≠ 谁能看谁（X3 单商户，见 AC②）。
	OwnerUserID string `gorm:"type:varchar(64);index" json:"owner_user_id"`

	// ExpectedCloseAt 预计关单日。指针 = 「没定关单日」是常态，
	// 零值时间会被 P8 的逾期/回款预期聚合读成"公元 1 年就该关了单"。
	ExpectedCloseAt *time.Time `json:"expected_close_at,omitempty"`

	// LostReason 输单原因，**只对 status=lost 有意义**。
	// 自由文本而不是码表：原因给人看，而 P8 的归因口径要到 T-P8-05 才定，
	// 现在建一张码表 = 替那张卡写死分类。收口时该不该清空由 T-P4-03 的跃迁表负责。
	LostReason string `gorm:"type:text" json:"lost_reason"`

	// CreatedAt 带索引：T-P4-04 的列表端点按创建时间倒序，是本索引的已命名下家；
	// 「新建商机数」（C6 北极星的分母）也要按它切时间窗。
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Opportunity) TableName() string { return "opportunities" }

// 商机推进阶段值域（AC① 的过程侧）。
//
// 四格来自 LTC 流程里"商机自己能走完的那一段"：资格确认 → 需求确认 → 出方案/报价
// → 商务谈判。报价**发出去**是 P6 的事（quote 域），这里的 proposal 只表示
// "商机已经走到有方案在手"，两件事不冲突也不需要互相知道。
//
// 刻意不引 PG ENUM/NOT NULL：值域改动不该需要 DDL（同 sales_events / order_drafts /
// approval_requests 四处的既有口径），而"没赋值"必须是一个合法的、可被值域校验
// 抓住的形状，不该被 DB 默认值盖成 qualification。
const (
	OpportunityStageQualification  = "qualification"
	OpportunityStageNeedsConfirmed = "needs_confirmed"
	OpportunityStageProposal       = "proposal"
	OpportunityStageNegotiation    = "negotiation"
)

// OpportunityStages 按推进顺序排列；用例逐字比这个切片（顺序变了 = 漏斗各格顺序变了）。
var OpportunityStages = []string{
	OpportunityStageQualification,
	OpportunityStageNeedsConfirmed,
	OpportunityStageProposal,
	OpportunityStageNegotiation,
}

// OpportunityStageIndex 返回阶段在管线上的位置，未知阶段返回 -1。
//
// 第二个返回值刻意不用：调用方拿到 -1 就必须自己决定怎么对待"不认识的阶段"，
// 而 (int, bool) 会被写成 `i, _ :=`，然后 0（第一格）与"没找到"在下游再也分不开。
// 同理不返回 0：0 会被漏斗读成"qualification 多了一条"。
func OpportunityStageIndex(stage string) int {
	for i, s := range OpportunityStages {
		if s == stage {
			return i
		}
	}
	return -1
}

// 商机生命周期终局值域（AC① 的结果侧）。
//
// cancelled 单独一态而不是并入 lost：把"这条根本不该建（误建/重复）"与
// "这单被客户拒了"塞进同一个值，赢率会虚低、丢单归因会掺进一堆不是丢单的行，
// 而后者正是 P8 看板要拿去给销售团队定改进项的数据。
const (
	OpportunityStatusOpen      = "open"
	OpportunityStatusWon       = "won"
	OpportunityStatusLost      = "lost"
	OpportunityStatusCancelled = "cancelled"
)

// OpportunityStatuses 全部合法状态（表驱动用例与越界判据用）。
var OpportunityStatuses = []string{
	OpportunityStatusOpen,
	OpportunityStatusWon,
	OpportunityStatusLost,
	OpportunityStatusCancelled,
}

// OpportunityOutcomes 算"结果"的两个状态。
//
// 与 OpportunityClosed 的差别就是本卡最容易被合并掉的那件事：
// closed 回答"还需不需要有人推进它"（清扫、超时提醒、待办收口都按这个筛），
// outcome 回答"这单最后怎么样了"（赢率、丢单归因按这个筛）。
// 拿 closed 当 outcome 的分母 ⇒ 误建的商机被算成输单；
// 拿 outcome 当 closed ⇒ 取消掉的在跑商机会一直躺在"待推进"里没人收。
var OpportunityOutcomes = []string{
	OpportunityStatusWon,
	OpportunityStatusLost,
}

// OpportunityClosed 报告这条商机是否已经不再推进。
//
// 未知状态一律 false（含空串）：判错的方向必须是"继续管着这行"，
// 而不是把一条在跑的商机当成已收口、从此没有跃迁、没有提醒、也没有人再看它。
// 具体的跃迁合法性在 T-P4-03，这里只回答"收口了没有"这一件跨层都要用的事。
func OpportunityClosed(status string) bool {
	switch status {
	case OpportunityStatusWon, OpportunityStatusLost, OpportunityStatusCancelled:
		return true
	default:
		return false
	}
}

const (
	// OpportunityCurrencyDefault 是建表默认币种。选它而不是空串的理由见 Currency 注释。
	OpportunityCurrencyDefault = "CNY"
	// OpportunityWinProbabilityMax 是赢率的语义上限，与 ltc.config 的 win_probability
	// 阈值同量程（0–1 概率，非百分数）。放这里的唯一用途是让"量程对齐"这件事
	// 在两个包各有一个可断言的名字；真正的计算与校验在 T-P4-03。
	OpportunityWinProbabilityMax = 1.0
)
