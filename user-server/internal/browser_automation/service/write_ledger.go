package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// 批6（F11b）：不可逆写台账。step 行的 status 只回答「这一步跑成什么样」，回答不了
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
// 写失败只告警不阻断：不可逆动作既已发生，把步判成失败也撤不回提交，反而丢掉后续 finalize 证据。
func (e *Executor) recordSubmitState(ctx context.Context, stepRowID uint, state, textHash string) {
	if stepRowID == 0 {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerWriteBudget)
	defer cancel()
	if err := e.stepRepo.UpdateSubmitState(writeCtx, stepRowID, state, textHash); err != nil {
		logger.Warnf("[BrowserExec] 写台账落库失败 step=%d state=%s: %v", stepRowID, state, err)
	}
}

// guardResubmit 双发闸：同任务、同文本在历史上（含其它 session、含被重启后重下发、含
// Brain 模式换到下标重投）已有提交尝试即不再下发。unattributed 也拦——归因不到不等于没发生。
//
// 三种结论（批7 把「任务级自动重试豁免」并进同一趟查询，判定只此一处）：
//   - nil             → 放行
//   - errRetrySkipped → 本次是自动重试轮（task.RetryCount>0）且已有尝试：这一步跳过，
//     让任务里剩下的只读步跑完。重试的目的不是把同一条内容再发一遍，而是补完可恢复的环节；
//     人工重跑（RetryCount==0）不走这条——人要看到「被闸门拦下」这个事实，而不是悄悄少跑一步。
//     哨兵里挂着前一轮那行的事实（见 writeAttemptPrior），上层据此分「可安心跳过」与「必须判红」。
//   - 其余 err        → 拒绝执行（步判 failed，台账不动）
//
// 查询本身失败时 fail-open 放行执行（错误方向仍是「照常执行但记日志」，
// 而不是查不到就拒绝一切提交：DB 抖动不该把整条自动化链路锁死）。
func (e *Executor) guardResubmit(ctx context.Context, task *model.BrowserTask, textHash string, excludeStepRowID uint) error {
	if e.stepRepo == nil || textHash == "" {
		return nil
	}
	prior, err := e.stepRepo.FindSubmitAttempt(ctx, task.ID, textHash, excludeStepRowID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		logger.Warnf("[BrowserExec] 双发闸查询失败 task=%d（按放行处理）: %v", task.ID, err)
		return nil
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
// 无从证明，只能判红。只传一句文案就会把后者也糊成全绿（批6 立项要消灭的那类假绿）。
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

// isWriteStep 判「这一步是不是不可逆写」= 编排声明 ∪ 原语推导，两者取并集且推导必赢：
// 声明位交给编排方/LLM 用，而 LLM 把发送步标成 is_write=false 就能摘掉服务端的重试闸门，
// 所以推导（命中平台注册定位表）只能追加、不能被声明撤销。why 供日志与归因。
//
// 推导规则刻意收窄（相对设计稿 §3.3）：type+submit_on_enter 只在目标命中平台注册的
// comment_input 定位表时才算写——真机交互搜索腿同样是「输入框 + 回车」，把一切回车都判成写，
// 会白白剥掉搜索步的重试能力并在重试轮里错误跳过。
func isWriteStep(task *model.BrowserTask, step parsedStep) (bool, string) {
	if step.Action == "post_comment" {
		return true, "action=post_comment"
	}
	locs, _ := platformStepLocators(task)
	switch step.Action {
	case "type":
		if step.SubmitOnEnter && locatorMatches(locs["comment_input"], step.Target) {
			return true, "derived=type+enter→comment_input"
		}
	case "click":
		if locatorMatches(locs["send_button"], step.Target) {
			return true, "derived=click→send_button"
		}
	case "click_near":
		if step.ButtonText != "" && step.ButtonText == locs["send_button_text"] {
			return true, "derived=click_near→send_button_text"
		}
	}
	if step.IsWrite {
		return true, "declared=is_write"
	}
	return false, ""
}

// writeStepKey 写步的台账键：有正文用正文（评论文本本身），无正文的发送动作（点发送按钮）
// 用定位三元组兜底——同一任务里同一个发送位反复出现，本就等同于「同一次提交」。
func writeStepKey(step parsedStep) string {
	if h := HashWriteText(step.Value); h != "" {
		return h
	}
	return HashWriteText(step.Action + "\x00" + step.Target + "\x00" + step.Anchor + "\x00" + step.ButtonText)
}

// platformStepLocators 取平台的定位表（推导写步的唯一依据）。取不到就返回空表——
// 推导规则随之全部不命中，只剩声明位与 post_comment，不会因为适配器缺失而误判写步。
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
// *_inject_timeout_ 是注入从未执行，两者都没有副作用，台账因此留空（= 不算尝试，可安全重下发）。
// 反过来，WS 超时/未知错误一律不算：超时不等于没发生，那正是双发的形状。
func isNeverExecuted(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "_inject_timeout_") || strings.Contains(msg, "_not_found")
}

// recordGenericWriteLedger 非 post_comment 写步的台账落点。post_comment 有自己的
// prepared→sent→终态 三段归因（含 finalize 回查），不在此列。
// 成功只记 sent 不记 verified：这些原语没有回查通路，宣称「已发布」就是假绿。
func (e *Executor) recordGenericWriteLedger(ctx context.Context, step parsedStep, stepRowID uint, textHash string, dispatchErr error) {
	if step.Action == "post_comment" || stepRowID == 0 {
		return
	}
	state := model.StepSubmitSent
	switch {
	case dispatchErr == nil:
	case isNeverExecuted(dispatchErr):
		return // 从未发生：不记尝试，留给重下发
	default:
		state = model.StepSubmitUnattributed // 结果未知 → 记成尝试，交人来判
	}
	e.recordSubmitState(ctx, stepRowID, state, textHash)
}
