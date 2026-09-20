// opportunity_convert.go T-P4-05：一条达标的线索变成一个有人推进的商机。
//
// 「一键」在这里指的是**运营的这一个开关**（ltc.config 的 opportunity 阶段），
// 不是页面上的一个按钮 —— 依据是三方调研成果.md 对 AC1 的原话："一键开启 LTC 后，
// 新线索可**无人值守**走完…"。所以本层的调用方是线索挖掘那条写路径（见 lead_mining.go
// 的 persistLead），人在其中不需要点任何东西。HTTP 侧的手工建单入口今天**不存在**，
// 它是 T-P4-04 那份"到得了但不是由点按钮的人决定"清单的延续，已登记为待办。
//
// 三道判据的顺序是有意的，每一道都在替下一道省钱或省事故：
//  1. 入参量程 —— 越界一律 ErrOpportunityInputInvalid，**不进闸门**。
//     本仓有两个都叫 confidence 的数（clue_scores 侧 0–100 的维度覆盖度、
//     判定侧 0–1 的模型把握），量程错位若被当成"不达标"，症状是"每条线索都不达标"，
//     而那道闸门每天都绿 —— 比放行隐蔽得多。
//  2. 闸门（阶段是否生效 + C5 双阈值）—— 不过就**不建**，并给出是哪一道拦的。
//     配置读不到时按"关"处理：降级那份是回落默认（全关），把它读成"默认值挺宽松"
//     就等于库里存储故障的当晚凭空多出一批商机。
//  3. 幂等 —— 闸门之后才查"这条线索是不是已经有商机了"。放在闸门之前会为了一个
//     注定不建的行去读库；放在写之后则根本来不及。
//
// 本层**一个字节都不写 clues.is_opportunity**（AC②）。那一列今天的语义是挖掘侧按
// intent_score 顺手打上的热度标记（lead_mining.go 与 lead_miner_unified.go 两处都在写），
// 不是"这条线索已经变成商机"。两处都写 ⇒ 同一列承载两个判据、取值时刻还不同，
// 旧读取方（列表筛 COALESCE(is_opportunity,0)>=1）看到的人群会凭空换一批。
// "由线索反查商机"从此走 opportunities.clue_id。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 闸门结论的字面量。LTC 阶段本身没开时直接透传 LTCReason*（配置层已经给全了原因），
// 这里只补两个配置层表达不出来的形状。
const (
	// ConvertGateAlreadyConverted 这条线索已经转化过 —— 与"闸门拒签"必须分开：
	// 前者说明系统状态是对的（该有人去推进那一条商机），后者说明系统按运营的要求没动。
	ConvertGateAlreadyConverted = "already_converted"
	// ConvertGateThresholdUnmet 阶段开着，但 lead_score / confidence 没过 C5 那两道阈值。
	ConvertGateThresholdUnmet = "threshold_unmet"

	// OpportunityConvertAuditModule 是 operation_logs 里本层的 module 名。
	OpportunityConvertAuditModule = "opportunity_convert"
	// opportunityConvertAuditActor 写在 username 而不是 user_id 上：
	// 无人值守的动作没有一个对应的人，塞某个真人的 id 等于把系统的行为记成他的操作，
	// 而 users 表里 id=0 那一行本来就不存在，join 过去是空的。
	opportunityConvertAuditActor = "system:ltc_opportunity_convert"

	// opportunityLeadScoreMax 是线索分的量程上界（0–100，与 ltc.config 的 lead_score 同量程）。
	opportunityLeadScoreMax = 100
)

// ErrOpportunityConverterUnavailable 转换器未装配。与"闸门关着"分开：
// 前者是部署的事（该去修装配），后者是运营的选择（什么都不用修）。
var ErrOpportunityConverterUnavailable = errors.New("opportunity convert: 转换器未装配（商机仓储 / LTC 配置 / 分配器任一缺失）")

// LTCConfigReader 闸门要读的那一份策略。
//
// 只暴露 Config 而不是把 *LTCConfigService 整个钉进签名：本层要的是"现在这份阈值是多少"，
// 而缓存、降级、存储那三件事已经全部发生在 Config 里面了。多接一个方法就多一处
// 需要仿真的依赖，而仿真出来的降级行为从来不像真的。
type LTCConfigReader interface {
	Config(ctx context.Context) *LTCConfig
}

// OpportunityConversion 一次转换的全部输入。
//
// 刻意不接 *model.Clue：线索行里有半打列与本层无关（群、消息、账号归属…），
// 把整行递进来迟早会有人顺手去读其中的某一列，那时本层的判据就不再是这一份签名了。
type OpportunityConversion struct {
	ClueID     string // 幂等键，必填
	CustomerID string // 可为空：没有 CDP 行时商机照样能建，只是查不到"该客户的老销售"
	OneID      string
	LeadScore  int     // 0–100
	Confidence float64 // 0–1，模型对自己结论的把握（不是 clue_scores 那个 0–100 覆盖度）

	Amount          float64    // 0 = 还不知道这单值多少钱（转来的时候多半就是这个值）
	Currency        string     // 空串按建表默认币种兜
	ExpectedCloseAt *time.Time // nil = 没有关单日目标
}

// OpportunityConvertResult 一次转换的结论。
//
// created/allowed 是两个维度，分不开就会把"没到阈值"与"早就转过了"读成同一件事：
//   - allowed=false ⇒ 本次没建，原因见 gate_reason（含 LTC 那四个 reason 字面量）；
//   - allowed=true, created=false ⇒ 闸门放行，但这条线索已经有商机了（幂等重放）；
//   - allowed=true, created=true  ⇒ 建了，那一行在 opportunity、解释在 assignment。
type OpportunityConvertResult struct {
	Allowed     bool               `json:"allowed"`
	Created     bool               `json:"created"`
	GateReason  string             `json:"gate_reason,omitempty"`
	GateDetail  string             `json:"gate_detail,omitempty"`
	Opportunity *model.Opportunity `json:"opportunity,omitempty"`
	Assignment  *OwnerAssignment   `json:"assignment,omitempty"`

	// 审计与转化分开报，口径抄 ltc.config 的 Save：商机已经落库是既成事实，
	// 审计写失败若报成整笔失败，调用方一重试就变成两行。
	AuditWritten bool   `json:"audit_written"`
	AuditError   string `json:"audit_error,omitempty"`
}

// OpportunityConvertService 线索→商机的转换层。依赖全部注入，本层不自己去拿全局 DB。
type OpportunityConvertService struct {
	repo     repository.OpportunityRepository
	gate     LTCConfigReader
	assigner *OwnerAssigner
	audit    ltcAuditWriter

	// now 同时喂三处：主键的时间段、赢率的逾期判据、审计的时间痕。
	// 分叉成两个时钟会出现"这一行刚建出来就已经逾期"这种自相矛盾的形状。
	now func() time.Time
}

// NewOpportunityConvertService 构造。audit 允许为 nil（装配回显会照实报出这一格空着）。
func NewOpportunityConvertService(repo repository.OpportunityRepository, gate LTCConfigReader,
	assigner *OwnerAssigner, audit ltcAuditWriter) *OpportunityConvertService {
	return &OpportunityConvertService{repo: repo, gate: gate, assigner: assigner, audit: audit, now: time.Now}
}

// SetClock 注入时钟。传 nil 退回 time.Now。
func (s *OpportunityConvertService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// Available 报告能不能转。
//
// 审计句柄**不在**这一条里：审计缺了要报出来（见 AuditError），但不该拦下转化 ——
// 拦住之后运营会以为闸门有问题，去翻 ltc.config，而真正缺的是一行装配。
func (s *OpportunityConvertService) Available() bool {
	if s == nil || s.gate == nil || !s.assigner.Available() {
		return false
	}
	return s.repo != nil && s.repo.Available()
}

// ConvertFromClue 判三道关，放行则落一行 open/qualification 的商机并带上分配解释。
//
// error 只用于"我们没能得出结论"（读失败、写失败、入参越界、未装配）；
// "按运营的配置这次不建"是**结果不是故障**，它走 allowed=false。
// 把这两类混成一类，挖掘那条路径就会在日志里把一次合法的拒签当故障重试。
func (s *OpportunityConvertService) ConvertFromClue(ctx context.Context, in OpportunityConversion) (OpportunityConvertResult, error) {
	if !s.Available() {
		return OpportunityConvertResult{}, ErrOpportunityConverterUnavailable
	}

	clueID := strings.TrimSpace(in.ClueID)
	if clueID == "" {
		return OpportunityConvertResult{}, fmt.Errorf(
			"%w: 线索号为空 —— 没有幂等键的转换，一次重投递就是两行商机", ErrOpportunityInputInvalid)
	}
	if in.LeadScore < 0 || in.LeadScore > opportunityLeadScoreMax {
		return OpportunityConvertResult{}, fmt.Errorf(
			"%w: lead_score %d 越出 0–%d（它是线索分；0–1 的那个数量叫 confidence，别递到这里来）",
			ErrOpportunityInputInvalid, in.LeadScore, opportunityLeadScoreMax)
	}
	if !(in.Confidence >= 0 && in.Confidence <= 1) {
		// NaN 走的是同一支：两个比较都为 false，取反后仍然为 false ⇒ 判越界。
		// 这里不写 !math.IsNaN(...) 的显式分支，是为了不让"NaN 合法"这件事有第二次被写出来的机会。
		return OpportunityConvertResult{}, fmt.Errorf(
			"%w: confidence %v 越出 0–1 —— 本仓有两个同名不同量程的数，clue_scores 侧的 0–100 覆盖度不是这一格",
			ErrOpportunityInputInvalid, in.Confidence)
	}

	cfg := s.gate.Config(ctx)
	// StageActive 的接收者可为 nil（nil 那份按降级处理），LeadQualified 不行：
	// 它要读 c.Thresholds。所以这道关必须排在下一道之前，顺序本身就是护栏。
	if active, reason := cfg.StageActive(LTCStageOpportunity); !active {
		detail := fmt.Sprintf("商机阶段闸门未生效（%s）", reason)
		if cfg != nil && cfg.DegradeReason != "" {
			detail += "：" + cfg.DegradeReason
		}
		return OpportunityConvertResult{Allowed: false, GateReason: reason, GateDetail: detail}, nil
	}
	if ok, detail := cfg.LeadQualified(in.LeadScore, in.Confidence); !ok {
		return OpportunityConvertResult{
			Allowed: false, GateReason: ConvertGateThresholdUnmet,
			GateDetail: "达标判定未过：" + detail,
		}, nil
	}

	existing, err := s.repo.GetByClueID(ctx, clueID)
	if err != nil {
		return OpportunityConvertResult{}, fmt.Errorf("opportunity convert: 反查线索 %s 失败：%w", clueID, err)
	}
	if existing != nil {
		// 不再分配、不再查历史：重放的代价必须是零副作用，否则一次网络抖动就会
		// 把已经有人推进的商机悄悄换个人。
		return OpportunityConvertResult{
			Allowed: true, Created: false, GateReason: ConvertGateAlreadyConverted,
			GateDetail:  fmt.Sprintf("线索 %s 已经转过商机 %s，本次未改写", clueID, existing.Code),
			Opportunity: existing,
		}, nil
	}

	preferred, err := s.priorOwner(ctx, in.CustomerID)
	if err != nil {
		return OpportunityConvertResult{}, err
	}
	assignment, err := s.assigner.Assign(ctx, AssignRequest{PreferredOwner: preferred, ClueID: clueID})
	if err != nil {
		return OpportunityConvertResult{}, err
	}

	now := s.now()
	row, err := s.newRow(now, clueID, in, assignment)
	if err != nil {
		return OpportunityConvertResult{}, err
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		// 原样上抛：撞编号（ErrOpportunityCodeConflict）与别的写故障在这里**必须分得开**，
		// 而分开的判据在仓储侧。换编号重试绝不做 —— 那会把一次重复投递变成两行商机。
		return OpportunityConvertResult{}, fmt.Errorf("opportunity convert: 线索 %s 落库失败：%w", clueID, err)
	}

	res := OpportunityConvertResult{
		Allowed: true, Created: true, GateReason: LTCReasonActive,
		Opportunity: row, Assignment: &assignment,
	}
	s.writeAudit(ctx, row, in, assignment, &res)
	logger.Infof("[ltc-convert] ✅ 线索 %s → 商机 %s（编号 %s）归属=%q 规则=%s 赢率=%v",
		clueID, row.ID, row.Code, row.OwnerUserID, assignment.Rule, row.WinProbability)
	return res, nil
}

// priorOwner 该客户**上一条**商机的归属销售，作为分配规则一的输入。
//
// 取"最近一条、任何状态"：沿用一条已丢单商机的销售是合理的（他本来就在跟这个客户），
// 而"只沿用还在跑的"会让同一客户在两次转化之间换人 —— 客户视角里那就是前功尽弃。
// 所以这里显式把四个状态全递进去，而不是让仓储"默认只看在办的"。
// 没有 customer_id 时**根本不发这次查询**：拿空串去查会命中别人的行，见仓储侧同款守卫。
func (s *OpportunityConvertService) priorOwner(ctx context.Context, customerID string) (string, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return "", nil
	}
	rows, err := s.repo.ListByCustomer(ctx, customerID, model.OpportunityStatuses, 1, 0)
	if err != nil {
		return "", fmt.Errorf("opportunity convert: 读客户 %s 的历史商机失败：%w", customerID, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].OwnerUserID, nil
}

// newRow 组装那一行，并在这里把赢率算出来。
//
// 赢率不是"落库后再回写"：仓储的 Insert 白名单里没有这一列之外的兜底，
// 而一个 win_probability=0 的新行会被漏斗与看板读成"绝不可能成"从而停掉所有跟进。
func (s *OpportunityConvertService) newRow(now time.Time, clueID string, in OpportunityConversion,
	assignment OwnerAssignment) (*model.Opportunity, error) {
	currency := strings.TrimSpace(in.Currency)
	if currency == "" {
		currency = model.OpportunityCurrencyDefault
	}
	id, code := newOpportunityKeysFromClock(now)
	row := &model.Opportunity{
		ID:              id,
		Code:            code,
		CustomerID:      strings.TrimSpace(in.CustomerID),
		OneID:           strings.TrimSpace(in.OneID),
		ClueID:          clueID,
		Stage:           model.OpportunityStageQualification,
		Status:          model.OpportunityStatusOpen,
		Amount:          in.Amount,
		Currency:        currency,
		OwnerUserID:     assignment.OwnerUserID,
		ExpectedCloseAt: in.ExpectedCloseAt,
		Version:         0, // 新行"一次都没改过"，这是仓储 Insert 的硬前置
	}
	// 四格编辑校验复用同一份函数：金额量程、币种形状、归属列宽、零值时间不当关单日，
	// 两处各写一份判据，早晚会有一处放过另一处拦住的值。
	if err := validateOpportunityEdit(OpportunityEdit{
		Amount: row.Amount, Currency: row.Currency,
		OwnerUserID: row.OwnerUserID, ExpectedCloseAt: row.ExpectedCloseAt,
	}); err != nil {
		return nil, err
	}
	p, err := opportunityWinProbability(OpportunityWinInput{
		Stage:    row.Stage,
		Assigned: row.OwnerUserID != "",
		Overdue:  opportunityIsOverdue(row.ExpectedCloseAt, now),
	})
	if err != nil {
		return nil, err
	}
	row.WinProbability = p
	return row, nil
}

// writeAudit 把"这条商机是怎么来的"记进行级审计。
//
// 记的不是"谁点了按钮"（无人值守没有按钮），而是**当时那三道判据的输入与输出**：
// 事后有人问"这单凭什么占我的工作台"，这一行就是唯一能拿出来的答案。
// 写失败只把缺的那半照实报出来（res.AuditError），不改整笔结论。
func (s *OpportunityConvertService) writeAudit(ctx context.Context, row *model.Opportunity,
	in OpportunityConversion, assignment OwnerAssignment, res *OpportunityConvertResult) {
	if s.audit == nil {
		res.AuditError = "审计仓储未装配 ⇒ 这次转化在 operation_logs 里没有对应行"
		logger.Errorf("[ltc-convert] ❌ %s（商机 %s / 线索 %s）", res.AuditError, row.ID, in.ClueID)
		return
	}
	log := &model.OperationLog{
		UserID:     0,
		Username:   opportunityConvertAuditActor,
		Action:     "create",
		Module:     OpportunityConvertAuditModule,
		Resource:   "opportunity",
		ResourceID: in.ClueID,
		Detail: fmt.Sprintf("线索 %s → 商机 %s（编号 %s）：lead_score=%d confidence=%v 分配规则=%s 归属=%q 赢率=%v 候选=[%s]",
			in.ClueID, row.ID, row.Code, in.LeadScore, in.Confidence, assignment.Rule, assignment.OwnerUserID,
			row.WinProbability, summarizeCandidates(assignment.Candidates)),
		NewValue: fmt.Sprintf(`{"id":%q,"code":%q,"clue_id":%q,"stage":%q,"status":%q,"owner_user_id":%q,"win_probability":%v}`,
			row.ID, row.Code, row.ClueID, row.Stage, row.Status, row.OwnerUserID, row.WinProbability),
	}
	if err := s.audit.Create(ctx, log); err != nil {
		res.AuditError = err.Error()
		logger.Errorf("[ltc-convert] ❌ 商机已落库但审计没跟上（%v）⇒ 商机 %s 在 operation_logs 里没有来源记录",
			err, row.ID)
		return
	}
	res.AuditWritten = true
}

func summarizeCandidates(candidates []OwnerCandidate) string {
	parts := make([]string, 0, len(candidates))
	for _, c := range candidates {
		parts = append(parts, fmt.Sprintf("%s:%d", c.SalesID, c.OpenCount))
	}
	return strings.Join(parts, ",")
}

// —— 主键与对外编号 ——————————————————————————————————————

// opportunitySeq 是进程内单调计数器。
//
// 为什么"纳秒 + 计数器"两样都要：只取纳秒，同一纳秒内的两次生成会撞（批量转线索时
// 真会撞上，一个循环里连着两条达标线索）；只取计数器，两个进程各自从 1 开始就撞。
// 同 human_task / approval 那两处生成器一个口径。
var opportunitySeq int64

func nextOpportunitySeq() int64 { return atomic.AddInt64(&opportunitySeq, 1) }

// newOpportunityKeys 纯函数版生成器（同一时刻 + 同一 seq ⇒ 同一对键），用例可直接复算。
//
// ID 走 unix **纳秒**：它是跨表引用的稳定键，同一秒内两条的区分全靠后面那个 seq，
// 纳秒让"同一秒的两次"也多半不必依赖 seq 就不撞。
// Code 走 unix **秒** 的 base36：它是给人念的，越短越好；
// **刻意不含日期串** —— 带日期就要选一个时区来格式化，而本仓的 PG 会话钉在 CST、
// Go 侧读宿主机时区，同一时刻会生成两个不同的"当天序号"（日期边界裂脑那条老账）。
// 长度上限：4 + 6（秒）+ 1 + 13（seq 在 int64 上限处的 base36）= 24 < 32（本列宽）。
func newOpportunityKeys(now time.Time, seq int64) (string, string) {
	return fmt.Sprintf("opp_%d_%d", now.UnixNano(), seq),
		fmt.Sprintf("OPP-%s-%s", strconv.FormatInt(now.Unix(), 36), strconv.FormatInt(seq, 36))
}

// newOpportunityKeysFromClock 从时钟 + 计数器取一对新键。
func newOpportunityKeysFromClock(now time.Time) (string, string) {
	return newOpportunityKeys(now, nextOpportunitySeq())
}

// ---- 进程级实例（与 GlobalLTCConfig / GlobalOpportunityService 同一形状：后写覆盖）------

var (
	globalOpportunityConverterMu sync.RWMutex
	globalOpportunityConverter   *OpportunityConvertService
)

// SetGlobalOpportunityConverter 装配层注入（装配点 app.InitOpportunityRuntime）。
// 传 nil 是**清空**而不是"忽略"：Init 会被重复调用（测试、灰度重启都是真实路径），
// 上一次的实例若赖在全局里，挖掘那条路径就会对着一句"未装配"的日志继续往一张
// 已经不该再写的表里写行 —— 同 T-P4-04 对商机底座那条判据。
func SetGlobalOpportunityConverter(svc *OpportunityConvertService) {
	globalOpportunityConverterMu.Lock()
	globalOpportunityConverter = svc
	globalOpportunityConverterMu.Unlock()
}

// GlobalOpportunityConverter 取全局实例；未装配时返回 nil。
//
// 这里**刻意不做惰性构造**（与 GlobalLTCConfig 不同）：惰性建一个要走全局 DB 句柄，
// 而本实例的第一个调用方是挖掘 worker 的协程 —— 那意味着建句柄的时刻、以及它用的是
// 哪一副库，都由一条后台消息的到达时间决定。商机竖的装配唯一入口是 app.InitOpportunityRuntime，
// 它已经把"有 DB 就装、没 DB 就清"写进日志了；这里再兜一次只会把那条日志变成假话。
//
// 返回 nil 是安全的调用形状：Available() 的接收者可为 nil，调用方不需要先判空。
func GlobalOpportunityConverter() *OpportunityConvertService {
	globalOpportunityConverterMu.RLock()
	svc := globalOpportunityConverter
	globalOpportunityConverterMu.RUnlock()
	return svc
}
