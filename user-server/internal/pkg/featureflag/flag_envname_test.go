package featureflag

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// 这组用例守的是"提示的名字 == 读的名字 == 人写得出来的名字"这一条链。
//
// 起因是白名单旗子 `ai.safety.tool_approval_gate`：flag 名里带点号是合法的，但拼出来的
// 环境变量名 `FF_AI.SAFETY.TOOL_APPROVAL_GATE` **在 shell 里 export 不了**（不是合法标识符），
// 于是运维端点照着 EnvNameOf 打印的那份提示去开旗子，永远开不上 —— 而端点上读到的
// `whitelist_flag_on=false` 看起来像"没人批准过这些账号"，排查方向整个错位。

// approvalFlagKeyShape 与 internal/approval 的 FlagKey 同形（点号分层 + 下划线尾段）。
// 这里不 import 那个包：判据属于"名字怎么拼"这一层，反过来依赖业务包会把依赖方向接反。
const approvalFlagKeyShape = "ai.safety.tool_approval_gate"

var shellIdentRe = regexp.MustCompile(`^FF_[A-Z0-9_]+$`)

func TestEnvNameOf_IsSettableInShell(t *testing.T) {
	for _, name := range []string{
		approvalFlagKeyShape, "parallel", "sse_bridge", "a.b.c", "with-dash", "with space",
	} {
		got := EnvNameOf(name)
		if !shellIdentRe.MatchString(got) {
			t.Errorf("EnvNameOf(%q) = %q，不是可用的 shell 变量名（export 会直接报错）", name, got)
		}
	}
	// 具体那一个的名字钉死：提示语、文档、用例三处引用的是同一个串。
	if got := EnvNameOf(approvalFlagKeyShape); got != "FF_AI_SAFETY_TOOL_APPROVAL_GATE" {
		t.Errorf("EnvNameOf(%q) = %q，期望 FF_AI_SAFETY_TOOL_APPROVAL_GATE", approvalFlagKeyShape, got)
	}
}

// TestEnvNameOf_DottedFormIsNotExportable 把"点号那份真的开不上"钉成事实，
// 而不是靠人记得 shell 标识符规则。少了这一格，上面那个归一化就只是"我觉得更好"。
//
// 判据是纯字符串判定：shell 变量名只允许 [A-Za-z_][A-Za-z0-9_]*（这里前缀 FF_ 已给，
// 故要求 [A-Z0-9_]），任何点号/横线/空格都不合法。不再起 bash 进程 —— 既避免
// 动态命令串（命令注入面），也让判据与平台无关（Windows 上没有可执行的 bash 也能跑）。
func TestEnvNameOf_DottedFormIsNotExportable(t *testing.T) {
	dotted := legacyEnvName(approvalFlagKeyShape)
	if shellIdentRe.MatchString(dotted) {
		t.Errorf("点号旧名 %q 不该是合法的 shell 变量名 —— 那归一化就没必要，本用例的前提要重新审", dotted)
	}
	if got := EnvNameOf(approvalFlagKeyShape); !shellIdentRe.MatchString(got) {
		t.Errorf("归一化名 %q 仍不是合法的 shell 变量名", got)
	}
	// 任意非法字符都应被归一化成下划线：点号、横线、空格三种都覆盖到。
	for _, name := range []string{"a.b", "with-dash", "with space"} {
		if got := EnvNameOf(name); !shellIdentRe.MatchString(got) {
			t.Errorf("EnvNameOf(%q) = %q，非法字符未被归一化", name, got)
		}
	}
}

// TestFlag_ReadsNormalizedEnvName 判据落在真读数上，而不是只落在字符串拼接上：
// 环境变量按归一化名设置 ⇒ 旗子必须为真（这是本次修的故障本身）。
func TestFlag_ReadsNormalizedEnvName(t *testing.T) {
	os.Unsetenv(legacyEnvName(approvalFlagKeyShape))
	os.Setenv("FF_AI_SAFETY_TOOL_APPROVAL_GATE", "1")
	defer func() {
		os.Unsetenv("FF_AI_SAFETY_TOOL_APPROVAL_GATE")
		os.Unsetenv(legacyEnvName(approvalFlagKeyShape))
	}()
	refreshAll()
	if !Get(approvalFlagKeyShape).Bool() {
		t.Errorf("设了 %s=1 之后旗子仍为 false ⇒ 读的名字与提示的名字不是同一个", "FF_AI_SAFETY_TOOL_APPROVAL_GATE")
	}
}

// TestFlag_StillReadsLegacyDottedName 点号那份**不是**死代码：compose/k8s 的 env 映射允许
// 点号键名，照着旧提示写过的人必须继续生效。归一化若只认新名字，那把旗子会静默从
// 开着掉回关着（安全闸门往失败方向漂），且没有任何一处报错。
func TestFlag_StillReadsLegacyDottedName(t *testing.T) {
	os.Unsetenv(EnvNameOf(approvalFlagKeyShape))
	os.Setenv(legacyEnvName(approvalFlagKeyShape), "1")
	defer func() {
		os.Unsetenv(legacyEnvName(approvalFlagKeyShape))
		os.Unsetenv(EnvNameOf(approvalFlagKeyShape))
	}()
	refreshAll()
	if !Get(approvalFlagKeyShape).Bool() {
		t.Errorf("只设了点号旧名 %s 时旗子应为真（旧写法不许静默失效）", legacyEnvName(approvalFlagKeyShape))
	}
}

// TestFlag_NormalizedNameWins 两份都设时新名优先：运维在端点上看到的名字开成 off，
// 就应当真的是 off，而不是被一条没人记得写过的旧变量顶着。
func TestFlag_NormalizedNameWins(t *testing.T) {
	os.Setenv(EnvNameOf(approvalFlagKeyShape), "0")
	os.Setenv(legacyEnvName(approvalFlagKeyShape), "1")
	defer func() {
		os.Unsetenv(EnvNameOf(approvalFlagKeyShape))
		os.Unsetenv(legacyEnvName(approvalFlagKeyShape))
	}()
	refreshAll()
	if Get(approvalFlagKeyShape).Bool() {
		t.Error("新名=0 应压过旧名=1：提示的那份才是运维看得见并会去改的那一个")
	}
}

// TestEnvNameOf_SSEBridgeFlagIsTheDocumentedKnob 钉住「文档里那个回退开关真的存在」。
//
// Go 侧常量写作 FF_ENABLE_SSE_BRIDGE，而它承载的 flag 名是 "sse_bridge" ⇒ 拼出来的变量名
// 是 FF_SSE_BRIDGE。照常量名去设 FF_ENABLE_SSE_BRIDGE=0 不起任何作用，桥接下行照旧走 SSE，
// 而运维以为已经切回轮询了。所以名字要钉死，且设置后必须真读到翻转——只测字符串拼接的话，
// readEnv 换成读常量名字符串这一格仍然是绿的。
func TestEnvNameOf_SSEBridgeFlagIsTheDocumentedKnob(t *testing.T) {
	const env = "FF_SSE_BRIDGE"
	if got := EnvNameOf(FF_ENABLE_SSE_BRIDGE); got != env {
		t.Fatalf("EnvNameOf(%q) = %q，运维手册写的是 %s", FF_ENABLE_SSE_BRIDGE, got, env)
	}

	prev, had := os.LookupEnv(env)
	t.Cleanup(func() {
		if had {
			os.Setenv(env, prev)
		} else {
			os.Unsetenv(env)
		}
		refreshAll()
	})

	// 前置：默认必须是开着的，否则下面的"设 0 变 false"没有判别力。
	if !Get(FF_ENABLE_SSE_BRIDGE).Bool() {
		t.Fatalf("前置不成立：未设置 %s 时 %q 应为默认 true（本机 env 里带着它？）", env, FF_ENABLE_SSE_BRIDGE)
	}

	os.Setenv(env, "0")
	refreshAll()
	if Get(FF_ENABLE_SSE_BRIDGE).Bool() {
		t.Errorf("设了 %s=0 后旗子仍为 true ⇒ 手册写的轮询回退开关是假的", env)
	}

	os.Unsetenv(env)
	refreshAll()
	if !Get(FF_ENABLE_SSE_BRIDGE).Bool() {
		t.Errorf("撤掉 %s 后应回到默认 true，实际 false ⇒ 上一格的红不是这格测出来的", env)
	}
}

// TestEnvNameOf_NoCollisionInRegistry 归一化会把 `a.b` 与 `a_b` 并成同一个变量名。
// 现在没有这种成对的名字，但**新加旗子的人会踩**：撞名之后两个旗子共用一份 env，
// 开一个就开了另一个，而两处都读不出这件事。这一格是那条的棘轮。
func TestEnvNameOf_NoCollisionInRegistry(t *testing.T) {
	seen := map[string]string{}
	names := make([]string, 0, len(DefaultManager().flags))
	for name := range DefaultManager().flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env := EnvNameOf(name)
		if other, dup := seen[env]; dup {
			t.Errorf("旗子 %q 与 %q 共用环境变量名 %s ⇒ 一次设置会同时翻转两把旗子", other, name, env)
			continue
		}
		seen[env] = name
	}
}
