// approval_request.go 异步审批检查点服务（T-P3-01 / N-4，C2 裁定的唯一实体）
//
// 本文件只有三件事：入队（含幂等与 auto-approve 快速路径）、裁决（比较并交换）、
// 到期落终态。**它不知道任何流程怎么挂起、从哪里续跑**：T-P3-02 把这些能力做成一个
// 出口（`ApprovalWaitNotifier`，由 SOP 侧实现）而不是把流程知识写进本文件 ——
// "审批这件事的结论是什么"与"流程跑到哪了"必须留在两处，一旦合表就会出现
// "审批说已批准、流程根本没人续跑"这种两边都对、合起来错的分歧
// （C2 明确要求复用同一套游标，不另造）。
//
// 旧接口 `tooluse.ApprovalChecker`（bool）在本卡**未被引用、也未被改动**：
// C2 的约束是「禁止在 decorator_approval.go 上叠加第二个布尔接口」，
// 而它允许旧接口"作为 auto-approve 的判据来源之一"。这里给的正是那个入口的形状
// （AutoApprovalPolicy），但刻意不在本卡把它接到白名单 checker 上 ——
// 从 (subject_type, subject_id) 映射到 (tool_name, account_id) 是调用方的知识
// （报价的 subject 是 quote_id、外发的 subject 是 reach_plan_id，账号从哪来各不相同），
// 现在接等于替三个还没写的调用方各自代设计一次。接线的卡是 T-P3-07 / T-P5-03。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 审批挂起的默认与上限。
//
// 24h 不是随手取的：本表服务的动作是外发/报价/下发订单指令，这些业务的时机窗口以小时计，
// 挂一天基本等于这单不做了 —— 而"没人裁决"必须以一个**会自己结束**的状态收口，
// 否则 pending 行只增不减、挂起的流程永远醒不过来，且没人能说出它是在等还是在漏。
// 上限 30 天是防"配一个十年 TTL"把闸门做成永久放行（见 ErrApprovalTTLTooLong）。
const (
	DefaultApprovalRequestTTL = 24 * time.Hour
	MaxApprovalRequestTTL     = 30 * 24 * time.Hour

	approvalSubjectTypeMaxLen = 32
	approvalPolicyKeyMaxLen   = 64
	approvalSubjectIDMaxLen   = 256
)

// ErrApprovalInputInvalid 入参不合法（包裹具体原因，判据用 errors.Is）。
var ErrApprovalInputInvalid = errors.New("approval_request: 入参不合法")

// ErrApprovalTTLTooLong 显式传入的挂起时限超过上限。
//
// 判错而不是夹到上限：静默夹一个越界配置，调用方以为自己挂了 60 天、实际是 30 天，
// 而它对"多久没裁决算放弃"这件事的判断恰恰来自它自己传的那个数（T-P1-08 的同一口径：
// 越界配置比不设配置更危险）。
var ErrApprovalTTLTooLong = fmt.Errorf("approval_request: 挂起时限超过上限 %v", MaxApprovalRequestTTL)

// ErrApprovalIllegalTransition 裁决目标不在状态机上（pending 去不了那个状态）。
// 与 ErrApprovalAlreadyDecided 分开：前者是"这个请求本身不可能被满足"（改代码/改表才会变），
// 后者是"这一行已经落定"（重试或换一条）。混成一个的话，运维会把状态机被改坏
// 当成"审批没人处理"来查，方向完全错。
var ErrApprovalIllegalTransition = errors.New("approval_request: 状态机不允许该跃迁")

// ErrApprovalNotFound 按 ID 查不到审批记录。
var ErrApprovalNotFound = errors.New("approval_request: 审批记录不存在")

// ErrApprovalAlreadyDecided 该记录已落终态，本次裁决**没有**改写它。
//
// 返回时**同时**带回当前记录：拿到这个错的调用方真正要回答的是"那到底批了没有"，
// 而不必再去查一次 —— 少了这份返回值，两个审批人先后点同一行时，后点的人只看到一个
// 笼统的失败，会把"别人已经拒了"当成"服务出错了，我重试一下"。
var ErrApprovalAlreadyDecided = errors.New("approval_request: 该审批已由他人裁决，本次未改写")

// ErrApprovalResumeNotPending 恢复凭证指向的记录还不可恢复（已被拒/过期）。
var ErrApprovalResumeNotPending = errors.New("approval_request: 审批尚未放行，不能恢复")

// ApprovalVerdict 人工裁决结论。
//
// 字面值刻意与目标状态**同一个串**（approved/rejected）：中间再放一张映射表，
// 就多一处"表改了常量没改"的漂移面；映射的正确性由 TestApprovalVerdictMapsToStatus 钉住。
// 注意 verdict 里没有 expired —— 到期不是裁决，走 ExpireOverdue。
type ApprovalVerdict string

const (
	ApprovalApprove ApprovalVerdict = model.ApprovalStatusApproved
	ApprovalReject  ApprovalVerdict = model.ApprovalStatusRejected
)

func (v ApprovalVerdict) targetStatus() (string, bool) {
	switch v {
	case ApprovalApprove, ApprovalReject:
		return string(v), true
	}
	return "", false
}

// ApprovalSubmitInput 一次审批入队。
type ApprovalSubmitInput struct {
	SubjectType string        // 被审对象类型：quote / reach_plan / order_command …
	SubjectID   string        // 被审对象主键
	PolicyKey   string        // 命中策略：谁要求审的（进幂等键）
	TTL         time.Duration // <=0 = DefaultApprovalRequestTTL
}

// normalize 就地整理入参并校验。
//
// 大小写归一只对 subject_type / policy_key 做，**不对 subject_id 做**：后者的取值来自
// 业务主键、可以合法区分大小写（外部平台的订单号就是这种），把它小写化会让两条不同
// 记录看起来是同一个对象 —— 那是把两个不同的审批合成一个，比不做归一危险得多。
//
// 归一必须做（不是洁癖）：这两个字段是幂等键的一部分，"Quote.Send" 与 "quote.send"
// 在库里是两行 pending、两条待办，而策略侧认的是同一个名字。
func (in *ApprovalSubmitInput) normalize() error {
	in.SubjectType = strings.ToLower(strings.TrimSpace(in.SubjectType))
	in.PolicyKey = strings.ToLower(strings.TrimSpace(in.PolicyKey))
	in.SubjectID = strings.TrimSpace(in.SubjectID)

	for _, f := range []struct {
		name  string
		val   string
		maxLn int
	}{
		{"subject_type", in.SubjectType, approvalSubjectTypeMaxLen},
		{"policy_key", in.PolicyKey, approvalPolicyKeyMaxLen},
		{"subject_id", in.SubjectID, approvalSubjectIDMaxLen},
	} {
		if f.val == "" {
			return fmt.Errorf("%w: %s 为空", ErrApprovalInputInvalid, f.name)
		}
		if len(f.val) > f.maxLn {
			return fmt.Errorf("%w: %s 长度 %d 超上限 %d", ErrApprovalInputInvalid, f.name, len(f.val), f.maxLn)
		}
	}
	// subject_type / policy_key 参与唯一索引与后续按名字取策略，字符集收窄到
	// [a-z0-9_.-]：空格、逗号、竖线这些字符会直接进入闸门基线的正则匹配面（见
	// scripts/check-unwired-assets.sh 按 | 分列的口径），留一个自由字符串进去，
	// 迟早有人用 "a|b" 当策略名，把一行登记劈成两行。
	for _, f := range []struct{ name, val string }{{"subject_type", in.SubjectType}, {"policy_key", in.PolicyKey}} {
		for _, r := range f.val {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '-' {
				return fmt.Errorf("%w: %s 含非法字符 %q（只允许小写字母、数字、_ . -）", ErrApprovalInputInvalid, f.name, r)
			}
		}
	}
	return nil
}

// AutoApprovalPolicy auto-approve 快速路径的判据来源（C2 的同步退化态）。
//
// 返回 (放行, 理由)。理由会原样写进 decision_note —— 事后要能回答"这条为什么没人看就过了"。
// 判据与理由**必须同源一次求得**（返回值成对，而不是"先判一下、再问一次为什么"）：
// 分两次读旗子会在翻转的瞬间给出 allowed=true 且 reason=disabled_by_flag 这种
// 自相矛盾的组合，T-P1-06 在审批门上就是这么修的。
//
// 这里**刻意没有**"让普通函数当策略用"的私有适配器：`golangci-lint` 的 unused 按
// `run.tests=false` 只数生产调用点，而本服务今天在生产侧零构造（闸门项12a），
// 一个只有测试在用的适配器必然被判死代码。真到装配那天，由那张卡按自己的入参形状决定
// 是造壳类型还是自带适配器 —— 现在造了也是替它猜。
type AutoApprovalPolicy interface {
	AutoApproves(ctx context.Context, in ApprovalSubmitInput) (allow bool, reason string)
}

// 时钟与凭证生成走包级函数变量，判据同 checkpointEnabledFn（测试可替换、生产走默认）。
//
// 前两个的读写只走紧随其后的四扇 accessor（approvalSeamMu 守，包内其余位置直读 0 处）：
// Submit 挂在入站闸门链上、Decide/ExpireOverdue 挂在 cron 的 RunOnce 协程上，两处读的都是
// 全局地址本身，而审批用例逐格换时钟。approvalRequestSeq/IDFn 不在门口径里：前者只走 atomic，
// 后者今日无用例改写（脚本按"测试会写"取候选）。
var (
	approvalSeamMu sync.RWMutex

	approvalNowFn       = time.Now
	approvalResumeTokFn = newApprovalResumeToken
	approvalRequestSeq  int64
	approvalRequestIDFn = newApprovalRequestID
)

func loadApprovalNowFn() func() time.Time {
	approvalSeamMu.RLock()
	defer approvalSeamMu.RUnlock()
	return approvalNowFn
}

func storeApprovalNowFn(fn func() time.Time) {
	approvalSeamMu.Lock()
	defer approvalSeamMu.Unlock()
	approvalNowFn = fn
}

func loadApprovalResumeTokFn() func() (string, error) {
	approvalSeamMu.RLock()
	defer approvalSeamMu.RUnlock()
	return approvalResumeTokFn
}

func storeApprovalResumeTokFn(fn func() (string, error)) {
	approvalSeamMu.Lock()
	defer approvalSeamMu.Unlock()
	approvalResumeTokFn = fn
}

func newApprovalRequestID() string {
	n := atomic.AddInt64(&approvalRequestSeq, 1)
	return fmt.Sprintf("apr_%d_%d", loadApprovalNowFn()().UnixNano(), n)
}

// newApprovalResumeToken 生成恢复凭证。
//
// 用 crypto/rand 而不是复用业务 ID：ID 会进日志与列表，能推出 ID 就能推别人的挂起凭证。
// 生成失败一律上抛（不降级成"那就先不发 token"）：pending 记录没有凭证就永远唤不醒，
// 一条唤不醒的挂起审批比一次入队失败坏得多 —— 失败会重试，卡死不会。
func newApprovalResumeToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("approval_request: 生成恢复凭证失败: %w", err)
	}
	return "rt_" + hex.EncodeToString(b[:]), nil
}

// ApprovalWaitNotifier 一条审批落定后该通知谁（T-P3-02 的出口，由 SOP 侧实现）。
//
// 形状上刻意只有"通知"而没有"注册等待者"：等待关系本来就在流程那一侧的记录里
// （SOP 是 `sop_timers` 那一行 pending），本服务再存一份就是第二个事实源，
// 两份不一致时永远是谁先动谁说了算 —— 那正是 C2 要合掉的那类双源。
//
// 无返回值是刻意的：**推送只是省时间，不是正确性的一部分**。正确性走"点火时回读结论"
// （见 sop_approval_resume.go 的 ResolveOnFire）：进程在裁决那一刻正好不在，
// 通知丢了也不要紧，定时器到自己的 wait_until 仍会把流程叫醒并读到真实结论。
// 反过来若把它做成 error 上抛，裁决就会因为"叫不醒某个流程"而失败 ——
// 人的裁决已经落库了，绝不能因为唤醒故障被回滚。
type ApprovalWaitNotifier interface {
	NotifyDecided(ctx context.Context, req *model.ApprovalRequest)
}

// ApprovalTaskSink 审批与统一待办池之间唯一的出口（T-P3-04）。
//
// 只有两个方法，且刻意不叫 SubmitTask/CancelTask 那样的动作名：投什么类别、标题写什么、
// 截止落哪一列、done 还是 cancelled 全由实现方（HumanTaskService）按待办域的规矩定，
// 审批侧只负责在正确的时刻递上"这一行现在长什么样"。
//
// 与 ApprovalWaitNotifier 同样的装配方式（setter + nil = 不接线），因为两者的
// 可用性前提不同：待办底座没有旗子，而审批运行时受 FF_LTC_APPROVAL_RESUME 控制。
type ApprovalTaskSink interface {
	// SubmitForApproval 为一条 pending 审批投递（或复用）那条待办。
	SubmitForApproval(ctx context.Context, req *model.ApprovalRequest) error
	// CloseForApproval 收掉一条已落定审批的待办。
	CloseForApproval(ctx context.Context, req *model.ApprovalRequest) error
}

// ApprovalRequestService 审批检查点服务
type ApprovalRequestService struct {
	repo   repository.ApprovalRequestRepository
	policy AutoApprovalPolicy // nil = 永不自动放行（全部走人工，最保守的一档）

	// notifierMu 保护 notifier 与 taskSink：两个装配期写入的出口，读发生在
	// 裁决期（HTTP 线程 / worker 线程），与写入没有先后保证 ——
	// 无锁读写是数据竞争（-race 可复现），口径同 SOPExecutionDispatcher.compensationMu。
	notifierMu sync.RWMutex
	notifier   ApprovalWaitNotifier // nil = 无人等（本服务可独立使用，见 T-P3-01 的零装配）
	taskSink   ApprovalTaskSink     // nil = 不进待办池（装配未接齐时的默认档）
}

// NewApprovalRequestService 构造。policy 可为 nil（见上）。
func NewApprovalRequestService(repo repository.ApprovalRequestRepository, policy AutoApprovalPolicy) *ApprovalRequestService {
	return &ApprovalRequestService{repo: repo, policy: policy}
}

// globalApprovalSvc 全局审批服务实例（与 globalHumanTaskSvc 同一口径：atomic.Pointer）。
//
// 为什么现在要有全局：T-P3-04 的裁决端点在 router 包里，而 router 拿服务的既有形状
// 就是"现取全局、取不到就回 503"（见 setupHumanTaskRoutes）。装配在 app 的
// InitApprovalRuntime 里做，off / 无 DB 句柄两档都必须把它清成 nil ——
// 留着上一份会让端点对着一个已经撤掉运行时实例继续回 200（T-P3-02 ⑤(b) 同一类缺陷）。
var globalApprovalSvc atomic.Pointer[ApprovalRequestService]

// SetGlobalApprovalRequestService 登记全局实例；传 nil 等于撤掉。
func SetGlobalApprovalRequestService(s *ApprovalRequestService) { globalApprovalSvc.Store(s) }

// GlobalApprovalRequestService 取全局实例（可能为 nil；调用方必须判空并给出"未装配"的答复）。
func GlobalApprovalRequestService() *ApprovalRequestService { return globalApprovalSvc.Load() }

// Available 底座可用（controller 用它区分 503 与业务错误，口径同 HumanTaskService.Available）。
//
// nil 接收者要返回 false 而不是 panic：路由层拿到全局实例时可能压根没装配
// （off 档 / 无 DB），那边构造 controller 时传的就是 nil，判空必须能在 nil 上做。
func (s *ApprovalRequestService) Available() bool {
	return s != nil && s.repo != nil && s.repo.Available()
}

// SetWaitNotifier 装配唤醒出口。传 nil 等于撤掉（回滚到"只靠定时器到期"那一档）。
//
// 装配点：internal/app/approval_runtime_wiring.go。刻意允许在 Start 之后调用，
// 因为审批服务要先于 SOP 调度器存在、而桥接器要拿到调度器才能唤醒（两者构造顺序相反）。
func (s *ApprovalRequestService) SetWaitNotifier(n ApprovalWaitNotifier) {
	if s == nil {
		return
	}
	s.notifierMu.Lock()
	s.notifier = n
	s.notifierMu.Unlock()
}

func (s *ApprovalRequestService) waitNotifier() ApprovalWaitNotifier {
	s.notifierMu.RLock()
	defer s.notifierMu.RUnlock()
	return s.notifier
}

// notifyWait 把结论推给等待方；未装配时静默（那是默认档，不是故障）。
func (s *ApprovalRequestService) notifyWait(ctx context.Context, req *model.ApprovalRequest) {
	if s == nil || req == nil {
		return
	}
	n := s.waitNotifier()
	if n == nil {
		return
	}
	n.NotifyDecided(ctx, req)
}

// SetTaskSink 装配待办池出口；传 nil 等于撤掉（回到 T-P3-02 的交付态：审批只落自己的表）。
//
// 装配点：internal/app/approval_runtime_wiring.go。允许在 Start 之后调用，
// 因为待办底座与审批运行时各自的装配顺序不固定（前者无旗子、后者有）。
func (s *ApprovalRequestService) SetTaskSink(sink ApprovalTaskSink) {
	if s == nil {
		return
	}
	s.notifierMu.Lock()
	s.taskSink = sink
	s.notifierMu.Unlock()
}

func (s *ApprovalRequestService) sink() ApprovalTaskSink {
	if s == nil {
		return nil
	}
	s.notifierMu.RLock()
	defer s.notifierMu.RUnlock()
	return s.taskSink
}

// deliverTask 投待办。错误**必须上抛**给 Submit 的调用方。
//
// 失败方向选得很具体：待办没投出去 ⇒ 这条 pending 大概永远没人看见 ⇒ 挂起必须失败
// （流程节点判失败、执行按失败收口，既不会放行危险动作，也不会把执行永久吊在 running）。
// 反过来"只记日志继续挂起"会造出一个更坏的状态：流程挂着、池子里看不见、
// 只能等 TTL 自己到期，而那看起来像"审批没人理"而不是"待办没投出去"。
func (s *ApprovalRequestService) deliverTask(ctx context.Context, req *model.ApprovalRequest) error {
	sink := s.sink()
	if sink == nil {
		return nil
	}
	if err := sink.SubmitForApproval(ctx, req); err != nil {
		return fmt.Errorf("approval_request: 审批 %s 的待办投递失败（审批行仍在库里，下次入队会补投）: %w", req.ID, err)
	}
	return nil
}

// closeTask 收待办。失败**只出声，不改变裁决的结果**。
//
// 与 deliverTask 方向相反，理由同一条判据（只有已经成立的事实能让流程继续）：
// 裁决此刻已落库、唤醒已发出，这里回一个 error 会让操作者以为自己没批成而去重试，
// 而第二次拿到的是 409"已由他人裁决"。残留是一行没人收的死待办：它会被看见、会逾期，
// 处理它的人点下去会拿到 409 并刷新列表 —— 难受，但不假。
func (s *ApprovalRequestService) closeTask(ctx context.Context, req *model.ApprovalRequest) {
	sink := s.sink()
	if sink == nil {
		return
	}
	if err := sink.CloseForApproval(ctx, req); err != nil {
		logger.Warnf("[approval_request] 审批 %s（%s）已落定为 %s，但它的待办没收口：%v",
			req.ID, req.PolicyKey, req.Status, err)
	}
}

// Submit 入队一次审批请求。
//
// 返回 (记录, 是否本次新建, error)：中间那个 bool 是 AC③ 的可见形式 ——
// "幂等"不等于"调用方拿到的东西和上次一样"，调用方还需要知道**有没有多出一条待办**。
// false 的含义很具体：库里已有一条 pending 在等同一件事的裁决，本次没有新增。
//
// 三步顺序是有讲究的（先查 pending → 问策略 → 落库）：
//   - 先查后问：已有 pending 时**不再问策略**。策略可能在两次入队之间被改过（白名单刚加了
//     这个账号），此时把老 pending 就地翻成 approved 等于"没人裁决却自己批了"，
//     而且待办已经发出去了，人工侧会看到一条凭空消失的待办。要改判就先由人裁决这一条。
//   - 问策略放在落库之前：auto-approve 的记录是**以 approved 落库**的，不是"先 pending 再改"。
//     后者会留一个极短的 pending 窗口，而清扫 worker 正好能在那个窗口里把它过期掉。
//
// 并发不加进程内锁：跨副本互斥只可能由部分唯一索引 uq_approval_request_open 提供，
// 进程内的锁给不了它、只会给出"单机测过了"的错觉。同进程撞车的后果与跨副本完全一样，
// 都由下面那条冲突重查路兜住（有专测：Insert 先撞 23505、重查拿回对手那一行）。
func (s *ApprovalRequestService) Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, false, errors.New("approval_request service: 未接仓储或句柄不可用")
	}
	if err := in.normalize(); err != nil {
		return nil, false, err
	}
	ttl := in.TTL
	if ttl == 0 {
		ttl = DefaultApprovalRequestTTL
	}
	if ttl < 0 {
		return nil, false, fmt.Errorf("%w: TTL 为负（%v）—— 要默认值请传 0，别传负数", ErrApprovalInputInvalid, in.TTL)
	}
	if ttl > MaxApprovalRequestTTL {
		return nil, false, ErrApprovalTTLTooLong
	}
	now := loadApprovalNowFn()()

	// 幂等：同一 (subject, policy) 已有 pending 就复用那一条（连 token 都保持原值，
	// 否则挂起流程手里那份凭证会莫名其妙失效）。
	existing, err := s.repo.GetPendingBySubject(ctx, in.SubjectType, in.SubjectID, in.PolicyKey)
	if err != nil {
		// 读故障**不能**当成"没有 pending"：那样会再建一条，于是同一件事在待办中心出现两条，
		// 而且两条各自能被独立批准 —— 一次批准只该放行一次动作。
		return nil, false, fmt.Errorf("approval_request: 查已有 pending 失败: %w", err)
	}
	if existing != nil {
		// 复用路径也要保证待办在（T-P3-04）：第一次入队可能碰上"审批落库成功、
		// 待办投递失败"（见 deliverTask 的失败方向），重试这一次就是它唯一的补投机会。
		if existing.Status == model.ApprovalStatusPending {
			if err := s.deliverTask(ctx, existing); err != nil {
				return nil, false, err
			}
		}
		return existing, false, nil
	}

	auto, reason := false, ""
	if s.policy != nil {
		auto, reason = s.policy.AutoApproves(ctx, in)
	}

	req := &model.ApprovalRequest{
		ID:          approvalRequestIDFn(),
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
		PolicyKey:   in.PolicyKey,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if auto {
		// AC② 同步退化态：入队即已批准，调用方原地拿结果、不需要轮询。
		req.Status = model.ApprovalStatusApproved
		req.DecidedBy = model.ApprovalDecidedByPolicy
		req.DecidedAt = &now
		req.DecisionNote = strings.TrimSpace(reason)
		if req.DecisionNote == "" {
			// 空理由不是"没理由"，是策略侧懒得说：把它写成可 grep 的字面值，
			// 免得审计里出现一条 decided_by=policy:auto、note 空白的记录，
			// 事后既看不出谁放的、也看不出是不是所有自动放行都这么含糊。
			req.DecisionNote = "auto_unspecified"
		}
	} else {
		token, terr := loadApprovalResumeTokFn()()
		if terr != nil {
			return nil, false, terr
		}
		req.Status = model.ApprovalStatusPending
		req.ResumeToken = token
		exp := now.Add(ttl)
		req.ExpiresAt = &exp
	}

	if ierr := s.repo.Insert(ctx, req); ierr != nil {
		if !errors.Is(ierr, repository.ErrApprovalPendingConflict) {
			return nil, false, ierr
		}
		// 撞车：另有人（同进程另一协程或另一副本）在"查"与"插"之间先落了一条 pending。
		// 重查一次拿对手那一行返回 —— 幂等的正确形状是"返回同一条"，不是"再插一条"。
		winner, werr := s.repo.GetPendingBySubject(ctx, in.SubjectType, in.SubjectID, in.PolicyKey)
		if werr != nil {
			return nil, false, fmt.Errorf("approval_request: 冲突后重查失败: %w", werr)
		}
		if winner == nil {
			// 冲突说"有 pending"、重查说"没有"：两种读在极短窗口里各看到了真相的一半
			// （对手那一行刚被裁决掉）。此时**不新建**，把冲突原样上抛让调用方重试 ——
			// 闸门在这里的失败方向必须是"这次动作没拿到批准"，而不是"再开一条待办"。
			return nil, false, ierr
		}
		if winner.Status == model.ApprovalStatusPending {
			if err := s.deliverTask(ctx, winner); err != nil {
				return nil, false, err
			}
		}
		return winner, false, nil
	}
	// 待办投递放在**落库之后**：反过来会造出"池子里有一条待办、库里没有对应审批"的孤儿，
	// 而点开它的人什么也批不了。落定态（auto-approve）不投——没有人要裁决它。
	if req.Status == model.ApprovalStatusPending {
		if err := s.deliverTask(ctx, req); err != nil {
			return nil, false, err
		}
	}
	return req, true, nil
}

// Decide 人工裁决一条 pending 审批。
//
// 只有 pending 可被裁决（见 model.ApprovalTransitionAllowed）：已批准的不能再改判、
// 已拒的也不允许在这里翻回来 —— 要重审就对新的一次动作重新 Submit，留下两条记录。
//
// 落败方（并发下别人先批了）拿到的是 ErrApprovalAlreadyDecided **加上当前记录**，
// 调用方因此能直接回答"那件事到底批没批"。
func (s *ApprovalRequestService) Decide(ctx context.Context, id string, verdict ApprovalVerdict, decidedBy, note string) (*model.ApprovalRequest, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, errors.New("approval_request service: 未接仓储或句柄不可用")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: id 为空", ErrApprovalInputInvalid)
	}
	target, ok := verdict.targetStatus()
	if !ok {
		// 空 verdict 与拼错的一律拒：让 "" 落到某个默认分支，等于"点了提交但什么都没批"。
		return nil, fmt.Errorf("%w: 裁决结论 %q 不是 approved/rejected", ErrApprovalInputInvalid, string(verdict))
	}
	decidedBy = strings.TrimSpace(decidedBy)
	if decidedBy == "" {
		return nil, fmt.Errorf("%w: 缺少裁决者（谁批的必须留得下来）", ErrApprovalInputInvalid)
	}
	if model.ApprovalDecidedAutomatically(decidedBy) {
		// Decide 是**人工**裁决入口。把 policy:auto / system:ttl 从这里写进去，
		// 等于伪造一条"人批过了"的记录 —— 而这两个值正是自动放行率与超时率的统计口径所在。
		return nil, fmt.Errorf("%w: 裁决者 %q 是系统保留值", ErrApprovalInputInvalid, decidedBy)
	}
	// 跃迁表在这里上写路径：本服务只可能从 pending 起改（仓储的 CAS 条件保证），
	// 所以判的是"从 pending 去 target 合不合法"。
	//
	// 它**不**用来读行当前的真实状态（那是 CAS 的活，见下面的 applied 分支），
	// 它拦的是另一件事：有人往表里放开 pending→某个不该存在的目标、或把
	// pending→rejected 整条删掉。前者让一次合法裁决把行改成表外状态，后者让"拒"这条路
	// 静默失效 —— 两种都只有一行表数据变了，代码看不出问题，只能靠这条判据变红。
	// 变异实测：把表里 pending→rejected 摘掉，Decide(ApprovalReject) 立刻在落库前失败。
	if !model.ApprovalTransitionAllowed(model.ApprovalStatusPending, target) {
		return nil, fmt.Errorf("%w: 状态机不允许 %s → %s", ErrApprovalIllegalTransition,
			model.ApprovalStatusPending, target)
	}

	applied, err := s.repo.MutatePending(ctx, id, func(a *model.ApprovalRequest) {
		a.Status = target
		a.DecidedBy = decidedBy
		a.DecisionNote = strings.TrimSpace(note)
		at := loadApprovalNowFn()()
		a.DecidedAt = &at
	})
	if err != nil {
		return nil, err
	}
	if applied {
		cur, gerr := s.repo.GetByID(ctx, id)
		if gerr != nil {
			return nil, gerr
		}
		if cur == nil {
			// 刚写成功就查不到，只能是有人在同一瞬间删了行。返回错误而不是 nil,nil：
			// 调用方拿着 nil 无从判断裁决有没有生效，而它下一步就是"要不要真的外发"。
			return nil, ErrApprovalNotFound
		}
		// 先收待办、再叫醒流程：反过来会让操作者在"已经批了"的那个瞬间还在池子里
		// 看见一条能点的待办（点下去拿到 409，而 409 在这里读起来像权限问题）。
		// 收口失败不上抛，理由见 closeTask。
		s.closeTask(ctx, cur)
		// 唤醒挂在这件事上的流程（T-P3-02）。放在**读回之后**：通知出去的那一刻，
		// 被叫醒的一方会立刻回读这一行，它必须读到刚落的结论而不是旧值。
		s.notifyWait(ctx, cur)
		return cur, nil
	}

	cur, gerr := s.repo.GetByID(ctx, id)
	if gerr != nil {
		return nil, gerr
	}
	if cur == nil {
		return nil, ErrApprovalNotFound
	}
	return cur, ErrApprovalAlreadyDecided
}

// Get 按 ID 读一条审批。
// 不存在 (nil, nil)；读故障 error —— 两者的差别调用方必须能分辨（同 T-P2-06 的口径）。
func (s *ApprovalRequestService) Get(ctx context.Context, id string) (*model.ApprovalRequest, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, errors.New("approval_request service: 未接仓储或句柄不可用")
	}
	return s.repo.GetByID(ctx, strings.TrimSpace(id))
}

// ByResumeToken 挂起流程凭恢复凭证查自己的审批结论。
//
// 返回 (记录, 是否可续跑, error)：可续跑 = 已批准。查不到 (nil, false, nil)。
// 这里**不**把"还没批"报成 error：等待是这条路径的正常态，报成错误会让调用方
// 写出一堆 retry，把一次挂起变成一次轮询风暴（C2 要的是"不阻塞、不轮询"）。
func (s *ApprovalRequestService) ByResumeToken(ctx context.Context, token string) (*model.ApprovalRequest, bool, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, false, errors.New("approval_request service: 未接仓储或句柄不可用")
	}
	a, err := s.repo.GetByResumeToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return nil, false, err
	}
	if a == nil {
		return nil, false, nil
	}
	switch a.Status {
	case model.ApprovalStatusApproved:
		return a, true, nil
	case model.ApprovalStatusPending:
		// 等待是这条路径的正常态：返回 (行,false,nil)，由调用方决定要不要继续挂着。
		//
		// 这里**不**再校验"pending 一定有凭证"：命中这条路意味着库里那一行的
		// resume_token 就等于刚查过的那个非空串（同一句 WHERE resume_token = ?），
		// 判据恒真。真正的不变量在写入侧 —— 凭证生成失败时 Submit 直接报错、不落库。
		return a, false, nil
	default: // rejected / expired / 库里出现未知状态
		return a, false, ErrApprovalResumeNotPending
	}
}

// ExpireOverdue 把一批到期仍未裁决的 pending 翻成 expired，返回**实际被翻转的那些行**。
//
// 返回行而不是计数（T-P3-02 改的签名）：到期不是"少了一条待办"就完事 ——
// 挂在它上面的流程同样需要被告知"这件事不会再有人批了"，否则 SOP 会一直等到
// 定时器自己的 wait_until（两者虽然同源于同一个 expires_at，但唤醒路径必须存在，
// 不然"到期"在流程侧读起来与"还没批"没有区别）。计数派不出这个用途。
//
// limit<=0 = 本轮不限（装配侧应传有界值，理由同 recovery worker 的单轮上限：
// 一次清理的最坏耗时与锁范围必须有上界）。
// 这一格状态必须由某个调用方按节拍来推（T-P2-06 在 PurgeTerminal 上刚踩过：
// 常量写了、没人调，等于没有保留期）⇒ 定时调用方就在本卡的装配里
// （internal/app/approval_runtime_wiring.go 的清扫协程），T-P3-01 登记的那条
// "pending 不会自动过期"的移交项到这里收口。
func (s *ApprovalRequestService) ExpireOverdue(ctx context.Context, limit int) ([]*model.ApprovalRequest, error) {
	if s == nil || s.repo == nil || !s.repo.Available() {
		return nil, errors.New("approval_request service: 未接仓储或句柄不可用")
	}
	flipped, err := s.repo.ExpirePendingBatch(ctx, loadApprovalNowFn()(), limit)
	if err != nil {
		return nil, err
	}
	for _, row := range flipped {
		s.closeTask(ctx, row)
		s.notifyWait(ctx, row)
	}
	return flipped, nil
}

// AllowedTransitions 暴露状态机给待办中心/审计视图（"这条还能被改成什么"）。
func (s *ApprovalRequestService) AllowedTransitions(status string) []string {
	return model.ApprovalTransitionTargets(strings.ToLower(strings.TrimSpace(status)))
}
