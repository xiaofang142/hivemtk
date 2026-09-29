package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// 不可逆写台账。step 行的 status 只回答「这一步跑成什么样」，回答不了
// 「这条内容的提交是否可能发生」——而后者才是禁止双发的依据。真机 Leg X 实测的假绿
// （零提交却 verified）说明：连 verified 本身都可能是错的，所以台账不是日志增强，
// 是重发闸门的唯一事实来源。状态语义见 model.StepSubmit*，列定义见 model.BrowserStep.SubmitState。

// HashWriteText 提交正文的自然键片段（跨 session 比对用）。
// 归一化口径与扩展侧 injPostCommentVerify 的 norm 一致（去掉全部空白）：
// 「同一篇评论」在平台侧就是同一份内容，多一个空格不构成两条不同评论，
// 因此这里偏保守——宁可把两条几乎相同的文本判成同一条而拦下，也不可放过一次双发。
func HashWriteText(s string) string {
	norm := strings.Join(strings.Fields(s), "")
	if norm == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(norm))
	return fmt.Sprintf("%08x", h.Sum32())
}

// recordSubmitState 台账状态写点。尽力而为但必达终态：ctx 已被取消（超时/中止腿必然如此）
// 时走 WithoutCancel——「send 已跨越、execCtx 恰好死掉」正是最需要留下台账的一刻。
//
// 改口径：写失败必须上抛。旧实现只 Warn，四个调用点拿不到失败事实，于是
// 「send 已跨越但台账没落」的下一轮 FindSubmitAttempt 查空——双发闸门整体消失，而这张表
// 在 §3.1 的定性是「重发闸门的唯一事实来源」。取舍方向不对称：写步失败是**可见、可人工重跑**，
// 双发是**不可见、撤不回**。失败时先在 ledgerWriteBudget 内退避重试（单行 UPDATE 失败最常见
// 是行锁抖动，重试能把缺口变成没缺口），仍失败则给本会话置降级并（crossed 时）记一条进程内兜底。
// crossed=「这一步是否可能已跨越不可逆提交点」：prepared 传 false（未跨越，拦下即可），
// sent/终态/通用写步传 true。判据是 UpdateSubmitState 失败不会动旧值，所以
// 「跨越后库里仍不在拦阻集合内」⟺ 本次 sent 写失败——crossed=true 覆盖它，且只会多拦不会漏拦。
func (e *Executor) recordSubmitState(ctx context.Context, taskID, sessionID, stepRowID uint, state, textHash string, crossed bool) error {
	if stepRowID == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerWriteBudget)
	defer cancel()
	var err error
	for attempt := 1; ; attempt++ {
		err = e.stepRepo.UpdateSubmitState(writeCtx, stepRowID, state, textHash)
		if err == nil {
			return nil
		}
		if attempt >= ledgerWriteAttempts || writeCtx.Err() != nil {
			break
		}
		select {
		case <-writeCtx.Done():
		case <-time.After(ledgerWriteRetryStep << (attempt - 1)):
		}
	}
	logger.Warnf("[BrowserExec] 写台账落库失败 step=%d state=%s（含重试共 %d 次）: %v", stepRowID, state, ledgerWriteAttempts, err)
	if crossed {
		e.rememberLedgerGap(taskID, textHash)
	}
	e.markLedgerBroken(sessionID, fmt.Sprintf("%s 未落账（重试 %d 次）: %v", state, ledgerWriteAttempts, err))
	return fmt.Errorf("%s（%v）: %w", errLedgerUnwritten, err, errLedgerDBDegraded)
}

// 台账写失败的三个固定片段：文案给运维看，哨兵给逻辑判。
const errLedgerUnwritten = "写台账落库失败"

var errLedgerDBDegraded = errors.New("台账未落，本会话写能力已降级")

const (
	ledgerWriteAttempts  = 3
	ledgerWriteRetryStep = 150 * time.Millisecond
	// ledgerGapCap 兜底集合的条数上限：只在台账写失败时增长，正常路径恒为 0。
	// 有界即可，不必按时间淘汰——它只需活得比「同一任务还能被同进程自动重试」久。
	ledgerGapCap = 512
)

// markLedgerBroken 给会话打上「写能力已降级」。只记第一个原因（最近一次失败往往是同一根因的重复）。
func (e *Executor) markLedgerBroken(sessionID uint, why string) {
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	if _, ok := e.ledgerBroken[sessionID]; !ok {
		e.ledgerBroken[sessionID] = why
	}
}

// ledgerBrokenReason 该会话是否已因台账写失败降级，以及最初的失败原因。
func (e *Executor) ledgerBrokenReason(sessionID uint) (string, bool) {
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	why, ok := e.ledgerBroken[sessionID]
	return why, ok
}

// clearLedgerBroken 会话结束即弃：降级是「这次执行期间台账不可信」，不是给未来会话判死刑。
func (e *Executor) clearLedgerBroken(sessionID uint) {
	e.ledgerMu.Lock()
	delete(e.ledgerBroken, sessionID)
	e.ledgerMu.Unlock()
}

// rememberLedgerGap 记一条「提交尝试已跨越、台账却没落成」。它是数据库闸门的进程内兜底，
// 覆盖面**只有本进程、本进程存活期**（键是 taskID|textHash，与是否换 session 无关）。
//
// 两条必须一起记住的边界（二次审核写清，之前那句「唯一会自动重跑的那条路」说过头了）：
//   - 自动重试的挂起态是**持久化**的（feedback.go scheduleRetry 只写 task.next_retry_at，
//     重启不丢、由任何持连接的实例认领），本集合不落库 ⇒ 进程一重启它就空了。
//     所以缺口不能只靠这里挡：消费方已经接到反馈层——OnSessionFinished 见到
//     缺口就不再挂重试（并把原因写进任务行），runRetry 认领后再查一次；调用方仍须把
//     「需人工核对是否已发布」写进那一步的文案，因为**跨进程**重启后的存量挂起行挡不住。
//   - 跨进程人工重跑同样挡不住，同一理由。
func (e *Executor) rememberLedgerGap(taskID uint, textHash string) {
	if textHash == "" {
		return
	}
	key := writeGapKey(taskID, textHash)
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	if e.ledgerGaps[key] {
		return // 只按不同键增长：集合上限 512 因此是「512 次独立缺口」，不是「512 次写尝试」
	}
	e.ledgerGaps[key] = true
	e.ledgerGapOrder = append(e.ledgerGapOrder, key)
	if len(e.ledgerGapOrder) > ledgerGapCap {
		oldest := e.ledgerGapOrder[0]
		e.ledgerGapOrder = e.ledgerGapOrder[1:]
		delete(e.ledgerGaps, oldest)
	}
}

func (e *Executor) ledgerGapHas(taskID uint, textHash string) bool {
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	return e.ledgerGaps[writeGapKey(taskID, textHash)]
}

// HasCrossedLedgerGap 该任务在本进程里是否留有「已跨越提交尝试点、台账却没落成」的缺口
// （任一文本）。消费方是反馈层：挂自动重试前与认领重试后各查一次——挡不住重启后的
// 存量挂起行，但能让「本机跑出的缺口」不再变成另一次自动下发。
func (e *Executor) HasCrossedLedgerGap(taskID uint) bool {
	prefix := strconv.FormatUint(uint64(taskID), 10) + "|"
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	for _, k := range e.ledgerGapOrder {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func writeGapKey(taskID uint, textHash string) string {
	return strconv.FormatUint(uint64(taskID), 10) + "|" + textHash
}

// guardResubmit 双发闸：同任务、同文本在历史上（含其它 session、含被重启后重下发、含
// Brain 模式换到下标重投）已有提交尝试即不再下发。unattributed 也拦——归因不到不等于没发生。
//
// 三种结论（把「任务级自动重试豁免」并进同一趟查询，判定只此一处）：
//   - nil             → 放行
//   - errRetrySkipped → 本次是自动重试轮（task.RetryCount>0）且已有尝试：这一步跳过，
//     让任务里剩下的只读步跑完。重试的目的不是把同一条内容再发一遍，而是补完可恢复的环节；
//     人工重跑（RetryCount==0）不走这条——人要看到「被闸门拦下」这个事实，而不是悄悄少跑一步。
//     哨兵里挂着前一轮那行的事实（见 writeAttemptPrior），上层据此分「可安心跳过」与「必须判红」。
//   - 其余 err        → 拒绝执行（步判 failed，台账不动）
//
// 查询失败改 fail-close（旧口径是 Warn 后放行）。同行默认值就是这条——K8s admission
// 的 failurePolicy 默认 Fail、sideEffects 把 Unknown 与 Some 同等对待。「DB 抖动期照常执行」的实际
// 含义是「抖动期没有闸门」，而抖动期恰恰是最容易重复下发的时候。
func (e *Executor) guardResubmit(ctx context.Context, task *model.BrowserTask, textHash string, excludeStepRowID uint) error {
	if e.stepRepo == nil || textHash == "" {
		return nil
	}
	if e.ledgerGapHas(task.ID, textHash) {
		return fmt.Errorf("写步拒绝执行：同文本此前有一次提交跨越了提交尝试点却未落台账（数据库闸门当时失效，现由进程内兜底拦下），重发即双发，请人工核对该内容是否已发布后再改文本或换新任务")
	}
	prior, err := e.stepRepo.FindSubmitAttempt(ctx, task.ID, textHash, excludeStepRowID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		logger.Warnf("[BrowserExec] 双发闸查询失败 task=%d（拒绝下发）: %v", task.ID, err)
		return fmt.Errorf("双发闸查询失败（%v）——无法确认同文本是否已有提交尝试，本写步拒绝下发；库恢复后重跑本任务即可", err)
	}
	p := &writeAttemptPrior{stepRowID: prior.ID, sessionID: prior.SessionID, state: prior.SubmitState}
	if task.RetryCount > 0 {
		return fmt.Errorf("%w: %w", errRetrySkipped, p)
	}
	return fmt.Errorf("写步拒绝执行：同文本已有提交尝试（%s），重发即双发，请人工核对后再改文本或换新任务", p)
}

// errRetrySkipped 哨兵：自动重试轮里撞见历史提交尝试 → 跳过该写步（区别于「拒绝执行」的失败态）。
var errRetrySkipped = errors.New("自动重试轮跳过已尝试过的写步")

// writeAttemptPrior 闸门命中那行「历史提交尝试」的事实。必须随哨兵一起上抛：
// 「前一轮已 verified」与「前一轮只到 sent/unattributed」在本轮该判什么上完全相反——
// 前者目标已达成，跳过它继续跑剩余步即可；后者提交从未被证明，而本轮重发即双发、
// 无从证明，只能判红。只传一句文案就会把后者也糊成全绿（立项要消灭的那类假绿）。
type writeAttemptPrior struct {
	stepRowID uint
	sessionID uint
	state     string
}

func (p *writeAttemptPrior) Error() string {
	return fmt.Sprintf("step=%d session=%d state=%s", p.stepRowID, p.sessionID, p.state)
}

// verified 前一轮是否已收紧回查确认命中（唯一可宣称「已发布」的态）。
func (p *writeAttemptPrior) verified() bool { return p.state == model.StepSubmitVerified }

// priorOfSkippedWrite 从 errRetrySkipped 哨兵里取回前一轮事实。取不到即「未知」，
// 按未验证处理——宁可多判一次红，也不把没证明的提交算成成功。
func priorOfSkippedWrite(err error) *writeAttemptPrior {
	var p *writeAttemptPrior
	if errors.As(err, &p) {
		return p
	}
	return &writeAttemptPrior{state: model.StepSubmitUnattributed}
}

// stepEffect 一个步的副作用分类。第三态 effectUnknown 是原来缺的那一格：
// 「判不出有没有副作用」既不是「有」也不是「没有」——K8s admission 把 sideEffects 的
// Unknown 与 Some 同等对待、failurePolicy 默认 Fail，道理都是「不能用判不出来换一次放行」。
type stepEffect int

const (
	effectNone stepEffect = iota
	effectWrite
	effectUnknown
)

// needsWriteGate 是否要过写闸门（retries 钳 0 + 双发闸 + D7）。
// 只有「确定没有副作用」才免检；判不出与确定有同样进闸门。
func (s stepEffect) needsWriteGate() bool { return s != effectNone }

// classifyStepEffect 判「这一步有没有不可逆副作用」= 编排声明 ∪ 原语推导，两者取并集且推导必赢：
// 声明位交给编排方/LLM 用，而 LLM 把发送步标成 is_write=false 就能摘掉服务端的重试闸门，
// 所以推导（命中平台注册定位表）只能追加、不能被声明撤销。why 供日志与归因。
//
// 推导规则刻意收窄（相对设计稿 §3.3）：type+submit_on_enter 只在目标命中平台注册的
// comment_input 定位表时才算写——真机交互搜索腿同样是「输入框 + 回车」，把一切回车都判成写，
// 会白白剥掉搜索步的重试能力并在重试轮里错误跳过。
//
// 定位表**取不到**（平台未注册/适配器缺失）不再等同于「推导不命中」。
// 旧写法 `locs, _ :=` 把错误丢掉，三条推导全体不命中，只剩 post_comment 与显式声明 ⇒
// retries=0、双发闸、D7 三道同时静默消失。收窄条件同批立住：只有**报错**才升未知，
// 单纯没命中 locator 仍判只读（上面那条口径不动）；且只升级「本来可能被推导出写」的
// 动作形态（type+回车 / click / click_near 带按钮文案），scroll、open_tab 这类
// 无论定位表里有什么都不可能是提交动作的步不受牵连。
func classifyStepEffect(task *model.BrowserTask, step parsedStep) (stepEffect, string) {
	if step.Action == "post_comment" {
		return effectWrite, "action=post_comment"
	}
	locs, err := platformStepLocators(task)
	if err != nil {
		if step.IsWrite {
			return effectWrite, "declared=is_write"
		}
		if stepCouldBeSubmit(step) {
			return effectUnknown, fmt.Sprintf("unknown=平台定位表不可得(%v)", err)
		}
		return effectNone, ""
	}
	switch step.Action {
	case "type":
		if step.SubmitOnEnter && locatorMatches(locs["comment_input"], step.Target) {
			return effectWrite, "derived=type+enter→comment_input"
		}
	case "click":
		if locatorMatches(locs["send_button"], step.Target) {
			return effectWrite, "derived=click→send_button"
		}
	case "click_near":
		if step.ButtonText != "" && step.ButtonText == locs["send_button_text"] {
			return effectWrite, "derived=click_near→send_button_text"
		}
	}
	if step.IsWrite {
		return effectWrite, "declared=is_write"
	}
	return effectNone, ""
}

// stepCouldBeSubmit 该动作形态是否**可能**由定位表推导成提交动作。
// 定位表不可得时，只有这一族步需要按最坏情况处置——其余形态与表内容无关，判未知是白扣分。
func stepCouldBeSubmit(step parsedStep) bool {
	switch step.Action {
	case "type":
		return step.SubmitOnEnter
	case "click":
		return true
	case "click_near":
		return step.ButtonText != ""
	default:
		return false
	}
}

// writeStepKey 写步的台账键：有正文用正文（评论文本本身），无正文的发送动作（点发送按钮）
// 用定位三元组兜底——同一任务里同一个发送位反复出现，本就等同于「同一次提交」。
func writeStepKey(step parsedStep) string {
	if h := HashWriteText(step.Value); h != "" {
		return h
	}
	return HashWriteText(step.Action + "\x00" + step.Target + "\x00" + step.Anchor + "\x00" + step.ButtonText)
}

// platformStepLocators 取平台的定位表（推导写步的唯一依据）。错误原样上抛：
// 起「取不到」是有结论的第三态（见 classifyStepEffect），不再就地吞成空表。
func platformStepLocators(task *model.BrowserTask) (map[string]string, error) {
	p, err := platform.Get(taskPlatformID(task))
	if err != nil {
		return nil, err
	}
	return p.Locators(), nil
}

// locatorMatches 步骤目标是否命中平台定位表项。定位表项是逗号分隔的 CSS 候选列表
// （".a, .b"），所以两种形态都算命中：整串相等，或恰好等于其中一段候选。
func locatorMatches(locatorList, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || locatorList == "" {
		return false
	}
	if strings.TrimSpace(locatorList) == target {
		return true
	}
	for _, piece := range strings.Split(locatorList, ",") {
		if strings.TrimSpace(piece) == target {
			return true
		}
	}
	return false
}

// isNeverExecuted 错误是否证明「这一步从未在页面上发生」——*_not_found 是元素从未命中、
// *_inject_timeout_ 是注入从未执行、*_not_interactable 是可点性判定在**拿坐标之前**就把这次
// 动作拒了（遮挡 / 零尺寸 / disabled / 抖动未落位），三者都没有副作用，台账因此留空
// （= 不算尝试，可安全重下发）。补上第四类：ErrCommandNeverOnWire=那一帧根本没写进
// socket（注册表里没有该用户的连接），扩展从未看到这条命令，副作用同样为零。
// 反过来，WS 超时/写后才断/未知错误一律不算：超时不等于没发生，那正是双发的形状。
//
// *_not_interactable 这条前提是可查的，不是猜的：这些文案只由页面内的三份 probe
// （injClick / injClickNear / injPostCommentSend）产出，且产出点全在 `cdpInput.clickAt`
// 之前——SW 侧的 dispatch 一句都不合成它（锁在 user-web 的静态腿上）。
// 把它算成「已尝试」的后果是具体的：一次被浮层遮住的写步会把唯一正确的处置
// （等页面停下再跑一次）永久拦死在双发闸外（真机腿实读 step 2287）。
func isNeverExecuted(err error) bool {
	if err == nil {
		return false
	}
	if isCommandNeverOnWire(err) {
		return true
	}
	// CDP attach 阶段就没成功 ⇒ 事件从未下发（与 cdp_send/click_unacked 那一族相反，
	// 后者事件已入队=结局未知）。见 executor.go isNeverDispatched 的归因与代价说明。
	if isNeverDispatched(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "_inject_timeout_") || strings.Contains(msg, "_not_found") ||
		strings.Contains(msg, "_not_interactable")
}

// ------------------------------------------------------------------ 存储层独占声明

// claimWriteSlot 读闸放行之后、任何帧下发之前占坑（(task_id, text_hash) 一把，见
// repository/write_claim.go 与 model/write_claim.go）。
//
// 两层闸门各管一种并发，缺一不可：
//   - guardResubmit（读侧）管「历史上是否发生过尝试」——依据台账三态，跨会话、跨重启；
//   - 本函数管「同一时刻另一条腿也在过闸」——那正是读侧两边都查空的形状，
//     只有 INSERT 的原子性能分出先后，所以裁决权必须下推到库里的唯一约束。
//
// 三种结论里只有第一种放行：占坑报错（含「插了 0 行却查不到持有者」）同样拒绝下发。
// 免检的条件只能是「这一步不是写步」，不能是「闸门没接好」——这条免检口径。
func (e *Executor) claimWriteSlot(ctx context.Context, taskID, sessionID, stepRowID uint, textHash string) error {
	if e.writeClaimRepo == nil {
		return errors.New("存储层写声明闸门未接线：占不了坑，就无从判断另一条腿是否正在同一份文本上")
	}
	holder, err := e.writeClaimRepo.ClaimWriteSlot(ctx, taskID, sessionID, stepRowID, textHash)
	if err != nil {
		return err
	}
	if holder != nil {
		return fmt.Errorf("同文本的写声明已在 step=%d session=%d（%s 占坑）手里——那一条腿正要去跨或已经跨过提交点，本腿下发即双发；若已核实那一腿从未真的提交，删掉 browser_write_claims 里这一行即可重跑",
			holder.StepRowID, holder.SessionID, holder.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

// releaseWriteSlot 写步收尾时腾坑，两个条件缺一不可：
//  1. 库里这一步没留下任何尝试凭据（submit_state 不在拦阻集合）——这是「从未发生 ⇒ 可安全重下发」
//     那半边链子。不腾就把声明表变成永久黑名单：一次浮层遮挡、一次 D7 等待超时，都会让
//     唯一正确的处置（等页面停下再跑一次）被拦死在闸门外。
//  2. 本会话台账没写过失败。缺了这条就危险：sent 那一写失败时库里停在 prepared（判据 1 成立），
//     可提交其实已经跨越了——那一刻**这条声明是这次公开提交在库里唯一的凭据**，
//     腾出去就等于把一次没人记得的提交重新变成可重发。此时宁可留一把占死的坑（可见、可人工释放）。
//
// 释放失败只告警：失败方向是「坑留着」，与判据 2 同侧，是保守的那一侧。
func (e *Executor) releaseWriteSlot(ctx context.Context, taskID, sessionID, stepRowID uint, textHash string) {
	if e.writeClaimRepo == nil || e.stepRepo == nil || textHash == "" {
		return
	}
	// ctx 很可能已被取消（超时腿与中止腿必然如此），而「腾不腾坑」这个决定恰恰要在事后也做得成。
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerWriteBudget)
	defer cancel()
	if why, broken := e.ledgerBrokenReason(sessionID); broken {
		logger.Warnf("[BrowserExec] 写声明保留不释放 task=%d step=%d：本会话台账写失败过（%s），保留是保守侧", taskID, stepRowID, why)
		return
	}
	state, err := e.stepRepo.SubmitStateOf(readCtx, stepRowID)
	if err != nil {
		logger.Warnf("[BrowserExec] 写声明保留不释放 task=%d step=%d（回读台账态失败，判不出=不按没发生算）: %v", taskID, stepRowID, err)
		return
	}
	for _, s := range model.StepSubmitAttemptedStates() {
		if s == state {
			return // 已跨越：声明与台账同生死，腾坑由「人工核对后删那一行」这条唯一的路负责
		}
	}
	affected, err := e.writeClaimRepo.ReleaseWriteSlot(readCtx, stepRowID, textHash)
	if err != nil {
		logger.Warnf("[BrowserExec] 写声明释放失败 task=%d step=%d（坑留着，必要时人工释放）: %v", taskID, stepRowID, err)
		return
	}
	if affected > 0 {
		logger.Infof("[BrowserExec] 写声明已释放 task=%d step=%d（本步未跨提交点，同文本可重下发）", taskID, stepRowID)
	}
}

// recordGenericWriteLedger 非 post_comment 写步的台账落点。post_comment 有自己的
// prepared→sent→终态 三段归因（含 finalize 回查），不在此列。
// 成功只记 sent 不记 verified：这些原语没有回查通路，宣称「已发布」就是假绿。
// 返回 error 供上层收口：这里必然是该行的第一条台账（prepared 都不曾有过），
// 所以写失败一律按「越点未落账」处理（crossed=true）。
func (e *Executor) recordGenericWriteLedger(ctx context.Context, taskID, sessionID uint, step parsedStep, stepRowID uint, textHash string, dispatchErr error) error {
	if step.Action == "post_comment" || stepRowID == 0 {
		return nil
	}
	state := model.StepSubmitSent
	switch {
	case dispatchErr == nil:
	case isNeverExecuted(dispatchErr):
		return nil // 从未发生：不记尝试，留给重下发
	default:
		state = model.StepSubmitUnattributed // 结果未知 → 记成尝试，交人来判
	}
	return e.recordSubmitState(ctx, taskID, sessionID, stepRowID, state, textHash, true)
}
