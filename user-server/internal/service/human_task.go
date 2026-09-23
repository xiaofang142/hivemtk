// human_task.go 统一待办服务（T-P3-03 / N-9）。
//
// 本层是三件事的唯一落点：
//  1. **写入闸门**：kind/身份/长度在这里判，落到仓储就晚了 —— 仓储只认列名与索引，
//     一行 kind 拼错的待办会永远不出现在任何一类的读数里（AC③ 的过滤救不了它）。
//  2. **幂等**：同一件事的第二次投递回到同一条开放待办（AC②）。这件事由
//     uq_human_task_open 在库里兜底、由本层把它翻成"复用那一行"而不是"报错"。
//     转人工那条调用方是**不许**因为待办已存在而失败的。
//  3. **跃迁**：动作合法性只认 model.HumanTaskTransitionAllowed 一张表（三类共用），
//     本层不写第二个状态机；仓储的 CAS 保证"判过的那一刻"与"写下去的那一刻"是同一刻。
//
// 刻意没有内存版底座、也没有批量导入接口：待办的意义是"进程外还记着这件事"。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// HumanTaskConfigReader 动态参数读取口（*ConfigParamService 的取数子集）。
//
// 只暴露GetInt一个方法而不是整个服务：本服务今天要的只有一个分钟数，
// 拿一整个配置服务进来，将来就会有人顺手往这里加第二个、第三个键，
// 而那张表就不再是"参数的读取处"而是"参数的定义处"了。
type HumanTaskConfigReader interface {
	GetInt(ctx context.Context, group, key string, fallback int) int
}

// 对外错误。四个出口各对应一个 HTTP 语义 —— 混成一个（尤其是把 409 也报成 500）
// 会让前端无法区分"这条已经被别人处理了"与"服务出故障了"，前者该刷新列表，后者不该。
var (
	// ErrHumanTaskInputInvalid 入参不合法（400）。与仓储同一 sentinel：
	// 校验分散在两层是历史事实，但对调用方来说它们都是"你给的东西不对"。
	ErrHumanTaskInputInvalid = repository.ErrHumanTaskInputInvalid
	// ErrHumanTaskNotFound 没有这条待办（404）。
	ErrHumanTaskNotFound = errors.New("human_task: 待办不存在")
	// ErrHumanTaskTransition 这一类/这个状态上不允许该动作（409）。
	ErrHumanTaskTransition = errors.New("human_task: 当前状态不支持该操作")
	// ErrHumanTaskNotHolder 动作只能由当前认领人发起（403）。
	ErrHumanTaskNotHolder = errors.New("human_task: 只有当前认领人可以执行该操作")
	// ErrHumanTaskLost 判据过了但写回时那一行已被他人改写（409，重试即可）。
	// 与 ErrHumanTaskTransition 分开的理由：这一条是"再来一次就有答案"，
	// 那一条是"再来一万次也不会有答案"。
	ErrHumanTaskLost = errors.New("human_task: 待办已被他人更新，请重新查看")
	// ErrHumanTaskOpenConflict 同一对象的开放待办撞在唯一索引上（409）。
	// 与仓储同一 sentinel（别名，理由同 ErrHumanTaskInputInvalid）：Submit 在绝大多数
	// 路径上把它吃成"复用那一行"（AC②），只剩"索引拦了、回读却为空"这一条窄缝会把它
	// 原样交出去 —— 那里调用方需要的是"这次没落进去，重试"，报成 500 会让前端在
	// 底座其实健康时无限打圈。
	ErrHumanTaskOpenConflict = repository.ErrHumanTaskOpenConflict
)

// HumanTaskListQuery 列表/聚合的查询条件（对仓储同类型的别名）。
//
// 别名而不是照抄一份结构体：HTTP 层必须能构造这个查询，而架构门禁止 controller
// 引用 repository（第五层不许出现在第三层的 import 里）。照抄一份会在两层之间
// 留下一个需要手工同步的形状；别名让"对外用哪个名字"只有一处定义。
type HumanTaskListQuery = repository.HumanTaskQuery

const (
	// HumanTaskConfigGroup / HumanTaskHandoffSlaKey 会话首响 SLA 的动态参数坐标。
	// 导出是为了 seed 注册、文档与用例三处指同一个键（键名打错不会报错，只会永远读默认值）。
	HumanTaskConfigGroup          = "human_task"
	HumanTaskHandoffSlaKey        = "handoff_first_response_minutes"
	MaxHumanTaskHandoffSlaMinutes = 24 * 60

	// 入参上限。text 列本身不限长，上限是给"调用方写错了"这件事设的：
	// 一条 200 万字的 Reason 与一条 2000 字的 Reason 在处理上没有任何差别，
	// 但前者能把待办列表的响应体撑爆（列表是整行返回的）。
	humanTaskSubjectTypeMaxLen  = 32 // 与列宽 varchar(32) 同
	humanTaskSubjectIDMaxLen    = 256
	humanTaskTitleMaxLen        = 200
	humanTaskReasonMaxLen       = 2000
	humanTaskPayloadRefMaxLen   = 512
	humanTaskOneIDMaxLen        = 100 // 与列宽 varchar(100) 同
	humanTaskOperatorMaxLen     = 100
	humanTaskCancelReasonMaxLen = humanTaskReasonMaxLen
)

// DefaultHumanTaskHandoffSlaMinutes 配置缺失/越界时的会话首响 SLA（分钟）。
//
// 为什么不走既有的 sla_policies 表：那张表（model.SLAPolicy / service.SLAService.AddPolicy）
// 在本仓**没有任何持久化与调用方**（T-P3-03 实测：全仓零构造点，闸登记在
// scripts/check-unwired-assets.sh 的未接线项里）。把待办的 SLA 挂在一个没人写的表上，
// 等于待办永远读不到截止。这里改用 config_params（有 seed、有管理端 CRUD、有读取缓存），
// 并在移交清单里记下"若将来要按坐席/技能组分别定 SLA，正确落点是 sla_policies + 写入方"。
const DefaultHumanTaskHandoffSlaMinutes = 5

// HumanTaskSubjectCustomerSession 会话待办的 subject_type 字面量。
//
// 导出是为了三处指同一个字符串：转人工投递（本卡）、会话关闭时的撤销钩子（本卡）、
// 以及闸机基线里那一列。
const HumanTaskSubjectCustomerSession = "customer_session"

// HumanTaskSubjectApprovalRequest 审批类待办的 subject_type 字面量（T-P3-04）。
//
// 它的 subject_id 是 approval_requests.id（不是被审对象的二元组）：待办与审批必须
// 一对一，而"同一次动作重新 Submit"在审批侧本来就会新建一行（改判无路），
// 于是待办也跟着新建一条 —— 用被审对象做键会让第二次入队复用第一次那条已关闭的待办。
const HumanTaskSubjectApprovalRequest = "approval_request"

// HumanTaskSubjectCollectionCase 催收升级待办的 subject_type 字面量（T-P7-03）。
//
// 它的 subject_id 是 **bills.id**（那一张应收），不是商机也不是报价：升级要人去看的是
// "这一笔钱还没收到"，而一张账单一生只会被升级到一次（升级窗口在 collection_job 侧另算，
// 因为 Submit 的幂等只覆盖"还在 open 的那条"）。
//
// 这一格从"没有生产投递方"变成有投递方，是 T-P7-01 立「登记一个没人用的字符串
// 就是留一份假象」那条判据时等的那个东西：常量跟着第一个真实写路径一起落地，不提前占位。
const HumanTaskSubjectCollectionCase = "collection_case"

// HumanTaskSubmitInput 投递一条待办。
//
// 只有身份与展示三件事由调用方给，**时刻与状态都由本层定**：
// 调用方能给的只有"这件事关于哪条记录"，其余（ID、kind 落哪一列、初始态）
// 一旦开放给调用方，就会有两套写法在同一张表里相遇。
type HumanTaskSubmitInput struct {
	Kind        string // 三类之一（大小写与空白会被归一）
	SubjectType string // customer_session / approval_request / collection_case
	SubjectID   string // 那条业务记录的 ID（不做大小写归一：外部主键可以合法区分大小写）
	Title       string // 待办中心那一行字（可空）
	Reason      string // 为什么要人工（可空，但转人工那一路一定会给）
	PayloadRef  string // 处理时要去看的位置（可空）
	OneID       string // 归一后的客户身份（可空）

	// SlaDueAt 这件事的截止时刻。
	// approval / collection_escalation **必填**（截止在各自的业务表里，本层无从推算）；
	// conversation_handoff **必空**（由本层按配置统一给，见 slaFor 的理由）。
	SlaDueAt *time.Time
}

// normalize 就地整理并校验入参。
//
// 长度一律按**字符数**算而不是字节数：标题与原因大量是中文，按字节判会把
// 一句 70 个汉字的正常标题（210 字节）拒成"超长"，而 PG 的 varchar(32) 数的也是字符。
func (in *HumanTaskSubmitInput) normalize() error {
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.SubjectType = strings.ToLower(strings.TrimSpace(in.SubjectType))
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	in.Title = strings.TrimSpace(in.Title)
	in.Reason = strings.TrimSpace(in.Reason)
	in.PayloadRef = strings.TrimSpace(in.PayloadRef)
	in.OneID = strings.TrimSpace(in.OneID)

	if !model.HumanTaskKindKnown(in.Kind) {
		return fmt.Errorf("%w: kind %q 不在三类值域里（未知类别的待办不会出现在任何一类的读数里）",
			ErrHumanTaskInputInvalid, in.Kind)
	}
	// 上限分两档，两档的处置不一样：
	//   - **身份字段越界即拒**（subject_type / subject_id / payload_ref / one_id）：
	//     这些字段决定"这条待办关于哪件事"，裁断等于把待办挂到另一件事上；
	//   - **展示字段越界只裁断**（title / reason）：拒收的代价是"这条待办压根不存在"，
	//     而这两个字段只负责"给人看得懂"。转人工的原因来自上游文案，长度不由我们定。
	for _, f := range []struct {
		name  string
		val   string
		maxLn int
	}{
		{"subject_type", in.SubjectType, humanTaskSubjectTypeMaxLen},
		{"subject_id", in.SubjectID, humanTaskSubjectIDMaxLen},
		{"payload_ref", in.PayloadRef, humanTaskPayloadRefMaxLen},
		{"one_id", in.OneID, humanTaskOneIDMaxLen},
	} {
		if utf8.RuneCountInString(f.val) > f.maxLn {
			return fmt.Errorf("%w: %s 长度 %d 超上限 %d",
				ErrHumanTaskInputInvalid, f.name, utf8.RuneCountInString(f.val), f.maxLn)
		}
	}
	in.Title = humanTaskClip(in.Title, humanTaskTitleMaxLen)
	in.Reason = humanTaskClip(in.Reason, humanTaskReasonMaxLen)
	// subject_type 与 subject_id 的区别是硬的：前者参与唯一索引、并且会被写进
	// 闸门基线（scripts/check-unwired-assets.sh 按 | 分列），字符集必须收窄；
	// 后者是外部主键，形态由上游决定，收窄就会把合法的 ID 拒掉。
	if in.SubjectType == "" {
		return fmt.Errorf("%w: subject_type 为空", ErrHumanTaskInputInvalid)
	}
	for _, r := range in.SubjectType {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '-' {
			return fmt.Errorf("%w: subject_type 含非法字符 %q（只允许小写字母、数字、_ . -）", ErrHumanTaskInputInvalid, r)
		}
	}
	if in.SubjectID == "" {
		return fmt.Errorf("%w: subject_id 为空（没有身份的待办既查不到也复用了不了）", ErrHumanTaskInputInvalid)
	}
	return nil
}

// humanTaskClip 按**字符**裁断展示文本。
//
// 不能按字节切：varchar 与列表页数的都是字符，按字节砍在半个汉字上会留下一个
// U+FFFD，而那正是这一行要被显示出来的地方。
func humanTaskClip(v string, maxLn int) string {
	if utf8.RuneCountInString(v) <= maxLn {
		return v
	}
	return string([]rune(v)[:maxLn])
}

// humanTaskPlaceSLA 把截止时刻放进该 kind 唯一那一档。
//
// 这是"三类共用一张表但 SLA 字段独立"（AC①）在写入侧的**唯一**落点：
// 调用方只说"什么时候算慢"，不说"写哪一列"，于是填错列在这条路径上不可表达。
// 反过来，如果哪天加第四档而忘了改这里（或忘了改 model.HumanTaskSLAField），
// default 分支会报错，而不是静默把截止丢在一边。
func humanTaskPlaceSLA(t *model.HumanTask, col string, due time.Time) error {
	at := due
	switch col {
	case "sla_first_response_at":
		t.SlaFirstResponseAt = &at
	case "sla_decide_at":
		t.SlaDecideAt = &at
	case "sla_escalate_at":
		t.SlaEscalateAt = &at
	default:
		return fmt.Errorf("%w: kind %q 的 SLA 档 %q 无法落列（model 与 service 的映射脱节）",
			ErrHumanTaskInputInvalid, t.Kind, col)
	}
	if bad := model.HumanTaskSLAUnsupportedFields(t); len(bad) > 0 {
		return fmt.Errorf("%w: %s 类待办填了越界 SLA 列 %v", ErrHumanTaskInputInvalid, t.Kind, bad)
	}
	return nil
}

// 时钟与 ID 生成走包级变量（判据同 approval_*）：测试可替换、生产走默认。
//
// humanTaskNowFn 的读写只走紧随其后的那对 accessor（humanTaskSeamMu 守，包内其余位置直读 0 处）：
// Submit 挂在入站闸门链的协程上，判定期/心跳期的时钟读同样在 cron 的 RunOnce 里，而待办用例
// 逐格换时钟。理由同 approvalNowFn。
var (
	humanTaskSeamMu sync.RWMutex

	humanTaskNowFn = time.Now
)

func loadHumanTaskNowFn() func() time.Time {
	humanTaskSeamMu.RLock()
	defer humanTaskSeamMu.RUnlock()
	return humanTaskNowFn
}

var (
	humanTaskIDSeq     int64
	humanTaskIDFn      = newHumanTaskID
	globalHumanTaskSvc atomic.Pointer[HumanTaskService]
)

func newHumanTaskID() string {
	n := atomic.AddInt64(&humanTaskIDSeq, 1)
	return fmt.Sprintf("ht_%d_%d", loadHumanTaskNowFn()().UnixNano(), n)
}

// HumanTaskService 统一待办服务。
type HumanTaskService struct {
	repo repository.HumanTaskRepository
	cfg  HumanTaskConfigReader // nil = 一律用默认口径（装配早期）
}

// NewHumanTaskService 构造。cfg 可为 nil。
func NewHumanTaskService(repo repository.HumanTaskRepository, cfg HumanTaskConfigReader) *HumanTaskService {
	return &HumanTaskService{repo: repo, cfg: cfg}
}

// SetGlobalHumanTaskService 登记全局实例（装配点：internal/app 构造编排器那一处）。
// 传 nil 等于撤掉 —— 撤掉后端点回 503、编排器不再投递，这是本卡天然的关闸。
func SetGlobalHumanTaskService(s *HumanTaskService) { globalHumanTaskSvc.Store(s) }

// GlobalHumanTaskService 取全局实例（可能为 nil；调用方必须判空并给出"未装配"的答复）。
func GlobalHumanTaskService() *HumanTaskService { return globalHumanTaskSvc.Load() }

// Available 报告底座是否可用。回 503 与回空列表的分界线就在这一个判断上。
func (s *HumanTaskService) Available() bool {
	return s != nil && s.repo != nil && s.repo.Available()
}

func (s *HumanTaskService) require() error {
	if !s.Available() {
		return errors.New("human_task: 未装配可用的待办底座（DB 句柄缺失）")
	}
	return nil
}

// Submit 投递一条待办，返回 (那一行, 是否新建, 错误)。
//
// created=false 有两个来源（查到已开放 / 撞唯一索引后回读到了），两者都是同一句
// 业务结论：**这件事已经有人在等，不需要第二个人**。调用方（转人工）只该关心
// 有没有 error，不该关心 created 是几 —— 把 created 当成"投递失败"就会在
// 第二次转人工时丢掉一条本来该复用的待办。
func (s *HumanTaskService) Submit(ctx context.Context, in HumanTaskSubmitInput) (*model.HumanTask, bool, error) {
	if err := s.require(); err != nil {
		return nil, false, err
	}
	if err := in.normalize(); err != nil {
		return nil, false, err
	}
	due, err := s.slaFor(ctx, in)
	if err != nil {
		return nil, false, err
	}

	existing, err := s.repo.GetOpenBySubject(ctx, in.SubjectType, in.SubjectID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if existing.Kind != in.Kind {
			// 同一件事的第二类待办：不是重复投递，是调用方把 subject 写错了。
			// 静默复用会把"审批没人裁"这件事记在一条会话待办头上，而两类看的根本不是同一批人。
			return nil, false, fmt.Errorf("%w: %s/%s 已有一条 %s 类开放待办（id=%s），本次要投的是 %s 类",
				ErrHumanTaskInputInvalid, in.SubjectType, in.SubjectID, existing.Kind, existing.ID, in.Kind)
		}
		return existing, false, nil
	}

	now := loadHumanTaskNowFn()()
	row := &model.HumanTask{
		ID:          humanTaskIDFn(),
		Kind:        in.Kind,
		Status:      model.HumanTaskStatusPending,
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
		Title:       in.Title,
		Reason:      in.Reason,
		PayloadRef:  in.PayloadRef,
		OneID:       in.OneID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := humanTaskPlaceSLA(row, model.HumanTaskSLAField(in.Kind), due); err != nil {
		return nil, false, err
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		if errors.Is(err, repository.ErrHumanTaskOpenConflict) {
			// 上面那次读是空的：另一个调用方在这个窗口里写下了那一行。
			// 竞态下的正确出口是复用它（AC②），而不是把唯一索引错误抛给转人工。
			fresh, gerr := s.repo.GetOpenBySubject(ctx, in.SubjectType, in.SubjectID)
			if gerr != nil {
				return nil, false, gerr
			}
			if fresh != nil {
				if fresh.Kind != in.Kind {
					return nil, false, fmt.Errorf("%w: %s/%s 的开放待办类别冲突（库内 %s，本次 %s）",
						ErrHumanTaskInputInvalid, in.SubjectType, in.SubjectID, fresh.Kind, in.Kind)
				}
				return fresh, false, nil
			}
			// 索引拦了、回读却为空：那一行在两条语句之间落定了。
			// 报冲突而不是报成功 —— 调用方需要知道这次没落进去，下一轮重试会拿到真实结果。
			return nil, false, err
		}
		return nil, false, err
	}
	return row, true, nil
}

// slaFor 求出这一条的截止时刻（kind 决定"谁有权给这个数"）。
func (s *HumanTaskService) slaFor(ctx context.Context, in HumanTaskSubmitInput) (time.Time, error) {
	var zero time.Time
	switch in.Kind {
	case model.HumanTaskKindConversationHandoff:
		if in.SlaDueAt != nil {
			// 拒而不是覆盖：允许调用方代填就等于允许三个转人工入口各定一套首响口径，
			// 而"响应时长"这个指标要的恰恰是口径一致（AC④ 的另一半）。
			return zero, fmt.Errorf("%w: sla_due_at 不该由调用方给，sla_first_response_at 由本服务按 %s.%s 统一计算",
				ErrHumanTaskInputInvalid, HumanTaskConfigGroup, HumanTaskHandoffSlaKey)
		}
		return loadHumanTaskNowFn()().Add(time.Duration(s.handoffSlaMinutes(ctx)) * time.Minute), nil
	default:
		if in.SlaDueAt == nil {
			// 报错而不是"那就先不设截止"：没有截止的审批待办永远不会出现在逾期读数里，
			// 于是"卡点没人管"这件事从指标上消失，而它恰恰是这张表要暴露的东西。
			return zero, fmt.Errorf("%w: sla_due_at 必填（%s 类的截止在各自业务表里，本服务没有第二个来源）",
				ErrHumanTaskInputInvalid, in.Kind)
		}
		return *in.SlaDueAt, nil
	}
}

// handoffSlaMinutes 读会话首响 SLA（分钟），越界一律退回默认。
//
// 0 / 负数不是"没有 SLA"而是"一落库就逾期"：那会让逾期读数当场被垃圾填满，
// 而运维的第一反应是"待办积压了"，不是"某个参数被填成了 0"。
func (s *HumanTaskService) handoffSlaMinutes(ctx context.Context) int {
	v := DefaultHumanTaskHandoffSlaMinutes
	if s.cfg != nil {
		v = s.cfg.GetInt(ctx, HumanTaskConfigGroup, HumanTaskHandoffSlaKey, DefaultHumanTaskHandoffSlaMinutes)
	}
	if v < 1 || v > MaxHumanTaskHandoffSlaMinutes {
		return DefaultHumanTaskHandoffSlaMinutes
	}
	return v
}

// humanTaskHandoffTitlePrefix 会话待办标题前缀。待办中心那一行要一眼看出是转人工，
// 而不是"某客户某会话"——后者在第二列（subject_id）里。
const humanTaskHandoffTitlePrefix = "转人工："

// SubmitHandoffForSession 把"这条会话需要人工"投进待办池（转人工那一路的唯一出口）。
//
// 为什么要专门一个方法而不是让调用方自己拼 HumanTaskSubmitInput：这个二元组
// （customer_session / session_id）一旦拼错，待办就挂到了另一件事上，而它在池子里
// 看起来完全正常 —— 幂等判据（GetOpenBySubject）也会跟着失效，第二次转人工就会
// 多投一条。把映射收在这里，AC② 才有单一判据可测。
//
// created=false 不是失败：这件事已经有人在等了（同一次转人工被两个策略出口各叫一次、
// 或上一轮还没处理完），调用方只该关心 error。
func (s *HumanTaskService) SubmitHandoffForSession(ctx context.Context, session *model.CustomerSession,
	reason string) (*model.HumanTask, bool, error) {
	if session == nil {
		return nil, false, fmt.Errorf("%w: session 为空，无法定位要人处理的会话", ErrHumanTaskInputInvalid)
	}
	if strings.TrimSpace(session.SessionID) == "" {
		// 空会话 id 必须拒而不是建一条孤儿待办：池子里会出现一行"关于一条不存在的会话"，
		// 坐席点进去无处可去，而它还会占住 uq_human_task_open 的空位挡住后面对同一条的投递。
		return nil, false, fmt.Errorf("%w: session_id 为空，待办将指不回任何一次转人工", ErrHumanTaskInputInvalid)
	}
	title := strings.TrimSpace(reason)
	if title == "" {
		title = "人工介入" // 原因缺失时也不能让标题空着：空行在列表里等于"没人知道要干什么"
	}
	return s.Submit(ctx, HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindConversationHandoff,
		SubjectType: HumanTaskSubjectCustomerSession,
		SubjectID:   session.SessionID,
		Title:       humanTaskHandoffTitlePrefix + title,
		Reason:      reason, // 原样带，不裁前缀不加句号：这是上游文案，事后要能对上日志
		OneID:       session.OneID,
		// SlaDueAt 留空 ⇒ 首响截止由本服务按配置统一算（见 slaFor：三个入口各自定档就毁了 CS-32）
	})
}

// HumanTaskHandoffProducer 造一个可注入编排器的会话待办生产者。
//
// 放在 service 包而不是 app 包，是为了"生产装配的那份就是用例测的那份"：app 可以
// import service，service 的用例却没法 import app。挂上去的闭包只做一件事 ——
// 把 (session, reason) 交给 SubmitHandoffForSession 并原样上抛 error，
// 判不判致命由编排器那一段（produceHandoffTask 的调用方）决定。
//
// svc 为 nil 时返回 nil：与"从没注入过"逐字等价（没 DB 句柄时就是这份形态）。
func HumanTaskHandoffProducer(svc *HumanTaskService) func(context.Context, *model.CustomerSession, string) error {
	if svc == nil {
		return nil
	}
	return func(ctx context.Context, session *model.CustomerSession, reason string) error {
		_, _, err := svc.SubmitHandoffForSession(ctx, session, reason)
		return err
	}
}

// Get 读一条待办；不存在给 (nil, nil)（404 由 controller 决定，503 由 error 决定）。
func (s *HumanTaskService) Get(ctx context.Context, id string) (*model.HumanTask, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, strings.TrimSpace(id))
}

// List 按过滤条件取一页。
//
// 服务侧只做归一（大小写/空白）后透传：过滤语义与报错口径在仓储已经钉死，
// 这一层若再"顺手兜一下底"（把未知 kind 当成不过滤），AC③ 的用例就会在服务层假绿。
func (s *HumanTaskService) List(ctx context.Context, q HumanTaskListQuery) ([]*model.HumanTask, int64, error) {
	if err := s.require(); err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, normalizeHumanTaskQuery(q))
}

// normalizeHumanTaskQuery 归一过滤值（新切片，不写调用方传进来的那个）。
func normalizeHumanTaskQuery(q HumanTaskListQuery) HumanTaskListQuery {
	out := q
	if len(q.Kinds) > 0 {
		out.Kinds = make([]string, len(q.Kinds))
		for i, k := range q.Kinds {
			out.Kinds[i] = strings.ToLower(strings.TrimSpace(k))
		}
	}
	if len(q.Statuses) > 0 {
		out.Statuses = make([]string, len(q.Statuses))
		for i, v := range q.Statuses {
			out.Statuses[i] = strings.ToLower(strings.TrimSpace(v))
		}
	}
	out.AssigneeUserID = strings.TrimSpace(q.AssigneeUserID)
	return out
}

// HumanTaskKindCount 一类待办的开放数与逾期数。
type HumanTaskKindCount struct {
	Kind      string `json:"kind"`
	Open      int64  `json:"open"`
	Overdue   int64  `json:"overdue"`
	SlaColumn string `json:"sla_column"`
}

// HumanTaskCounts 未读聚合（AC③ 的"未读数"与 AC④ 的"指标隔离"共用这一个出口）。
type HumanTaskCounts struct {
	// At 逾期判据所用的时刻。必须回显：没有它，同一个读数无法复算，
	// 而"为什么刚才 3 条现在 4 条"是值班一定会问的问题。
	At        time.Time            `json:"at"`
	TotalOpen int64                `json:"total_open"`
	ByKind    []HumanTaskKindCount `json:"by_kind"`
	// UnknownOpen 库里 kind 不在三类值域内的开放行数。
	// 单列一个数而不是悄悄并进三类：这些行是真实存在的人工负担，
	// 但**不能**进任何一类的逾期读数（未知类没有自己的 SLA 档，挑一档就是污染）。
	UnknownOpen int64 `json:"unknown_kind_open"`
}

// Counts 每类的开放数与逾期数。
func (s *HumanTaskService) Counts(ctx context.Context) (*HumanTaskCounts, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	now := loadHumanTaskNowFn()()
	open, err := s.repo.CountOpenByKind(ctx)
	if err != nil {
		return nil, err
	}
	overdue, err := s.repo.CountOverdueOpenByKind(ctx, now)
	if err != nil {
		return nil, err
	}
	out := &HumanTaskCounts{At: now, ByKind: make([]HumanTaskKindCount, 0, len(model.HumanTaskKinds))}
	for _, k := range model.HumanTaskKinds {
		row := HumanTaskKindCount{Kind: k, Open: open[k], Overdue: overdue[k], SlaColumn: model.HumanTaskSLAField(k)}
		out.ByKind = append(out.ByKind, row)
		out.TotalOpen += row.Open
	}
	for k, v := range open {
		if !model.HumanTaskKindKnown(k) {
			out.UnknownOpen += v
		}
	}
	return out, nil
}

// Claim 认领（仅 conversation_handoff）：会话所有权可以抢、可以退。
func (s *HumanTaskService) Claim(ctx context.Context, id, operator string) (*model.HumanTask, error) {
	return s.transition(ctx, id, model.HumanTaskActionClaim, operator, nil,
		func(m *model.HumanTask, op string) error {
			now := loadHumanTaskNowFn()()
			m.AssigneeUserID = op
			m.ClaimedAt = &now
			return nil
		})
}

// Release 释放：退回池子等别人认领。**只有当前认领人**能释放。
//
// 为什么完成可以旁落、释放不行：完成是"这件事做完了"——一句关于世界的事实，
// 谁点的不影响真假；释放是"我手上这件还回去了"——一句关于所有权的事实,
// 旁人替放等于悄悄把 A 正在处理的会话转给池子，而 A 自己不知道。
func (s *HumanTaskService) Release(ctx context.Context, id, operator string) (*model.HumanTask, error) {
	return s.transition(ctx, id, model.HumanTaskActionRelease, operator,
		// 快照判一次（给一句准话），锁内再判一次（防"读完之后换人"的窗口）。
		// 只留锁内那次也能保正确，但那时提示会退化成笼统的 403。
		func(cur *model.HumanTask, op string) error {
			return humanTaskCheckHolder(cur, op)
		},
		func(m *model.HumanTask, op string) error {
			if err := humanTaskCheckHolder(m, op); err != nil {
				return err // 上抛 ⇒ 仓储整笔回滚
			}
			m.AssigneeUserID = ""
			m.ClaimedAt = nil // 下一次认领的起点必须从零算，否则响应时长会算成上一次
			return nil
		})
}

func humanTaskCheckHolder(cur *model.HumanTask, op string) error {
	if cur.AssigneeUserID != op {
		return fmt.Errorf("%w: %q 认领的不是这条（当前认领人 %q）", ErrHumanTaskNotHolder, op, cur.AssigneeUserID)
	}
	return nil
}

// Complete 完成：这件事不用再等人了。三类通用。
func (s *HumanTaskService) Complete(ctx context.Context, id, operator string) (*model.HumanTask, error) {
	return s.transition(ctx, id, model.HumanTaskActionComplete, operator, nil,
		func(m *model.HumanTask, op string) error {
			now := loadHumanTaskNowFn()()
			// 记下**做完的人**，即使他不是认领人（"我认领了同事替我做了"若仍挂在
			// 认领人名下，坐席工作量当场虚高）。认领时间不动：那是响应时长的起点。
			m.AssigneeUserID = op
			m.CompletedAt = &now
			return nil
		})
}

// Cancel 撤销：这条待办因为别的缘故不再需要人处理。**必须给理由**。
//
// 撤销人不写进 assignee_user_id：那一列的语义是"在处理这件事的人"，
// 混进撤销人就会污染"谁做完了"与坐席工作量。撤销人由 API 审计日志承担
// （本端点挂在 AuditMiddleware 之后，落库行里有请求者身份）。
func (s *HumanTaskService) Cancel(ctx context.Context, id, operator, reason string) (*model.HumanTask, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 撤销必须给 reason（事后要能回答\"这条为什么没人做完就结束了\"）", ErrHumanTaskInputInvalid)
	}
	if utf8.RuneCountInString(reason) > humanTaskCancelReasonMaxLen {
		return nil, fmt.Errorf("%w: reason 长度 %d 超上限 %d",
			ErrHumanTaskInputInvalid, utf8.RuneCountInString(reason), humanTaskCancelReasonMaxLen)
	}
	return s.transition(ctx, id, model.HumanTaskActionCancel, operator, nil,
		func(m *model.HumanTask, _ string) error {
			now := loadHumanTaskNowFn()()
			m.CancelReason = reason
			m.CancelledAt = &now
			return nil
		})
}

// CancelOpenBySubject 按业务身份撤销那条开放待办（会话侧钩子的出口）。
//
// 返回 (行, 是否本次撤销, 错误)：
//   - (nil, false, nil) = 压根没有开放待办。这是**成功**不是失败：会话关掉了，
//     而待办池里本来就没有这件事，钩子调用方（非致命旁路）不必为它记一行告警。
//   - (row, false, nil) = 有行，但在同一刻已被他人落定。目标同样已达成。
//
// operator 这一路没有真人（是系统替会话清的场），所以走 status CAS 而不走带人的跃迁。
func (s *HumanTaskService) CancelOpenBySubject(ctx context.Context, subjectType, subjectID, reason string) (*model.HumanTask, bool, error) {
	if err := s.require(); err != nil {
		return nil, false, err
	}
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	subjectID = strings.TrimSpace(subjectID)
	if subjectType == "" || subjectID == "" {
		// 空身份必须拒：仓储会报错，但如果这里先"查不到就算了"，就会变成
		// 每一次会话关闭都静默地什么都没撤销，而池子里的待办长期越积越多。
		return nil, false, fmt.Errorf("%w: subject_type/subject_id 为空，无法定位要撤销的待办", ErrHumanTaskInputInvalid)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, false, fmt.Errorf("%w: 撤销必须给 reason", ErrHumanTaskInputInvalid)
	}
	reason = humanTaskClip(reason, humanTaskCancelReasonMaxLen) // 系统侧给的长句截断即可（不是入参错误）

	cur, err := s.repo.GetOpenBySubject(ctx, subjectType, subjectID)
	if err != nil || cur == nil {
		return nil, false, err
	}
	to, ok := model.HumanTaskTransitionAllowed(cur.Kind, cur.Status, model.HumanTaskActionCancel)
	if !ok {
		// 已落定的行不该被这条路径再动一次（与 ApplyAction 的期望态判据同方向）。
		return cur, false, nil
	}
	now := loadHumanTaskNowFn()()
	applied, err := s.repo.ApplyAction(ctx, cur.ID, cur.Status, func(m *model.HumanTask) error {
		m.Status = to
		m.CancelReason = reason
		m.CancelledAt = &now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !applied {
		fresh, gerr := s.repo.GetByID(ctx, cur.ID)
		return fresh, false, gerr
	}
	row, err := s.repo.GetByID(ctx, cur.ID)
	return row, applied, err
}

// CompleteOpenBySubject 按业务身份把那条开放待办收成 done，并把完成人记成 operator。
//
// 与 CancelOpenBySubject 的分工只在"这一行怎么结束"上：completed 是有人给了一句结论，
// cancelled 是这件事不再需要结论。判据在调用方（裁决 vs 到期），本方法只认 operator：
// 空 operator 必须拒 —— 完成而不记谁做的，坐席工作量与"这件事谁结的"同时失去事实源。
func (s *HumanTaskService) CompleteOpenBySubject(ctx context.Context, subjectType, subjectID, operator string) (*model.HumanTask, bool, error) {
	if err := s.require(); err != nil {
		return nil, false, err
	}
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	subjectID = strings.TrimSpace(subjectID)
	operator = strings.TrimSpace(operator)
	if subjectType == "" || subjectID == "" {
		return nil, false, fmt.Errorf("%w: subject_type/subject_id 为空，无法定位要收口的待办", ErrHumanTaskInputInvalid)
	}
	if operator == "" {
		return nil, false, fmt.Errorf("%w: 完成必须记一个人（operator 为空）", ErrHumanTaskInputInvalid)
	}
	if utf8.RuneCountInString(operator) > humanTaskOperatorMaxLen {
		operator = humanTaskClip(operator, humanTaskOperatorMaxLen)
	}

	cur, err := s.repo.GetOpenBySubject(ctx, subjectType, subjectID)
	if err != nil || cur == nil {
		return nil, false, err
	}
	to, ok := model.HumanTaskTransitionAllowed(cur.Kind, cur.Status, model.HumanTaskActionComplete)
	if !ok {
		return cur, false, nil
	}
	now := loadHumanTaskNowFn()()
	applied, err := s.repo.ApplyAction(ctx, cur.ID, cur.Status, func(m *model.HumanTask) error {
		m.Status = to
		m.AssigneeUserID = operator // 与 Complete 同一口径：记下给出结论的人，即使他不是认领人
		m.CompletedAt = &now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !applied {
		fresh, gerr := s.repo.GetByID(ctx, cur.ID)
		return fresh, false, gerr
	}
	row, err := s.repo.GetByID(ctx, cur.ID)
	return row, applied, err
}

// --- 审批类待办（T-P3-04：一条 pending 审批 = 池子里的一行待办）——————————————

// approvalTaskTitlePrefix 审批待办标题前缀（与"转人工："同一形状：一眼看出这类要干什么）。
const approvalTaskTitlePrefix = "待审批："

// approvalTaskSLAPart 把截止时刻写进理由里的格式（RFC3339：可与 approval_requests.expires_at 直接对账）。
const approvalTaskSLAPart = time.RFC3339

// SubmitForApproval 实现 ApprovalTaskSink：审批落库为 pending ⇒ 投一条 approval 类待办。
//
// 为什么这里不"顺手"把被审对象的业务内容也带进来（报价金额、外发名单）：那是调用方
// （审批服务）也不知道的东西，本层能给的只有"哪条策略在等谁、几点截止"。
// 想看得更多，待办中心凭 subject_id 去读审批详情端点。
func (s *HumanTaskService) SubmitForApproval(ctx context.Context, req *model.ApprovalRequest) error {
	if req == nil {
		return fmt.Errorf("%w: 审批记录为空，待办将指不回任何一次裁决", ErrHumanTaskInputInvalid)
	}
	if req.Status != model.ApprovalStatusPending {
		// 只有 pending 需要人。给一条已落定的审批投待办 = 造一行永远等不到人、
		// 且会进逾期读数的死待办。
		return fmt.Errorf("%w: 审批 %s 当前态 %s，不需要人工裁决",
			ErrHumanTaskInputInvalid, req.ID, req.Status)
	}
	if req.ExpiresAt == nil {
		// pending 必带到期时刻是审批侧的写入不变量（见 SubmitForNode 同一条判据）。
		// 破了就不投：没有截止的审批待办永远不会逾期，而"卡点没人管"恰恰是这张表要暴露的。
		return fmt.Errorf("%w: 审批 %s 是 pending 却没有 expires_at，待办无法进逾期读数",
			ErrHumanTaskInputInvalid, req.ID)
	}
	_, _, err := s.Submit(ctx, HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindApproval,
		SubjectType: HumanTaskSubjectApprovalRequest,
		SubjectID:   req.ID,
		Title:       approvalTaskTitlePrefix + req.PolicyKey + "（对象 " + req.SubjectType + "/" + req.SubjectID + "）",
		Reason: "策略 " + req.PolicyKey + " 要求人工裁决，截止 " +
			req.ExpiresAt.Format(approvalTaskSLAPart),
		PayloadRef: req.SubjectType + "/" + req.SubjectID,
		// 截止直接取审批行自己那一列：TTL 与逾期读数必须同源，否则一次改 TTL 就会
		// 让"流程还等不等"与"待办逾不逾期"两套答案各说各话（T-P3-02 ⑥ 同一条理由）。
		SlaDueAt: req.ExpiresAt,
	})
	return err
}

// CloseForApproval 实现 ApprovalTaskSink：审批落终态 ⇒ 收掉那条待办。
//
// 分成两件事而不是统一"关掉就行"：
//   - 人给了结论（approved / rejected，裁决者是真人）⇒ done，并完成人记成那个真人；
//   - 系统自己落的（TTL 到期、策略自动放行）⇒ cancelled，因为它不是"人处理完了"。
//     把到期记成 done 会同时污染人工处理量与达成率两个数，而那正是 C3 分离视图的目的。
func (s *HumanTaskService) CloseForApproval(ctx context.Context, req *model.ApprovalRequest) error {
	if req == nil {
		return fmt.Errorf("%w: 审批记录为空，无法定位要收口的待办", ErrHumanTaskInputInvalid)
	}
	if req.Status == model.ApprovalStatusPending {
		// 收口只服务于"已经落定"。pending 行走到这里说明调用方把顺序写反了，
		// 静默返回 nil 会让待办一直挂着而没人知道它其实早该收。
		return fmt.Errorf("%w: 审批 %s 仍在等人裁决，不该收口它的待办", ErrHumanTaskInputInvalid, req.ID)
	}
	if model.ApprovalDecidedAutomatically(req.DecidedBy) {
		_, _, err := s.CancelOpenBySubject(ctx, HumanTaskSubjectApprovalRequest, req.ID,
			approvalTaskAutoCancelReason(req))
		return err
	}
	_, _, err := s.CompleteOpenBySubject(ctx, HumanTaskSubjectApprovalRequest, req.ID, req.DecidedBy)
	return err
}

// approvalTaskAutoCancelReason 系统侧收口那句话。到期与自动放行要分开写：
// 值班看到"到期"会去查为什么没人批，看到"自动放行"会去查策略配置，是两个动作。
func approvalTaskAutoCancelReason(req *model.ApprovalRequest) string {
	switch req.Status {
	case model.ApprovalStatusExpired:
		return "审批到期未裁决，系统按 TTL 收口（裁决者 " + req.DecidedBy + "）"
	case model.ApprovalStatusApproved:
		return "策略自动放行，无需人工裁决（裁决者 " + req.DecidedBy + "）"
	default:
		return "审批已由 " + req.DecidedBy + " 落定为 " + req.Status + "，无需人工裁决"
	}
}

// globalHumanTaskSink 审批服务应装的待办出口：每次调用**现取**全局底座（T-P3-04）。
//
// 为什么不直接把实例传过去（SetTaskSink(GlobalHumanTaskService()) 那种一步写法）：
// router.go 里 InitApprovalRuntime 跑在 InitHumanTaskRuntime 之前（:230 与 :235），
// 装配那一刻拿到的恒是 nil ⇒ 审批永远不进待办池。而"静默功能缺失"是这里最难查的
// 失败形状：不报错、不告警，只有池子里永远看不见审批类待办。现取让装配顺序不再有意义。
type globalHumanTaskSink struct{}

// GlobalHumanTaskApprovalSink 审批运行时应装的出口（装配点：app.InitApprovalRuntime）。
func GlobalHumanTaskApprovalSink() ApprovalTaskSink { return globalHumanTaskSink{} }

// 底座未装配 ⇒ 按"没有出口"处理，返回 nil 而不是 error。
//
// 这与 deliverTask 对投递失败的上抛不矛盾，两者判的是不同的事：
//   - 投递失败是**运行时故障**（表在、连接在、这次写坏了）⇒ 挂起必须失败，
//     否则会留下一行没人看得见的 pending；
//   - 底座未装配是**装配状态**，与 SetTaskSink(nil) 同一含义（"审批不进池子"那一档，
//     也就是 T-P3-02 的交付态）。此时报故障会把一个部署选择伪装成事故。
//
// 未装配档不会把流程吊死：pending 行仍由清扫按自己的 expires_at 翻成 expired 并叫醒
// 等待方（FF_LTC_APPROVAL_RESUME 开时清扫一定在跑），所以最坏结果是"这单等到超时"，
// 而不是"永远挂着"。出声（Warn）是为了让这件事在日志里说得出。
func (globalHumanTaskSink) SubmitForApproval(ctx context.Context, req *model.ApprovalRequest) error {
	if pool := GlobalHumanTaskService(); pool != nil {
		return pool.SubmitForApproval(ctx, req)
	}
	logger.Warnf("[human_task] 待办底座未装配 ⇒ 审批 %s 不进池子（仍由其 expires_at 到期收口）", approvalLinkID(req))
	return nil
}

func (globalHumanTaskSink) CloseForApproval(ctx context.Context, req *model.ApprovalRequest) error {
	if pool := GlobalHumanTaskService(); pool != nil {
		return pool.CloseForApproval(ctx, req)
	}
	return nil
}

// approvalLinkID 给日志用的审批 id（nil 记录也走得通：日志行不该因一句 id  panic）。
func approvalLinkID(req *model.ApprovalRequest) string {
	if req == nil {
		return "<nil>"
	}
	return req.ID
}

// transition 三个对外动作共用的那一段：读快照 → 判跃迁 → 锁内改 → 回读。
//
// preGuard 拿的是**锁外**快照（只用于给一句准话），fn 拿的是**锁内**那一行
// （正确性判据必须放这里）。两者不是重复：外层那次读与写回之间有时间窗，
// 窗口里状态可以被任何人改掉，而 CAS 只保证"状态没变"，不保证"认领人没换"。
func (s *HumanTaskService) transition(
	ctx context.Context,
	id, action, operator string,
	preGuard func(cur *model.HumanTask, op string) error,
	fn func(m *model.HumanTask, op string) error,
) (*model.HumanTask, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	operator = strings.TrimSpace(operator)
	if id == "" {
		return nil, fmt.Errorf("%w: 待办 id 为空", ErrHumanTaskInputInvalid)
	}
	if operator == "" {
		// 三个动作都要留人：没有操作者的状态跃迁在本表里就是"凭空被人处理了一条待办"，
		// 事后无法复盘，而且坐席工作量指标会白送一次。
		return nil, fmt.Errorf("%w: %s 必须带操作者（未登录或身份取不到）", ErrHumanTaskInputInvalid, action)
	}
	if utf8.RuneCountInString(operator) > humanTaskOperatorMaxLen {
		return nil, fmt.Errorf("%w: 操作者长度 %d 超上限 %d",
			ErrHumanTaskInputInvalid, utf8.RuneCountInString(operator), humanTaskOperatorMaxLen)
	}

	cur, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, fmt.Errorf("%w: id=%s", ErrHumanTaskNotFound, id)
	}
	to, ok := model.HumanTaskTransitionAllowed(cur.Kind, cur.Status, action)
	if !ok {
		return nil, humanTaskTransitionError(cur, action, operator)
	}
	if preGuard != nil {
		if err := preGuard(cur, operator); err != nil {
			return nil, err
		}
	}
	applied, err := s.repo.ApplyAction(ctx, id, cur.Status, func(m *model.HumanTask) error {
		m.Status = to
		return fn(m, operator)
	})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, fmt.Errorf("%w: id=%s", ErrHumanTaskLost, id)
	}
	return s.repo.GetByID(ctx, id)
}

// humanTaskTransitionError 把"这条路走不通"翻成一句值班能直接照做的答复。
//
// 同一种失败（409）在三种场景下的处置完全不同：
// 本人重复点 → 刷新即可；被他人认领 → 等对方释放；已落定 → 这件事已经结束了。
// 只回一句"状态不允许"等于把判断推回给前端去猜。
func humanTaskTransitionError(cur *model.HumanTask, action, operator string) error {
	switch {
	case action == model.HumanTaskActionClaim && cur.Status == model.HumanTaskStatusClaimed && cur.AssigneeUserID == operator:
		return fmt.Errorf("%w: 这条待办已由本人认领，无需重复认领（id=%s）", ErrHumanTaskTransition, cur.ID)
	case action == model.HumanTaskActionClaim && cur.Status == model.HumanTaskStatusClaimed:
		return fmt.Errorf("%w: 这条待办已被他人认领（id=%s），如需接手请由当前认领人释放后再认领", ErrHumanTaskTransition, cur.ID)
	case action == model.HumanTaskActionClaim:
		return fmt.Errorf("%w: %s 类待办不可认领，只能裁决（id=%s，当前态 %s）——C3：审批/催收不可抢占",
			ErrHumanTaskTransition, cur.Kind, cur.ID, cur.Status)
	case action == model.HumanTaskActionRelease:
		return fmt.Errorf("%w: 只有 pending 的会话待办可以退回到池子（id=%s，当前态 %s）",
			ErrHumanTaskTransition, cur.ID, cur.Status)
	case model.HumanTaskIsOpen(cur.Status):
		return fmt.Errorf("%w: %s 在 %s 态不可执行（id=%s）", ErrHumanTaskTransition, action, cur.Status, cur.ID)
	default:
		return fmt.Errorf("%w: 这条待办已处理完毕（当前态 %s），同一件事若还需人工请重新投递一条（id=%s）",
			ErrHumanTaskTransition, cur.Status, cur.ID)
	}
}
