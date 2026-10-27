package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
	"hivemtk-user/internal/pkg/utils/logger"
)

// executor_dispatch.go —— 按动作分发到 Hand 原语 + 回包收口 + 提取合并 + finalize 验证轮询。
// dispatchStep 按动作分发到 Hand 原语。
// 回包（snapshot/extract/markdown/screenshot 等）作为返回值交 executeStepWithRetry 落库——
// 不挂 Executor 字段：Executor 是进程级单例、多 session 并发触达（R-A4 竞态修复）。
// stepRow：本步的 DB 行（含 ID），写原语分支用它落 submit_state 台账。
// seq：session 局部命令号——post_comment 的 D7 闸门在分支内部，它要落的 d7_wait/d7_confirm 帧
// 与外层命令帧共用同一个号段（计数器不下放给闸门，否则两处各自编号必然撞号）。
// outreachCtx：写步去重键（executeStepWithRetry 的去重检查点冻结），verified 后落去重行用；
// 非写步恒为 nil，落库入口自行判空短路。
func (e *Executor) dispatchStep(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, step parsedStep, stepRow *model.BrowserStep, seq *int, outreachCtx *outreachDedupeCtx) ([]byte, error) {
	userID := task.UserID
	tabID := session.ChromeTabID
	p := buildStepParams(step)

	switch step.Action {
	case "open_tab":
		openURL := step.Target
		if openURL == "" {
			openURL = task.Url // 编排未填 target 时兜底任务起始 URL
		}
		tabID, res, err := e.hand.openTab(ctx, userID, openURL, false, step.TimeoutMs) // active 恒 false
		if err != nil {
			return nil, err
		}
		session.ChromeTabID = tabID
		_ = e.sessionRepo.UpdateChromeTabID(ctx, session.ID, tabID)
		// page_loaded 原样透传（可能是 nil：老 Host 不回这个字段时如实记 null，
		// 不能把「不知道」写成 false，也不能反过来把 false 洗成 true）。
		// load_wait_ms/title/create_ms 一并留档：真机 session611 真站 open_tab 往返 26159ms
		// 而 step.duration 已把整段吞掉，缺扩展侧「等加载花了多久」就无法把慢归因到
		// tabs.create+waitForLoad 还是链路；session619-621 的 30s 黑盒更是在
		// tabs.create 挂死时连 create 耗时都拿不到——create_ms 就是那案的归因字段。
		// title 用来确认打开的确实是目标页。字段缺席（老 Host 不回）时原样不写，不编造 0。
		payload := map[string]any{"chrome_tab_id": tabID, "page_loaded": res["loaded"]}
		if v, ok := res["load_wait_ms"]; ok && v != nil {
			payload["load_wait_ms"] = v
		}
		if v, ok := res["title"]; ok && v != nil {
			payload["title"] = v
		}
		if v, ok := res["create_ms"]; ok && v != nil {
			payload["create_ms"] = v
		}
		return recordResultPayload(payload)
	case "click":
		// 写步才请求点后身份复核。判据取 stepRow.IsWrite 而不是在这里重算
		// classifyStepEffect：那一列是本步落库时写死的事实，重试轮、双发闸、D7 都读它——
		// 在这里重算就等于允许「闸门认它是写、请求复核时认它不是写」这种裂脑存在。
		res, err := e.hand.click(ctx, userID, tabID, step.Target, stepRow.IsWrite)
		if err != nil {
			return nil, err
		}
		// channel 如实透传：cdp / dom_fallback / null（老扩展没这个字段）。
		// 兜底本身不是失败，但「一片绿里全是 dom_fallback」= trusted 通道死了，
		// 这一列就是用来发现它死了的（实证形态）。
		// identity_checked 同口径：true=这次写步点击真的过了点后复核；null=旧扩展没这个概念。
		// 不加这一列，「复核跑过」与「复核从没被请求」在库里长得一样（立项理由）。
		return recordResultPayload(map[string]any{
			"navigated": res["navigated"] == true, "channel": res["channel"],
			"identity_checked": res["identity_checked"],
		})
	case "type":
		res, err := e.hand.typeText(ctx, userID, tabID, step.Target, step.Value, p.ClearFirst, p.SubmitOnEnter)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{"channel": res["channel"]})
	case "click_near":
		// 以 Anchor CSS 为基准点击容器内指定文本的 button（发送/提交按钮无稳定 class 场景）
		res, err := e.hand.clickNear(ctx, userID, tabID, step.Anchor, step.ButtonText, stepRow.IsWrite)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(map[string]any{
			"clicked": res["clicked"] == true, "channel": res["channel"],
			"identity_checked": res["identity_checked"],
		})
	case "post_comment":
		// F2②（G11 正确版）：三段式拆分——prep（可重入）→ send（唯一不可逆点，F2① 已禁重试）
		// → verify 轮询 finalize（只读、可中断、可归因）。提交与验证彻底分离：
		// verify 超时 ≠ 重试提交（Postiz maximumAttempts:1 红线）；finalize 落库证据闭环。
		// 选择器四元组由平台适配器（L3）下发——扩展零平台知识（设计稿 P5）；
		// 未声明 post_comment 能力的平台在此 fails-loudly（P4 契约默认失败）。
		locs, err := platform.CommentLocatorsFor(ctx, taskPlatformID(task))
		if err != nil {
			return nil, err
		}
		// 提交前基线（E1：零提交假绿修复 / E21：拿不到基线不再降级）。判据必须是**新旧**，
		// 不是「有没有」。实测 session644：comment_send 回 element_moved（点已派发、落点位移）
		// → 平台侧没有新评论，而 finalize 的 comment_verify 命中了**提交前就存在的**那条同文本
		// 评论（同账号对同一条笔记重复发同一句话），回查判 verified=true、回执落 verified=true
		// ——一条根本没发出去的提交拿到了「已发布」的结论，并且再被双发闸锁死
		// （verified 在拦阻集合内），任务重跑也发不出去。
		//
		// 位置：任何写动作之前（prep 只是往输入框填字，不改评论条数），所以基线与提交点之间
		// 唯一的差异就是「这次提交有没有发生」。取不到基线 = 新旧判据无从建立 = **拒绝提交**：
		// 旧实现在这里降级成「有没有」判据 + recency_unproven 留痕，等于把假绿换成了
		// 「有标签的假绿」——留痕不改变结论，回执照样 verified=true。所以那条岔路整条删了。
		// 读两遍（只读、可安全重复）：extract 偶发失败不该把一个健康任务判死。
		var baseMatched int
		var baseErr error
		for attempt := 0; attempt < 2; attempt++ {
			baseMatched, baseErr = e.countCommentMatches(ctx, userID, tabID, locs, step.Value)
			if baseErr == nil {
				break
			}
		}
		if baseErr != nil {
			return nil, fmt.Errorf("post_comment 未提交（提交前基线不可用：%v）——新旧判据无从建立，本次点击从未发生", baseErr)
		}
		*seq++
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", "comment_baseline",
			map[string]any{"pre_matched": baseMatched, "item_selector": locs.CommentItemText}, 0, verdict(true))
		// 双发闸在 executeStepWithRetry 下发任何帧之前已过（起对全部写步统一生效）。
		textHash := writeStepKey(step)
		prepReq := map[string]any{
			"input_selector":   locs.InputSelector,
			"send_button_text": locs.SendButtonText,
		}
		if _, err := e.hand.commentPrep(ctx, userID, tabID, step.Value, prepReq); err != nil {
			// A1 自愈一次：输入框定位失效（改版）→ LLM 按快照重选；选不中或已自愈过 → 原样上抛
			if !e.healCommentInput(ctx, task, session, tabID, prepReq, err) {
				return nil, err
			}
			if _, err2 := e.hand.commentPrep(ctx, userID, tabID, step.Value, prepReq); err2 != nil {
				return nil, err2
			}
		}
		// prep 成功=文本已进输入框，点击仍未发生 → prepared（可安全重下发态，也是
		// 「闸门/中止腿从未提交」的正面证据）
		// 这一行写不进去就不再往下走一格。prepared 不在拦阻集合内，send 之后
		// 库里若还是空的，下一轮（含同进程自动重试）就查不到任何尝试——那正是双发的形状。
		if err := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitPrepared, textHash, false); err != nil {
			return nil, fmt.Errorf("post_comment 未提交（prepared 台账未落，点击从未发生，本会话写能力已降级）: %w", err)
		}
		// D7 人工确认闸门：require_confirm=true 的任务在此挂起，等 POST /sessions/:id/confirm
		// 放行后才进不可逆提交点。挂起期间只有 prep（填文本，页面内可撤销、零平台副作用）；
		// 未放行即中止=从未提交，可安全重下发。
		// 等待用 task.confirm_wait_sec 独立预算，不再吃 task.TimeoutSec（那条管自动化本身）。
		if task.RequireConfirm {
			out, why := e.awaitConfirmGate(ctx, task, session, stepRow.ID, stepRow.StepIndex,
				step.Value, textHash, seq, confirmWaitBudget(task))
			switch out {
			case confirmGranted:
			case confirmStoppedByUser:
				return nil, errors.New(errConfirmAbortedByStop)
			default: // confirmWaitTimedOut：确认预算或执行预算先到头，why 里写明是哪条
				return nil, fmt.Errorf("post_comment 等待人工确认超时（%s），评论未提交", why)
			}
		}
		// 不可逆提交点。send 结局分两类归因（R26-2 竞速超时使边界可判）：
		// ① *_inject_timeout=按钮定位注入未执行→点击从未发生→无副作用，直接判失败早返
		//    （不进 finalize 白轮 16s，也不污染证据——错误文本自证「未点击」可安全重下发）；
		// ② 其余任何结局（成功/出错/WS 超时）→点击可能已发生=结果未知→必须 finalize 回查：
		//    Postiz 心跳判因矩阵语义——超时≠未发生，回查是唯一合法归因路径，绝不重新提交。
		// 每次真实提交都成对落两帧：command 帧=attempted 且不带结论，
		// event 帧=accepted。自愈重发的那一次同样落帧——「全场只提交一次」是运行期红线，
		// 不是审计期红线：审计流要如实有几场就几场，藏起来等于让读包的人猜。
		// sendRes=最后一次 comment_send 回包，扩展的点后复核结论从它身上取；
		// 判据是回包里的 identity_checked 而不是「我们请求过复核」——请求发出去 ≠ 复核跑成了，
		// 旧扩展或复核给不出结论时如实记 null（click / click_near 同口径）。
		var sendRes map[string]any
		sendOnce := func() error {
			*seq++
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "command", commentSendAction,
				map[string]any{"action": commentSendAction, "tab_id": tabID, "payload_hash": textHash}, 0, nil)
			start := time.Now()
			res, err := e.hand.commentSend(ctx, userID, tabID, prepReq, stepRow.IsWrite)
			*seq++
			e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "event", commentSendAction,
				map[string]any{"error": sendErrText(err)}, time.Since(start).Milliseconds(), verdict(err == nil))
			// 留最后一次回包：自愈重发过的那一次才是这一步的结论，前一次的复核结论已经不作数
			sendRes = res
			return err
		}
		sendErr := sendOnce()
		// ① 那一类在传输层还有第三种形态——这一帧根本没写进 socket（D7 等多分钟，
		// Host 掉在等待里是常态）。此处刻意**不**就地重发一次：判据成立即 Host 已经没了，
		// 微秒级的第二次调用只会拿到同一条错误；恢复归任务级重试（台账留 prepared ⇒ 不在
		// 拦阻集合 ⇒ 下一轮重跑会重新 prep、重新过一次闸门，那是更严而不是更松的路径）。
		if isCommandNeverOnWire(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（命令未上线，点击从未发生，可安全重下发）: %w", sendErr)
		}
		if isInjectTimeout(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（页面注入拥堵，点击未发生）: %w", sendErr)
		}
		if isSendGateReject(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（发送按钮不可点，点击未发生）: %w", sendErr)
		}
		if isNeverDispatched(sendErr) {
			return nil, fmt.Errorf("post_comment 未提交（CDP 未 attach，事件从未下发，可安全重下发）: %w", sendErr)
		}
		// 提交点已跨越：立即落 sent，不等 finalize 的结论。理由——「send 之后 execCtx 恰好到期」
		// 是最坏窗口（步被判超时、终态归因模糊），此时台账若还没写，重下发就没有任何拦阻。
		// 上面三个早返分支（未上线/注入超时/闸门拒点）台账都留在 prepared：那三支点击从未发生。
		// 这次写失败=「越点未落账」，recordSubmitState 会同时记下进程内兜底缺口，
		// 本步最终判红（见下面 sentLedgerErr）——撤不回了，但至少不再有人替我们假设它没发生。
		sentLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, model.StepSubmitSent, textHash, true)
		// A1 自愈一次：send_button_not_found=按钮从未命中=点击从未发生（同 R26-2 归因），
		// 重发不违「单次提交禁重试」红线；其余错误结局未知，交 finalize 回查绝不重发。
		if sendErr != nil && e.healCommentSendButton(ctx, task, session, tabID, prepReq, sendErr) {
			sendErr = sendOnce()
		}
		verified, evidence := e.finalizeComment(ctx, userID, tabID, step.Value, locs, baseMatched, e.stopChFor(session.ID))
		// 回查结论落台账：verified 是唯一可对外宣称「已发布」的态；未见即 unattributed
		// （已尝试、归因不到）——unattributed 仍在双发闸的拦截集合内，交人来判。
		finalState := model.StepSubmitUnattributed
		if verified {
			finalState = model.StepSubmitVerified
		}
		finalLedgerErr := e.recordSubmitState(ctx, task.ID, session.ID, stepRow.ID, finalState, textHash, true)
		// 审计流的第三态：跨过提交点后，无论回查见没见都要落一帧结论。
		// 落在台账写之后、ledgerErr 早返之前——「库里台账没写全」是 ledger 的问题，
		// 不改变「平台侧回查见了/没见」这个已经观测到的事实，两件事各自留痕。
		*seq++
		e.appendCommandLog(ctx, session.ID, task.ID, stepRow.ID, *seq, "judge", writeConfirmFrame, map[string]any{
			"payload_hash": textHash,
			"step_index":   stepRow.StepIndex,
			"state":        finalState,
		}, 0, verdict(verified))
		// finalize 证据落 extracted_data（追溯面板 + I4 续跑位点：重放可见「哪条评论已提交已验证」）
		e.mergeExtract(ctx, session, "post_comment", map[string]any{
			"text":        step.Value,
			"send_error":  sendErrText(sendErr),
			"verified":    verified,
			"evidence":    evidence,
			"pre_matched": baseMatched,
			"posted_at":   time.Now().Format(time.RFC3339),
		})
		// Chunk5 回执挂起（不落库，收口时一起落，见 captureSessionReceipts）：
		// 放在 mergeExtract 之后、两条结论分支之前——verified 与 unattributed 都是
		// 「已经发出去了」的事实，只有一边留回执的话，验收面就成了替平台做裁决。
		e.enqueueOutreachReceipt(ctx, task, session, step, stepRow, outreachCtx, evidence, verified)
		// 台账缺口优先于「看起来成功」：verified 的回查证据是真的，但库里没有这次提交，
		// 下一轮的同文本重发就不会被拦——所以这里不能给绿。判红不等于宣称失败，
		// 文案里把「回查见/未见」原样带上，人一眼能判该不该补这条台账。
		if ledgerErr := errors.Join(sentLedgerErr, finalLedgerErr); ledgerErr != nil {
			seen := "回查未见评论（结果未知）"
			if verified {
				seen = "回查已见评论（内容确已发布）"
			}
			return nil, fmt.Errorf("post_comment %s，但提交台账未落全（%v）——本会话写能力已降级，重跑本任务前请人工核对该评论是否已发布", seen, ledgerErr)
		}
		if verified {
			// Chunk3 跨任务触达去重落行：published 是事实，行落不落得住不改这个事实
			// （warn-only）。行在，下一个任务撞见同目标同文案就会跳过。
			recordOutreachDedupeSend(ctx, e, task, session, outreachCtx)
			return recordResultPayload(map[string]any{
				"posted": true, "verified": true, "evidence": evidence,
				// finalize 只回答「我的文字上去没」，identity_checked 回答的是
				// 「点下去的是不是发送按钮」——两问各自留痕。复核没过（element_moved）而回查见了评论时，
				// 这一列是 null、send_error 在 extracted_data 里，绿得有据可查，不是把结论洗掉。
				"identity_checked": sendRes["identity_checked"],
			})
		}
		// fails-loudly：提交结局未知或验证未见——归因「平台静默吞/审核中/渲染超时」，
		// 步判失败但不重试（重试=双发）。
		// 归因文案要能自己说明「为什么这次不算发布」：stale_match=回查命中的是提交前
		// 就存在的那条同文本评论（本次提交没有任何正面证据），别让人去翻 evidence 才知道。
		why := ""
		if evidence != nil {
			if stale, _ := evidence["stale_match"].(bool); stale {
				why = fmt.Sprintf("（回查命中的是提交前就已存在的同文本评论：提交前 %v 条 → 提交后 %v 条，本次提交无正面证据）",
					evidence["pre_matched"], evidence["post_matched"])
			}
		}
		if sendErr != nil {
			return nil, fmt.Errorf("post_comment 提交命令异常（%v）且回查未见新评论%s——结果未知，不重试防双发", sendErr, why)
		}
		return nil, fmt.Errorf("post_comment 已提交但回查未见新评论%s（可能被平台拦截/审核中），不重试防双发", why)
	case "assert":
		// 断言类原语（洞察层）：contains_text / selector_exists，失败即抛错（Playwright expect 语义）
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 5000
		}
		return nil, e.hand.assert(ctx, userID, tabID, p.AssertKind, step.Value, timeout)
	case "query":
		// 只读洞察原语：text/exists/count/attr，返回数据不抛错（Midscene 洞察类语义）
		res, err := e.hand.query(ctx, userID, tabID, p.QueryKind, step.Target, p.Attribute)
		if err != nil {
			return nil, err
		}
		return recordResultPayload(res)
	case "snapshot":
		snap, pageURL, err := e.hand.snapshot(ctx, userID, tabID)
		if err != nil {
			return nil, err
		}
		_ = e.sessionRepo.UpdateSnapshot(ctx, session.ID, snap)
		return recordResultPayload(map[string]any{"snapshot_chars": len(snap), "snapshot": snap, "page_url": pageURL})
	case "markdown":
		res, err := e.hand.markdown(ctx, userID, tabID)
		if err != nil {
			return nil, err
		}
		md, _ := res["markdown"].(string)
		// 截断标志如实上抛：markdown 是「快照太长时改用 markdown 取全文」的出口
		// （brain_budget 的截断提示就是这么写的），只记本地长度会把 64KiB 截断值当成整页，
		// 而 len() 是字节数、扩展侧 markdown_chars 是字符数，两者对中文差 3 倍。
		return recordResultPayload(map[string]any{
			"markdown":       md,
			"markdown_chars": res["markdown_chars"],
			"full_chars":     res["full_chars"],
			"truncated":      res["truncated"],
			"content_empty":  res["content_empty"],
		})
	case "screenshot":
		// G17 定稿（二验修正）：captureVisibleTab 只能截「当前激活 tab」且不报错——
		// 不激活直接截会静默截到用户正在看的页面（假内容），降级方案不成立。
		// 因此：激活是正确性必需，抢焦点的收口放在「频度」——护栏禁止 Brain 轮内使用
		// screenshot（观察用 snapshot/markdown），用户显式编排/终态留证时才会激活一次。
		b64, err := e.hand.screenshot(ctx, userID, tabID, true)
		if err != nil {
			return nil, err
		}
		if b64 != "" && e.feedback != nil {
			if url, err := e.feedback.SaveFinalScreenshot(ctx, session.ID, b64); err != nil {
				logger.Warnf("[BrowserExec] 截图落库失败 session=%d: %v", session.ID, err)
			} else if url != "" {
				session.FinalScreenshotURL = url
				_ = e.sessionRepo.UpdateArtifacts(ctx, session.ID, nil, url, "")
			}
		}
		return recordResultPayload(map[string]any{"screenshot_url": session.FinalScreenshotURL, "screenshot_b64_chars": len(b64)})
	case "wait":
		return nil, e.hand.waitFor(ctx, userID, tabID, p.Ms)
	case "wait_for_selector":
		timeout := p.TimeoutMs
		if timeout <= 0 {
			timeout = 10000
		}
		return nil, e.hand.waitForSelector(ctx, userID, tabID, p.Selector, timeout)
	case "scroll":
		return nil, e.hand.scroll(ctx, userID, tabID, p.Direction, p.Amount)
	case "extract":
		// Brain 兼容：LLM 常把单选择器放 target 而不是 selectors map——基座层归一（铁律 3：大模型驱动容错）
		selectors := p.Selectors
		if len(selectors) == 0 && strings.TrimSpace(step.Target) != "" {
			selectors = map[string]string{"target": strings.TrimSpace(step.Target)}
		}
		data, err := e.hand.extract(ctx, userID, tabID, selectors)
		if err != nil {
			return nil, err
		}
		// 合并本 session 各次 extract 到 extracted_data（追溯面板读这里）
		merged, ok := data["data"].(map[string]any)
		if !ok {
			merged = map[string]any{"raw": data}
		}
		e.mergeExtract(ctx, session, "", merged)
		return recordResultPayload(merged)
	case "close_tab":
		if err := e.hand.closeTab(ctx, userID, tabID); err != nil {
			return nil, err
		}
		// 内存 tab id 归零（DB 值保留作审计）：收口回收据此判定「已回收」，
		// 否则编排里的 close_tab + 收口清理会对同一 tab 发两帧。
		session.ChromeTabID = 0
		return nil, nil
	default:
		return nil, fmt.Errorf("未知动作: %s", step.Action)
	}
}

// recordResultPayload 序列化原语回包（R-A4：纯函数，经 dispatchStep 返回值传递，
// 不再挂 Executor 字段）。marshal 失败显式抛错——旧 recordResult 吞错会让步静默
// 落库空 result，追溯面板永远看不到内容。
func recordResultPayload(payload map[string]any) ([]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	return json.Marshal(payload)
}

// mergeExtract 合并写 session.extracted_data（追溯面板读这里）。
// key 非空时嵌套在 key 名下（如 post_comment 证据），空时直接顶层合并（extract 原语语义）。
func (e *Executor) mergeExtract(ctx context.Context, session *model.BrowserSession, key string, payload map[string]any) {
	existing := map[string]any{}
	if len(session.ExtractedData) > 0 {
		_ = json.Unmarshal(session.ExtractedData, &existing)
	}
	if key != "" {
		// 同名 key 多次提交 → 追加为数组（评论可能一条任务发多条）
		prev, had := existing[key]
		if !had {
			existing[key] = []any{payload}
		} else if arr, ok := prev.([]any); ok {
			existing[key] = append(arr, payload)
		} else {
			existing[key] = []any{prev, payload}
		}
	} else {
		for k, v := range payload {
			existing[k] = v
		}
	}
	blob, err := json.Marshal(existing)
	if err == nil {
		_ = e.sessionRepo.UpdateExtractedData(ctx, session.ID, blob)
		session.ExtractedData = blob
	}
}

// finalizeComment F2② finalize：提交后的只读验证轮询（Postiz pending→checkPostStatus→finalize 语义）。
// 短超时多次 comment_verify——每次都是只读命令，可安全重试/可中断；绝不重新提交。
// 轮询窗口 = 首验 6s + 复核 2×5s（覆盖评论区异步审核后回显的实测节奏），stopCh 关闭即放弃。
// 返回 (verified, evidence)：evidence=最后一次验证回包（含命中容器数/条目文本节选）。
//
// 新旧判据（E1）：comment_verify 只回答「评论区页面上有没有这句话」，它回答不了
// 「这句话是不是我这次发上去的」。同账号对同一条笔记重复发同一句话时，提交前这一问
// 就已经是 true —— 于是任何一次「点没点中都不知道」的提交都能拿到 verified=true
// （session644 实测：comment_send 回 element_moved、平台上没有新评论，回查命中 27 分钟前
// 那条同文本评论，回执落 verified=true，再被双发闸锁死）。因此：
//   - baseMatched（提交前同文本条目数）在这里一定是**已知**的：调用方在任何写动作之前
//     就把它读到了，读不到直接拒提交（E21）。因此回查命中后必须**再数一次**，严格多于
//     基线才算本次提交的证据；不多就记 stale_match 继续轮询（不放弃、不判绿）。
//   - 不存在「退化判据」这条岔路：一条连新旧都分不清的 verified 不是结论，删掉比留痕强
//     （留痕不改变结论，回执照样 verified=true —— 那就是带标签的假绿）。
func (e *Executor) finalizeComment(ctx context.Context, userID uint, tabID int, text string, locs platform.CommentLocators, baseMatched int, stopCh chan struct{}) (bool, map[string]any) {
	rounds := []int{6000, 5000, 5000}
	var last map[string]any
	for i, timeoutMs := range rounds {
		if ctx.Err() != nil || (stopCh != nil && e.stopFired(stopCh)) {
			return false, last
		}
		res, err := e.hand.commentVerify(ctx, userID, tabID, text, locs.CommentContainer, locs.CommentItemText, timeoutMs)
		if err != nil {
			logger.Warnf("[BrowserExec] comment_verify 第%d轮出错（继续轮询）user=%d tab=%d: %v", i+1, userID, tabID, err)
			continue
		}
		last = res
		verified, _ := res["verified"].(bool)
		if !verified {
			continue
		}
		// 「见了」≠「新见的」。数一次，必须严格多于提交前的同文本条目数。
		now, cErr := e.countCommentMatches(ctx, userID, tabID, locs, text)
		if cErr != nil {
			// 数不出来就不判绿：基线已知却没拿到回数，证据链缺一格，继续轮询等它可读。
			logger.Warnf("[BrowserExec] comment_verify 命中后回数失败（第%d轮，继续轮询）user=%d tab=%d: %v", i+1, userID, tabID, cErr)
			res["count_error"] = cErr.Error()
			last = res
			continue
		}
		res["pre_matched"] = baseMatched
		res["post_matched"] = now
		if now <= baseMatched {
			// 命中的是提交前就存在的那条：这不是本次提交的证据（漏判=不可逆动作的假绿）。
			res["stale_match"] = true
			logger.Warnf("[BrowserExec] comment_verify 命中的是旧评论（提交前 %d 条 → 提交后 %d 条），不判 verified user=%d tab=%d", baseMatched, now, userID, tabID)
			last = res
			continue
		}
		return true, res
	}
	return false, last
}

// countCommentMatches 数「评论区里文本与目标相同的条目」有几条。
// 用 extract 而不是 query count：query 只能数 CSS 命中数，分不清「这条是我刚发的」
// 还是「同一句话的历史评论」——那正是假绿的形状。extract 回的是逐条文本，
// 在 Go 侧按归一化后精确相等计数（与扩展 comment_verify 的 own 判据同口径），
// 无精确命中时退化为包含匹配（与 verified 的 contains 语义同口径）。
// 只用于**前后对比**：两次测量同口径即可，绝对值本身不进结论。
func (e *Executor) countCommentMatches(ctx context.Context, userID uint, tabID int, locs platform.CommentLocators, text string) (int, error) {
	sel := strings.TrimSpace(locs.CommentItemText)
	if sel == "" {
		return 0, errors.New("平台适配器未声明 comment_item_text（无条目选择器可用）")
	}
	want := normalizeCommentText(text)
	if want == "" {
		return 0, errors.New("目标文本为空，无法建立计数基线")
	}
	res, err := e.hand.extract(ctx, userID, tabID, map[string]string{"comment_items": sel})
	if err != nil {
		return 0, err
	}
	raw, ok := res["data"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("extract 回包缺少 data（%T）", res["data"])
	}
	items, ok := raw["comment_items"].([]any)
	if !ok {
		// 0 条时扩展回的是空数组字面量，类型仍是 []any；走到这里说明契约变了，如实报错。
		return 0, fmt.Errorf("extract 回包 comment_items 不是数组（%T）", raw["comment_items"])
	}
	exact, contains := 0, 0
	for _, it := range items {
		s, _ := it.(string)
		s = normalizeCommentText(s)
		if s == "" {
			continue
		}
		if s == want {
			exact++
		} else if strings.Contains(s, want) {
			contains++
		}
	}
	// 精确相等优先：它才是「一句话一模一样的另一条评论」这个判据。
	if exact > 0 {
		return exact, nil
	}
	return contains, nil
}

// normalizeCommentText 去掉所有空白后再比：评论正文里的换行/缩进/全角空格
// 在 innerText 与 textContent 两条取文路径上并不一致（扩展侧 norm 同口径）。
func normalizeCommentText(s string) string {
	return strings.Join(strings.Fields(s), "")
}
