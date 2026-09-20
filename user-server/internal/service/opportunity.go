// opportunity.go 商机服务层（T-P4-03 / N-1 的第三层）
//
// 本层是 opportunities 表上**唯一**的业务判断处：跃迁合法性、值域校验、赢率计算。
// 这个位置不是选出来的，是 T-P4-01/02 两层各自让出来之后剩下的那一格 ——
// 仓储刻意不校验（`TestOpportunityRepo_DoesNotValidate` 钉着"越界值原样落库"），
// 模型刻意不放跃迁表（业务规则要随旗子演进，不该需要 DDL）。
// 所以"越界值今天能进库"是本层的**前提**，不是漏洞：守门这件事从这里开始才有人做。
//
// ─────────────────────────────────────────────────────────────────────────────
// 赢率计算式与它的依据（AC② / C5）
//
// 式子（唯一的一条，输入只有这一行自己的事实）：
//
//	p = round₂( base(阶段) − 无归属(0.10) − 已过预计关单日未收口(0.15) )，下限 0.05
//	base：qualification 0.10 / needs_confirmed 0.30 / proposal 0.55 / negotiation 0.75
//
// **能被证明的部分只有"形状"**：本层的跃迁规则要求逐格向前（见下方机器规则），
// 也就是"通过第 k 格"是赢单的必要条件。必要条件给出集合的嵌套：
//
//	{赢单} ⊆ {到达第 k+1 格} ⊆ {到达第 k 格}
//	⇒ P(赢 | 到第 k 格) = P(赢) / P(到第 k 格)，分母随 k 变小 ⇒ 该值随 k 单调不减
//
// 这条推导只锁死**递增**，锁不死任何具体数字。所以四个 base 是显式登记的**先验刻度**，
// 不是标定过的概率：现仓里没有一行"已收口的商机"可以拿去回标（opportunities 今日零生产
// 写入方），把它们当成标定值写进任何报表都是假的。等 P8 攒够 outcome 行，回标是改这四个数，
// 式子的形状不用动 —— 用例里的严格递增只在"无任何惩罚项"那一档断，就是这个原因。
//
// 两项惩罚同理只声称方向：无归属、已逾期，各自都让"能不能成"下降；
// 但**不做**"逾期越久扣越多"的梯度 —— 那需要一条"逾期时长→概率"的曲线，
// 而这条曲线没有任何数据支撑，比常数先验更假。
//
// 下限 0.05 而不是 0：0 会被下游读成"这单绝不可能成"，于是所有跟进动作都被这个读数劝退；
// 一条没人推进、又过了关单日的资格期商机需要的是"看一眼"，不是"判死刑"。
// 上限不写进代码：四个 base 都 <1 且惩罚只减不加，值域上界由常量表本身保证
// （用例 `TestOpportunityWinProbabilityMonotoneInStage` 逐档断 p<1），
// 在这里再 clamp 一次是给不可能发生的分支写代码。1.0 也不许由本式产出 ——
// 那是"结果已知"的取值，属于 status，不属于预测。
//
// **禁止跨域乘算，且这里没有统计学依据**：confidence 以"这一问一答"为条件、
// lead_score 以"这个客户"为条件、win_probability 以"这一单自身的阶段与事实"为条件。
// 条件集不同的两个概率相乘不对应任何事件；要用它们，得显式建
// P(赢 | 阶段, lead_score, confidence) 并**用数据拟合** —— 而拟合需要的历史行还没有。
// 未标定却已分域的三个量，相乘只会得到一个既不可解释也不可回标的数。
// 所以本层的输入结构里没有它们（字段名集合由用例钉住），
// 而"到没到运营阈值"交给读方拿 `ltc.config` 自己比（阈值默认 0.50 与 base 表同量程，
// 本式跨过 0.50 的那一格是 proposal —— 即"方案到了客户手上才算过半"，这是刻意的对齐，
// 且阈值可配，改线不需要改代码）。
//
// 落库前先把值舍到列的两位小数：`numeric(5,2)` 迟早要舍，而不在这里舍的话，
// 返回给调用方的那份与库里的那份就是两个数（0.5999999999999999 vs 0.60），
// 阈值比较读哪一份取决于代码先撞上谁。金额那一格是同一件事的第二份，一并舍。
//
// ─────────────────────────────────────────────────────────────────────────────
// 跃迁机器规则（AC①）
//
// 阶段：**向前只一步，向后可任意，同格不算跃迁**。
// 向前一步是因为每一格都是闸门（跳格 = 某一格的证据从未产生过）；
// 向后不限步数是因为禁回退的出口只剩"作废重建"，而那会把 C6 北极星的分母
// （新建商机数）灌满修数据的假商机 —— 代价由指标承担，不由销售承担。
// 同格改写必须拒：它什么也没改，却会把 version 涨一格，
// 而 version 的语义是"被成功改过几次"（T-P4-02 的 CAS 判据就建在这句话上）。
//
// 状态：在跑的行可以去 lost / cancelled（销售侧决定）与 won（**只有回款完成能落到它**）；
// lost 可以回到 open（"当时没买"是会被时间推翻的事实）；won 与 cancelled 是死态。
// won 不可回退的理由不是难看，是钱已经真实发生 —— 改它等于改史，
// 该走 P7 的冲销；cancelled 不可回退是因为它的含义是"这根本不该是一条商机"，
// 回退它等于把误建的行重新投进漏斗。
//
// "唯一入口"（AC③）靠 (来源, 起点, 终点) 三元表实现，而不是靠"源码里只有一处赋值"：
// 新的导出方法想写 won，必须挑一个 cause，而放行 won 的只有"回款完成"。
// 复用内部通道也绕不过这张表。外部保证另有一条：架构门 [1/10] 禁止 controller 直引
// repository ⇒ 生产路径上能改这一行的只有本层。
//
// 终态之后**不再重算赢率**：那格留下的是"当时预测它能不能成"，
// P8 的校准分析要的正是这个预测与结果的差；收口时把它改成本式的输出，
// 等于用已知结果污染预测，Brier 分数会一路向好而模型其实什么都没学到。
//
// 幂等只给"事实的重复上报"，不给"跃迁的重复请求"：同一条回款完成第二次到达要成功
// 且不产生第二次改写（否则 P7 的 webhook 会为了一条早已赢单的行一直重试）；
// 而"再点一次推进到当前阶段"是请求，改不动就该报错让人去看。
//
// 返回值一律是**回读**的那一份：本层每次改写都是 读-校验-写-读。多一次 SELECT 换来的是
// "调用方拿到的 version 可以直接用作下一刀的期望版本"。就地 +1 那个便宜不能占：
// updated_at 由仓储落，就地改会让返回体带一个谎时间，而"多久没动过"是待办与提醒的排序键。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 赢率计算式的常量表（依据见文件头）。
const (
	opportunityWinProbQualificationBase  = 0.10
	opportunityWinProbNeedsConfirmedBase = 0.30
	opportunityWinProbProposalBase       = 0.55
	opportunityWinProbNegotiationBase    = 0.75

	// 两项惩罚：各自只声称"方向"，不声称幅度之间可比。
	opportunityWinProbPenaltyNoOwner = 0.10
	opportunityWinProbPenaltyOverdue = 0.15
	opportunityWinProbFloor          = 0.05
	opportunityWinProbDecimalPlaces  = 2 // numeric(5,2)
	opportunityAmountDecimalPlaces   = 2 // numeric(12,2)
	opportunityAmountMax             = 1e10
	opportunityOwnerMaxLen           = 64  // 与 varchar(64) 同宽，越界在事件抄写侧才炸
	opportunityLostReasonMaxLen      = 512 // 自由文本也要有界：原因给人读
	opportunityCurrencyLen           = 3
)

// 服务层错误。四个各自对应一种**修法不同**的失败，合并任何一个都会把排查引向别处：
// 入参不合法 = 改请求；跃迁不允许 = 改流程认知；已收口 = 先回退状态；
// 现值读不懂 = 数据坏了，既不该重发请求，也不该改状态机。
var (
	// ErrOpportunityInputInvalid 调用方给的值不在值域内（400 面）。
	ErrOpportunityInputInvalid = errors.New("opportunity service: 入参不合法")

	// ErrOpportunityTransitionIllegal 值合法，但状态机不允许这一跳（409 面）。
	ErrOpportunityTransitionIllegal = errors.New("opportunity service: 状态机不允许该跃迁")

	// ErrOpportunityClosed 行已收口，本次未改写（409 面，但下家不同：
	// 报成"跃迁不允许"会让人去查表，查完发现表没问题）。
	ErrOpportunityClosed = errors.New("opportunity service: 商机已收口，本次未改写")

	// ErrOpportunityStateInvalid 库里这一行本身越界，本层拒绝在它之上继续计算。
	// 方向刻意选"停"而不是"按默认值续"：猜出来的规则盖掉现场，事后没人看得出哪一列是被猜的。
	ErrOpportunityStateInvalid = errors.New("opportunity service: 这一行本身不合法（值域越界），拒绝继续计算")
)

// OpportunityMoveKind 一次可请求动作改的是哪一维。
//
// 分两维而不是把 won/lost 铺进阶段里（T-P4-01 的 AC① 就是这条），
// 因为两维各自成数：漏斗按 stage 计数、北极星按 status 收口。
type OpportunityMoveKind string

const (
	OpportunityMoveStage  OpportunityMoveKind = "stage"
	OpportunityMoveStatus OpportunityMoveKind = "status"
)

// OpportunityMove 一个"改一维"的动作。
type OpportunityMove struct {
	Kind   OpportunityMoveKind `json:"kind"`
	Target string              `json:"target"`
}

// OpportunityCloseCause 是谁要求这行收口的。
//
// 它是 AC③ 的实现机制（见文件头），不是审计字段：值不入库，只参与跃迁判定。
type OpportunityCloseCause string

const (
	// OpportunityCauseSales 销售侧的人工判断：能落到 lost / cancelled，也能把 lost 拉回 open。
	OpportunityCauseSales OpportunityCloseCause = "sales"
	// OpportunityCauseCollection 回款完成这个事实（P7 才持有）。**只有它能落到 won。**
	OpportunityCauseCollection OpportunityCloseCause = "collection_completed"
)

// 状态机的边表：(来源, 起点状态) → 允许的终点状态。
//
// 表驱动而不是散在 if 里：新增来源时若忘了配边，默认是"一条都不放行"，
// 而不是继承某个已有来源的权限 —— 后者会让新代码悄悄拿到赢单的写入权。
var opportunityStatusEdges = map[OpportunityCloseCause]map[string][]string{
	OpportunityCauseCollection: {
		model.OpportunityStatusOpen: {model.OpportunityStatusWon},
	},
	OpportunityCauseSales: {
		model.OpportunityStatusOpen: {model.OpportunityStatusLost, model.OpportunityStatusCancelled},
		model.OpportunityStatusLost: {model.OpportunityStatusOpen},
	},
}

func opportunityStatusMoveLegal(cause OpportunityCloseCause, from, to string) bool {
	for _, target := range opportunityStatusEdges[cause][from] {
		if target == to {
			return true
		}
	}
	return false
}

// opportunityStageMoveLegal 向前恰好一步，向后任意步，同格不算跃迁。
func opportunityStageMoveLegal(from, to string) bool {
	fromIdx, toIdx := model.OpportunityStageIndex(from), model.OpportunityStageIndex(to)
	if fromIdx < 0 || toIdx < 0 || fromIdx == toIdx {
		return false
	}
	return toIdx == fromIdx+1 || toIdx < fromIdx
}

func opportunityStatusLegal(status string) bool {
	for _, s := range model.OpportunityStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// AllowedOpportunityMoves 这一行现在**允许被请求**哪些动作（T-P4-04 的响应面，
// 形状与顺序都进判据：前端按它渲染按钮，不自己抄第二份状态机）。
//
// 回答的是"调用方能要求什么"，不是"这单可能被什么改变" —— 后者还包括回款完成，
// 而那不是按钮（所以 won 永远不在这里，同一行的赢单由 MarkWonByCollection 落）。
// 顺序：先阶段动作（按管线顺序），再状态动作（按 OpportunityStatuses 顺序）。
func AllowedOpportunityMoves(stage, status string) []OpportunityMove {
	if model.OpportunityStageIndex(stage) < 0 || !opportunityStatusLegal(status) {
		return nil
	}
	var moves []OpportunityMove
	switch status {
	case model.OpportunityStatusOpen:
		for _, candidate := range model.OpportunityStages {
			if opportunityStageMoveLegal(stage, candidate) {
				moves = append(moves, OpportunityMove{Kind: OpportunityMoveStage, Target: candidate})
			}
		}
		for _, candidate := range model.OpportunityStatuses {
			if opportunityStatusMoveLegal(OpportunityCauseSales, status, candidate) {
				moves = append(moves, OpportunityMove{Kind: OpportunityMoveStatus, Target: candidate})
			}
		}
	case model.OpportunityStatusLost:
		if opportunityStatusMoveLegal(OpportunityCauseSales, status, model.OpportunityStatusOpen) {
			moves = append(moves, OpportunityMove{Kind: OpportunityMoveStatus, Target: model.OpportunityStatusOpen})
		}
	}
	return moves
}

// OpportunityWinInput 赢率计算式的全部输入（依据见文件头）。
//
// 字段集合由用例钉成"只有这三个"：多一个字段本身无害，
// 但它是"把别的域的评分乘进来"这一步的唯一入口，所以在这里封死。
type OpportunityWinInput struct {
	Stage    string // 推进位置
	Assigned bool   // owner_user_id 非空 = 有人推进
	Overdue  bool   // 已过预计关单日仍未收口
}

// OpportunityEdit 一次整份改写（四个可编辑列，其余列各有主人）：
// 身份列与 code 在仓储侧就被白名单挡了，stage/status 走跃迁入口，
// 赢率是派生量、没有写入口。**没有"未提供"这一档**：稀疏编辑需要给每个字段再造一个
// "没传 vs 传空"的分支，而那两个分支在库里是同一个值；整份编辑配上 CAS
// 已经保证不会覆盖别人，稀疏编辑省下的只是几次传值。
type OpportunityEdit struct {
	Amount          float64    `json:"amount"`
	Currency        string     `json:"currency"`
	OwnerUserID     string     `json:"owner_user_id"`
	ExpectedCloseAt *time.Time `json:"expected_close_at"` // nil = 没有关单日目标（常态）
}

// OpportunityService 商机业务层
type OpportunityService struct {
	repo repository.OpportunityRepository

	// now 只被"是否逾期"这一条判据读。做成字段而不是包级变量：
	// 本文件与 600 多个同包文件共处，改包级时钟会跨用例串扰（-count=2 稳定红那一类）。
	now func() time.Time
}

// NewOpportunityService 构造。仓储句柄由装配侧注入 —— 本层不自己去拿全局 DB。
func NewOpportunityService(repo repository.OpportunityRepository) *OpportunityService {
	return &OpportunityService{repo: repo, now: time.Now}
}

// SetClock 注入时钟。传 nil 退回 time.Now。
//
// 装配期与测试期用同一个入口：逾期判据读它，不冻的话"把用例挪到明年跑"会有一半结论翻面。
func (s *OpportunityService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// Available 报告底座可用（装配回显与 controller 区分 503 用；**不用于吞错**，
// 改写路径不预检它，句柄坏了由仓储自己报）。
func (s *OpportunityService) Available() bool {
	return s != nil && s.repo != nil && s.repo.Available()
}

// MoveStage 把在跑的商机推到另一个阶段（向前一步或向后任意步，见文件头）。
//
// expectedVersion 是**调用方看到的那一行的版本**，不是"最新版本"的别名：
// 乐观锁要防的是"两个人各自看着 v3 各改一处"，若本层自己重读并采用读到的版本，
// 保护就死在这一层里了 —— 库里那条已经被别人改到 v5 这件事必须由调用方听见。
func (s *OpportunityService) MoveStage(ctx context.Context, id string, expectedVersion int64, toStage string) (*model.Opportunity, error) {
	if model.OpportunityStageIndex(toStage) < 0 {
		return nil, fmt.Errorf("%w: 目标阶段 %q 不在值域里", ErrOpportunityInputInvalid, toStage)
	}
	return s.transition(ctx, id, &expectedVersion, false, func(row *model.Opportunity) (bool, error) {
		if !opportunityStageMoveLegal(row.Stage, toStage) {
			return false, fmt.Errorf("%w: %s → %s（阶段向前只一步、向后任意步，同格不算跃迁）",
				ErrOpportunityTransitionIllegal, row.Stage, toStage)
		}
		row.Stage = toStage
		// 赢率跟着新阶段走：预测的条件集变了（现在多了"这一格的证据已经产生"）。
		return true, s.recomputeWinProbability(row)
	})
}

// Edit 整份改写四个可编辑列，并按新事实重算赢率。
func (s *OpportunityService) Edit(ctx context.Context, id string, expectedVersion int64, in OpportunityEdit) (*model.Opportunity, error) {
	if err := validateOpportunityEdit(in); err != nil {
		return nil, err
	}
	return s.transition(ctx, id, &expectedVersion, false, func(row *model.Opportunity) (bool, error) {
		row.Amount = roundOpportunityColumn(in.Amount, opportunityAmountDecimalPlaces)
		row.Currency = in.Currency
		row.OwnerUserID = in.OwnerUserID
		if in.ExpectedCloseAt != nil {
			at := *in.ExpectedCloseAt
			row.ExpectedCloseAt = &at
		} else {
			row.ExpectedCloseAt = nil
		}
		return true, s.recomputeWinProbability(row)
	})
}

// MarkLost 判输。原因是本表的必填项而不是可选备注：P8 的丢单归因只有这一列可读，
// 空着的原因会让"这单为什么没了"重新变成问销售本人的事。
func (s *OpportunityService) MarkLost(ctx context.Context, id string, expectedVersion int64, reason string) (*model.Opportunity, error) {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: 输单原因为空（P8 的归因只有这一列可读）", ErrOpportunityInputInvalid)
	}
	if len(trimmed) > opportunityLostReasonMaxLen {
		return nil, fmt.Errorf("%w: 输单原因长度 %d 超上限 %d",
			ErrOpportunityInputInvalid, len(trimmed), opportunityLostReasonMaxLen)
	}
	client := expectedVersion
	return s.statusMove(ctx, id, &client, OpportunityCauseSales, model.OpportunityStatusLost, func(row *model.Opportunity) {
		row.LostReason = trimmed
	})
}

// Cancel 作废：误建或重复。它**不写** lost_reason —— 那个字段是丢单归因的口径，
// 把误建混进去，看板会拿去给销售团队定一条根本不存在问题的改进项（同 T-P4-01 分两态的理由）。
func (s *OpportunityService) Cancel(ctx context.Context, id string, expectedVersion int64) (*model.Opportunity, error) {
	client := expectedVersion
	return s.statusMove(ctx, id, &client, OpportunityCauseSales, model.OpportunityStatusCancelled, nil)
}

// Reopen 把误判输的商机拉回在跑：清掉输单原因（不变量：非 lost 的行不带原因，
// 留着旧原因，P8 会在"还在跑的行"上读到上一轮的结论），并按当前事实重算赢率。
//
// 只有 lost 能回退：won 背后是真发生的钱，cancelled 的含义是"根本不该有这一行"。
func (s *OpportunityService) Reopen(ctx context.Context, id string, expectedVersion int64) (*model.Opportunity, error) {
	client := expectedVersion
	return s.statusMove(ctx, id, &client, OpportunityCauseSales, model.OpportunityStatusOpen, func(row *model.Opportunity) {
		row.LostReason = ""
	})
}

// MarkWonByCollection 回款完成 → 赢单。**本层写 won 的唯一入口**（AC③，P7 调）。
//
// 三个不显眼但都是刻意的决定：
//   - 不带 expectedVersion：它报的是事实，不是某人看到的视图；行的自锁由刚读到的版本承担；
//   - 不动 stage："停在 proposal 就成了"是一句合法的商机史（T-P4-01 立着这条）；
//   - 不重算赢率：留下收口前那个预测，供 P8 与真实结果比对（依据见文件头）。
//
// 已赢单的行重放 = 成功且零改写；lost / cancelled 的行被回款"改成"赢单 = 拒绝，
// 那是两笔账对不上，要人来判（显式两步：先 Reopen 再等下一次回款完成）。
func (s *OpportunityService) MarkWonByCollection(ctx context.Context, id string) (*model.Opportunity, error) {
	return s.statusMove(ctx, id, nil, /* 机器路径：以刚读到的版本为准，见函数头 */
		OpportunityCauseCollection, model.OpportunityStatusWon, nil)
}

// statusMove 的 expectedVersion：人工入口传调用方看到的那一格，机器入口（回款完成）传 nil
// 让核心采用刚读到的那一格 —— 两条路都是 CAS 改写，区别只在"谁的那份视图在赌"。
func (s *OpportunityService) statusMove(ctx context.Context, id string, expectedVersion *int64,
	cause OpportunityCloseCause, target string, extra func(*model.Opportunity)) (*model.Opportunity, error) {
	// won 是终态，回款这件事本身要求行还在跑或已经赢过；其余状态动作对已收口的行都该拒。
	// 所以只有"落到 open"（Reopen）与"幂等重放的 won"需要跳过收口守卫，
	// 由下面的 allowClosed 精确放行，而不是让每个终态都自己解释一遍。
	allowClosed := target == model.OpportunityStatusOpen || target == model.OpportunityStatusWon
	return s.transition(ctx, id, expectedVersion, allowClosed, func(row *model.Opportunity) (bool, error) {
		if row.Status == target {
			if cause == OpportunityCauseCollection && target == model.OpportunityStatusWon {
				return false, nil // 同一条事实第二次到达：成功，零改写
			}
			return false, fmt.Errorf("%w: %s → %s", ErrOpportunityTransitionIllegal, row.Status, target)
		}
		if !opportunityStatusMoveLegal(cause, row.Status, target) {
			return false, fmt.Errorf("%w: %s → %s（来源 %s）",
				ErrOpportunityTransitionIllegal, row.Status, target, cause)
		}
		row.Status = target
		if extra != nil {
			extra(row)
		}
		if model.OpportunityClosed(target) {
			return true, nil // 终态不重算：依据见文件头
		}
		return true, s.recomputeWinProbability(row)
	})
}

// transition 本层唯一的改写通道：读-验现值-（收口守卫）-（版本比对）-改-验结果-写-回读。
//
// 现值与结果各验一次不是重复：前者拦"库里那条本来就坏了"（报 StateInvalid，
// 让调用方去查数据），后者拦"这一刀把它改坏了"（同样报 StateInvalid，但现场没人被改）。
// 两次之间没有任何写库动作，所以坏的结果一定出自本层自己这一次计算。
func (s *OpportunityService) transition(ctx context.Context, id string, expectedVersion *int64,
	allowClosed bool, mutate func(*model.Opportunity) (bool, error)) (*model.Opportunity, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("opportunity service: 未装配仓储句柄")
	}
	if expectedVersion != nil && *expectedVersion < 0 {
		return nil, fmt.Errorf("%w: expectedVersion 不能为负（想表达「以最新为准」的是机器入口，不是这里）",
			ErrOpportunityInputInvalid)
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err // 原样上抛：读故障不能被改写成"没有这条商机"，那边会去重走一遍新建流程
	}
	if row == nil {
		return nil, repository.ErrOpportunityNotFound
	}
	if err := validateOpportunityRow(row); err != nil {
		return nil, err
	}
	if model.OpportunityClosed(row.Status) && !allowClosed {
		return nil, fmt.Errorf("%w（当前 %s）", ErrOpportunityClosed, row.Status)
	}
	if expectedVersion != nil && row.Version != *expectedVersion {
		return nil, repository.ErrOpportunityStaleVersion
	}
	changed, err := mutate(row)
	if err != nil {
		return nil, err
	}
	if !changed {
		return row, nil // 幂等重放：返回的就是库里那一行（刚才读回来的）
	}
	if err := validateOpportunityRow(row); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, row); err != nil {
		return nil, err // 不自动重试：重试策略属于调用方，且 CAS 的失败本来就该被看见
	}
	fresh, err := s.repo.GetByID(ctx, id)
	if err != nil {
		// 改写已经落库了，这里不能把它说成失败：报出来，但把已改写的版本带上让调用方看见。
		return nil, fmt.Errorf("opportunity service: 改写已落库但回读失败（%s v%d 起）: %w", id, row.Version+1, err)
	}
	if fresh == nil {
		return nil, fmt.Errorf("opportunity service: 改写落库后这一行不在了（%s）: %w", id, repository.ErrOpportunityNotFound)
	}
	return fresh, nil
}

// recomputeWinProbability 按这一行当前的阶段与事实回写赢率。
func (s *OpportunityService) recomputeWinProbability(row *model.Opportunity) error {
	p, err := opportunityWinProbability(OpportunityWinInput{
		Stage:    row.Stage,
		Assigned: strings.TrimSpace(row.OwnerUserID) != "",
		Overdue:  opportunityIsOverdue(row.ExpectedCloseAt, s.now()),
	})
	if err != nil {
		return err
	}
	row.WinProbability = p
	return nil
}

// opportunityIsOverdue 逾期判据只此一处：将来"逾期提醒/逾期聚合"要用同一个定义时
// 必须走这里，否则两处对"逾期"的口径会分家，而分家之后没人能说出漏斗上那个数算的是哪个。
func opportunityIsOverdue(expectedCloseAt *time.Time, now time.Time) bool {
	return expectedCloseAt != nil && expectedCloseAt.Before(now)
}

func opportunityWinProbability(in OpportunityWinInput) (float64, error) {
	var p float64
	switch in.Stage {
	case model.OpportunityStageQualification:
		p = opportunityWinProbQualificationBase
	case model.OpportunityStageNeedsConfirmed:
		p = opportunityWinProbNeedsConfirmedBase
	case model.OpportunityStageProposal:
		p = opportunityWinProbProposalBase
	case model.OpportunityStageNegotiation:
		p = opportunityWinProbNegotiationBase
	default:
		// 不退化成 0：0 是最响的那个错误答案（下游读成"绝不可能成"而停掉所有跟进）。
		return 0, fmt.Errorf("%w: 阶段 %q 不在值域里，赢率无法计算", ErrOpportunityStateInvalid, in.Stage)
	}
	if !in.Assigned {
		p -= opportunityWinProbPenaltyNoOwner
	}
	if in.Overdue {
		p -= opportunityWinProbPenaltyOverdue
	}
	if p = roundOpportunityColumn(p, opportunityWinProbDecimalPlaces); p < opportunityWinProbFloor {
		p = opportunityWinProbFloor
	}
	return p, nil
}

// roundOpportunityColumn 把值舍到列的小数位。**先舍再比再写**：
// 让 PG 去舍的话，本层返回的那一份与库里的那一份会差在最后一条判据的边上（阈值 0.50）。
func roundOpportunityColumn(v float64, places int) float64 {
	shift := math.Pow(10, float64(places))
	return math.Round(v*shift) / shift
}

// validateOpportunityEdit 只查**调用方递进来的那四格**：金额量程与可表示性、币种形状、
// 归属列宽、以及"零值时间不许当成关单日"（它会被 P8 的逾期聚合读成早已过期）。
func validateOpportunityEdit(in OpportunityEdit) error {
	if err := validateOpportunityAmount(in.Amount); err != nil {
		return err
	}
	if err := validateOpportunityCurrency(in.Currency); err != nil {
		return err
	}
	if len(in.OwnerUserID) > opportunityOwnerMaxLen {
		return fmt.Errorf("%w: 负责人长度 %d 超列宽 %d（越界要到事件抄写那一步才炸，且整批回滚）",
			ErrOpportunityInputInvalid, len(in.OwnerUserID), opportunityOwnerMaxLen)
	}
	if in.ExpectedCloseAt != nil && in.ExpectedCloseAt.IsZero() {
		return fmt.Errorf("%w: 预计关单日是零值时间（想表达「没有目标日」请传 nil）", ErrOpportunityInputInvalid)
	}
	return nil
}

func validateOpportunityAmount(amount float64) error {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return fmt.Errorf("%w: 金额 %v 不可入库（numeric 无对应值，且它会静默污染一切聚合）",
			ErrOpportunityInputInvalid, amount)
	}
	if amount < 0 {
		return fmt.Errorf("%w: 金额为负（这单值多少钱没有负的说法；退款走 P7 的冲销）", ErrOpportunityInputInvalid)
	}
	if amount >= opportunityAmountMax {
		return fmt.Errorf("%w: 金额 %.2f 超出 numeric(12,2) 容量", ErrOpportunityInputInvalid, amount)
	}
	return nil
}

func validateOpportunityCurrency(currency string) error {
	if len(currency) != opportunityCurrencyLen {
		return fmt.Errorf("%w: 币种 %q 不是三位码（空串在仓储侧会被兜成 %s，那是把「没填」读成「改成 CNY」）",
			ErrOpportunityInputInvalid, currency, model.OpportunityCurrencyDefault)
	}
	for _, r := range currency {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("%w: 币种 %q 含非大写 ASCII 字母（ISO 4217 三位码）", ErrOpportunityInputInvalid, currency)
		}
	}
	return nil
}

// validateOpportunityRow 整行值域（含那条贯穿两列的不变量：lost_reason 非空 ⟺ status=lost）。
//
// 判"这一行能不能被本层理解"，不判"这一行好不好看"：越界的现值一律拒在计算之前，
// 因为本层每次改写都会把它读过的列原样写回去（仓储的白名单是整份 map），
// 看不懂就动手等于用一套猜出来的规则盖掉现场，而事后没人能指出哪一列是被猜的。
func validateOpportunityRow(row *model.Opportunity) error {
	if model.OpportunityStageIndex(row.Stage) < 0 {
		return fmt.Errorf("%w: 阶段 %q", ErrOpportunityStateInvalid, row.Stage)
	}
	if !opportunityStatusLegal(row.Status) {
		return fmt.Errorf("%w: 状态 %q", ErrOpportunityStateInvalid, row.Status)
	}
	if err := validateOpportunityAmount(row.Amount); err != nil {
		return fmt.Errorf("%w（现值）: %v", ErrOpportunityStateInvalid, err)
	}
	if err := validateOpportunityCurrency(row.Currency); err != nil {
		return fmt.Errorf("%w（现值）: %v", ErrOpportunityStateInvalid, err)
	}
	if row.WinProbability < 0 || row.WinProbability > model.OpportunityWinProbabilityMax {
		return fmt.Errorf("%w: 赢率 %v 越出 0–1", ErrOpportunityStateInvalid, row.WinProbability)
	}
	if len(row.OwnerUserID) > opportunityOwnerMaxLen {
		return fmt.Errorf("%w: 负责人列宽 %d 越界", ErrOpportunityStateInvalid, len(row.OwnerUserID))
	}
	if row.ExpectedCloseAt != nil && row.ExpectedCloseAt.IsZero() {
		return fmt.Errorf("%w: 预计关单日是零值时间（与 NULL 在逾期聚合里是两个答案）", ErrOpportunityStateInvalid)
	}
	if (row.Status == model.OpportunityStatusLost) != (row.LostReason != "") {
		return fmt.Errorf("%w: lost_reason 与 status=%s 不匹配（不变量：非空 ⟺ 输单）",
			ErrOpportunityStateInvalid, row.Status)
	}
	return nil
}
