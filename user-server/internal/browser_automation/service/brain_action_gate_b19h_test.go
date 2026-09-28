package service

// 契约锁：Brain 轮内每个步骤在落库前先过服务端闸门。
//
// 落点在 executeBrain 的步循环：LLM 输出的 steps 是 `json.Unmarshal` 出来的 dto.StepItem，
// 这条路上 gin 的 binding 一行都不跑（oneof 只在 HTTP 入口生效）。所以模型幻觉出
// `hover` / `drop_pin` 这种动作名时，旧行为是照原样建一条 browser_steps 行、发下去、
// 在 dispatchStep 的 default 上炸「未知动作」——库里留下一条注定失败的步骤，
// 前台渲染成一个用户看不懂的红灯，而这次幻觉还白烧了一次 CDP 往返。
// 收口方式和 G17 的 screenshot 闸同型：**不信任模型输出，落库前判掉**，判掉的写历史
// 回喂（LLM 下轮能看见自己被拒了什么，才知道换动作），静默跳过则永远不会自我纠正。
//
// 三道锁各管一段漂移：
//   1. 闸门本身的分腿（放行/拒绝/拒绝文案有界）；
//   2. 白名单 == dispatchStep 的 case 集合——REST 收得下但执行器跑不了，是同一个洞的另一半；
//   3. prompt 动作表 ⊆ 白名单——护栏让模型用的动作，服务端不许拒。

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"hivemtk-user/internal/browser_automation/dto"
)

func TestB19HBrainPlanStepRejectionLegs(t *testing.T) {
	// screenshot 的文案逐字锁死：它是 G17 就已存在的拒绝，历史回喂文案变了会改变 LLM 下轮的
	// 观察策略选择（"请用 snapshot/markdown" 这句是特意写的），不是可顺手改的字符串。
	if got := brainPlanStepRejection("screenshot"); got !=
		"screenshot → 被拒绝（Brain 模式禁止抢焦点截图，请用 snapshot/markdown 观察）" {
		t.Errorf("screenshot 拒绝文案变了：\n got=%q\nwant=%q", got,
			"screenshot → 被拒绝（Brain 模式禁止抢焦点截图，请用 snapshot/markdown 观察）")
	}
	// 除 screenshot 外，白名单内动作一律放行（返回空串）。
	// 空 action 不在这里放行：旧代码 `it.Action == ""` 静默 continue，模型收不到任何反馈，
	// 于是下一轮照样吐空步——拒它是信息，跳过它只是把它藏起来。
	for _, a := range []string{
		"open_tab", "click", "type", "click_near", "post_comment",
		"snapshot", "markdown", "wait", "wait_for_selector", "scroll",
		"extract", "assert", "query", "close_tab",
	} {
		if got := brainPlanStepRejection(a); got != "" {
			t.Errorf("白名单动作 %q 被拒（%q）：闸门与 oneof 不同源，Brain 直接少一个能力", a, got)
		}
	}
	for _, bad := range []string{"", " ", "hover", "download_file", "Click", "click ", "click\nrm"} {
		got := brainPlanStepRejection(bad)
		if got == "" {
			t.Errorf("动作 %q 被放行，want 拒：它会原样落库再到 dispatchStep 炸成失败步骤", bad)
			continue
		}
		if !strings.Contains(got, "被拒绝") {
			t.Errorf("拒绝文案 %q 不含「被拒绝」：回喂给 LLM 的一行必须能被读成拒绝", got)
		}
	}
}

// TestB19HRejectionEchoBounded 拒绝文案里回显的是模型原文，长度不受我们控制。
// 历史窗口按条数折叠（historyWindow）不按字数，滑窗溢出的行还会以「动作名」为键进折叠台账——
// 一个 5000 字的幻觉 action 会同时撑大 prompt 和台账键名。所以回显必须截断。
func TestB19HRejectionEchoBounded(t *testing.T) {
	huge := strings.Repeat("very_made_up_action_", 400) // 8000 字符
	got := brainPlanStepRejection(huge)
	if utf8.RuneCountInString(got) > 200 {
		t.Errorf("拒绝文案 %d 字符，want ≤200：未截断模型原文，长幻觉会直接进 prompt 与折叠台账键名",
			utf8.RuneCountInString(got))
	}
	if strings.Contains(got, huge) {
		t.Errorf("拒绝文案原样回显了模型超长串：%s", got)
	}
}

// b19hDispatchCases 从 executor.go 里抠出 dispatchStep 的 case 动作集合。
// 为什么读源码而不是跑一遍：跑一遍要为 15 个动作各配一条真 Host/CDP 回包，那是 e2e 的活；
// 而这里要锁的只是「switch 认得的字符串」这一事实本身，它就在源码里写着。
// 边界取「下一个顶层 func」，不然会把 post_comment 分支里嵌套 switch 的 case 一起收进来。
func b19hDispatchCases(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatalf("读 executor.go 失败：%v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (e *Executor) dispatchStep(")
	if start < 0 {
		t.Fatal("找不到 dispatchStep：它改名/挪文件了，本锁的判据已失效，请连锁一起迁")
	}
	rest := src[start:]
	if end := strings.Index(rest[len("func (e *Executor) dispatchStep("):], "\nfunc "); end >= 0 {
		rest = rest[:end+len("func (e *Executor) dispatchStep(")]
	}
	cases := map[string]bool{}
	for _, m := range regexp.MustCompile(`\n\tcase "([a-z_]+)":`).FindAllStringSubmatch(rest, -1) {
		cases[m[1]] = true
	}
	if len(cases) == 0 {
		t.Fatal("dispatchStep 里一个 case 都没抠到：正则失配，空集会让下面的比对恒真")
	}
	return cases
}

func TestB19HWhitelistMatchesDispatchCases(t *testing.T) {
	cases := b19hDispatchCases(t)
	for a := range cases {
		if !dto.IsKnownStepAction(a) {
			t.Errorf("执行器会跑 %q，但 binding oneof 不认它：Brain 闸门拒了，"+
				"而 REST 建的任务里它本可以出现——两处对同一个动作的判定相反", a)
		}
	}
	for a := range dto.KnownStepActions {
		if !cases[a] {
			t.Errorf("oneof 放行 %q，但 dispatchStep 没有对应 case：REST 能把这种步写进任务，"+
				"落库后必在 default 分支失败（本批要堵的洞在 REST 侧的原样复现）", a)
		}
	}
}

// b19hPromptActions 从 planActionTable 抠「告知模型可用」的动作名：
// 每个 bullet 的说明段首词（"snapshot / markdown" 这类并列拆成两个），只留 [a-z_]+ 形状的词。
func b19hPromptActions(t *testing.T) []string {
	t.Helper()
	word := regexp.MustCompile(`^[a-z_]+$`)
	var out []string
	for _, line := range strings.Split(planActionTable, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		for _, seg := range strings.Split(line[2:], "；") {
			name := strings.TrimSpace(seg)
			if i := strings.Index(name, "："); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			for _, one := range strings.Split(name, "/") {
				one = strings.TrimSpace(one)
				if word.MatchString(one) {
					out = append(out, one)
				}
			}
		}
	}
	if len(out) < 10 {
		t.Fatalf("从动作表只解析出 %d 个动作名，解析失配会让本锁恒真（当前解析到：%v）", len(out), out)
	}
	return out
}

func TestB19HPromptActionsAreWhitelisted(t *testing.T) {
	for _, a := range b19hPromptActions(t) {
		if !dto.IsKnownStepAction(a) {
			t.Errorf("prompt 教模型用 %q，闸门却不认：模型每轮照做就每轮被拒，"+
				"这条护栏变成死循环发生器", a)
		}
	}
}
