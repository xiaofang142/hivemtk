// ego-runner — HiveMtk ego-lite 腿的本机执行代理（设计文档 §4.3 R1/R2b，调研 §6.5）。
//
// 拓扑：本机 daemon ←outbound WS→ user-server :8204 /api/egolite/runner-ws
// 职责（刻意薄）：conf 加载 → WS 连接（eg_ token + device_id）→ 收 task spec →
// 校验（hash/域名/版本）→ 受限环境组装 round 脚本 → exec ego-browser nodejs →
// stdout 事件行逐行上报。无状态机、无业务决策——大脑在服务端（调研 §6.5 定案）。
//
// 鉴权（调研 §6.5 定案）：
//   - token 形如 eg_<userID>_<rand>，userID 内嵌，握手即绑定归属（照抄 host_token.go）；
//   - 回环门按部署形态开关：conf 里 loopback_only=true 时本地自检（runner 本就在本机，
//     服务端侧的回环门由服务端实现；此处形态甲仅影响 conf 提示）；
//   - register 帧携带 device_id（首次安装生成、持久化）+ hostname + runner/ego 版本。
//
// 不做的事（纪律，见设计文档 §一"禁止的"）：
//   - 不实现执行状态机/重试策略/confirm 决策——服务端状态机是唯一事实源；
//   - 不解析页面内容——只透传结构化事件行（自述级审计，G-E5 口径）；
//   - 不持久化任务数据。
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ---------- 配置（~/.hivemtk/ego_runner.conf，key=value 行，同 nm_host.conf 形状） ----------

type runnerConf struct {
	ServerURL     string // ws(s)://host:8204
	Token         string // eg_<userID>_<rand>
	WorkspaceDir  string // learnings workspace 根（EGO_BROWSER_AGENT_WORKSPACE 指向）
	DeviceID      string // 首次安装生成，持久化
	EgoBin        string // ego-browser 可执行文件路径
	EgoServerName string // 可选：--ego-server-name（E3：官方唯一跨实例选择手段，多实例并存时防连错）
	RepoDir       string // 可选：user-server 源码目录（repo_dir=...）——配置后 upgrade 帧追加 runner 自更新
	LoopbackOnly  bool   // 形态甲标记（调研 §6.5；runner 侧仅记录与自检提示）
}

func confPath() string {
	if p := os.Getenv("EGO_RUNNER_CONF"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hivemtk", "ego_runner.conf")
}

func deviceIDPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hivemtk", "ego_runner_device_id")
}

func loadConf() (*runnerConf, error) {
	p := confPath()
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("打不开配置 %s（参考 nm_host.conf 的 key=value 格式）: %w", p, err)
	}
	defer f.Close()
	c := &runnerConf{
		ServerURL: "ws://127.0.0.1:8204",
		// T2 勘误后定案：workspace=ego skill 物理目录（learnings 解包点），不再走 env 重定向
		WorkspaceDir: filepath.Join(mustHome(), ".local", "share", "ego", "ego-skills"),
		EgoBin:       "ego-browser",
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "server_url":
			c.ServerURL = v
		case "token":
			c.Token = v
		case "workspace_dir":
			c.WorkspaceDir = v
		case "device_id":
			c.DeviceID = v
		case "ego_bin":
			c.EgoBin = v
		case "ego_server_name":
			c.EgoServerName = v
		case "repo_dir":
			c.RepoDir = v
		case "loopback_only":
			c.LoopbackOnly = v == "true"
		}
	}
	if c.Token == "" {
		return nil, errors.New("conf 缺 token= 行（eg_ 前缀，admin 生成）")
	}
	if !strings.HasPrefix(c.Token, "eg_") {
		return nil, errors.New("token 不是 eg_ 前缀——runner-ws 与 host-ws 的 token 不通用")
	}
	// device_id 持久化（调研 §6.5 形态乙补偿控制③）
	if c.DeviceID == "" {
		if b, err := os.ReadFile(deviceIDPath()); err == nil && len(strings.TrimSpace(string(b))) == 36 {
			c.DeviceID = strings.TrimSpace(string(b))
		} else {
			c.DeviceID = uuid.NewString()
			_ = os.MkdirAll(filepath.Dir(deviceIDPath()), 0o700)
			_ = os.WriteFile(deviceIDPath(), []byte(c.DeviceID), 0o600)
		}
	}
	return c, nil
}

func mustHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// ---------- task spec（服务端下发，调研 §6.1：不下发脚本全文，正式通道） ----------

type taskSpec struct {
	ExecutionID   string          `json:"execution_id"`
	AssetID       string          `json:"asset_id"`
	AssetVersion  string          `json:"asset_version"`
	Entry         string          `json:"entry"` // 资产内 entry 工具名或文件相对路径
	Params        json.RawMessage `json:"params"`
	SpaceName     string          `json:"space_name"`
	SpaceID       string          `json:"space_id,omitempty"`
	ConfirmPolicy string          `json:"confirm_policy"` // none / require（M2 全量实现，本骨架透传）
	ConfirmTTL    int             `json:"confirm_ttl_sec"`
	TimeoutSec    int             `json:"timeout_sec"`
	Round         int             `json:"round"` // resume 时 >1
	ResumeSpaceID string          `json:"resume_space_id,omitempty"`
	Origin        string          `json:"origin"` // market / draft / session
	DraftScript   string          `json:"draft_script,omitempty"`
	Domains       []string        `json:"domains"` // 域名白名单（specs/015：运行时域审计数据源；下发时刻快照）
	// ScriptHash 服务端批准 hash 快照（specs/017①：market 下发时刻快照）。非空时与本地
	// manifest 的 hivemtk.script_hash 交叉核对，拦"本地工作区滞留旧版本资产照旧组装"
	// （完整性校验管"装完被改"，这里管"装的就是旧版"）。旧服务端不下发该字段，空值跳过。
	ScriptHash string `json:"script_hash,omitempty"`
	// 通用执行能力（2026-10-28）：ops 轮次协议
	SessionID string          `json:"session_id,omitempty"`
	Mode      string          `json:"mode,omitempty"` // ops（默认空=script）
	Ops       json.RawMessage `json:"ops,omitempty"`
	Observe   json.RawMessage `json:"observe,omitempty"`
}

// ---------- 事件行（A3 协议；runner 只透传+补 seq，不自产结论） ----------

type eventLine struct {
	Seq       int             `json:"seq"`
	Type      string          `json:"type"` // action/snapshot/artifact/handoff/extract/confirm/done/error/runner
	Asset     string          `json:"asset,omitempty"`
	Execution string          `json:"execution_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Stderr    string          `json:"stderr,omitempty"` // runner 级错误（非资产自述）
	ExitCode  int             `json:"exit_code,omitempty"`
}

// ---------- WS 客户端（照抄 host_registry client 侧纪律：outbound、退避、心跳） ----------

const (
	pongWait        = 90 * time.Second // 服务端读超时参数沿用（调研 §6.5）；保活靠服务端 30s ping（runner_ws.go pingLoop）
	dialTimeout     = 10 * time.Second
	reconnectBase   = 2 * time.Second
	reconnectCap    = 60 * time.Second
	execHardTimeout = 30 * time.Minute // R6：单次执行硬超时（wall-clock 看门狗）
)

type runner struct {
	conf      *runnerConf
	conn      *websocket.Conn
	writeMu   sync.Mutex   // 同时守 conn 与断线缓冲（同一把锁保证补传与实时帧不交错）
	current   *execContext // 同一时刻至多一个执行（服务端并发闸是真正限制，这里防本地踩踏）
	currentMu sync.Mutex
	pending   []eventLine // 断线缓冲：conn 为空期间的事件行（DF-4⑤），重连后补传
	dropped   int         // 缓冲溢出丢弃计数（补传时先补一条 runner 说明帧）
}

type execContext struct {
	spec         taskSpec
	cancel       context.CancelFunc
	exitCode     int
	decisionPath string // 事前阻塞确认闸的裁决文件（allow/deny 由 confirm 帧写入；每执行独立临时目录）
}

func (r *runner) send(ev eventLine) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.conn == nil {
		// 断线缓冲（DF-4⑤）：脚本在 ego lite 内继续跑，事件行不能因为连接断开就
		// 从审计里消失——本地暂存，重连后按原 seq 补传（服务端 append-only 只增不覆盖）。
		r.enqueuePendingLocked(ev)
		return
	}
	_ = r.conn.WriteJSON(ev)
}

// egoPendingCap 缓冲上限（条）。超限丢最老——长时间断线不该拖垮 runner 内存；
// 丢过的条数在补传时以 runner 说明帧如实上报（审计诚实：不能装作没丢过）。
const egoPendingCap = 5000

func (r *runner) enqueuePendingLocked(ev eventLine) {
	if len(r.pending) >= egoPendingCap {
		r.pending = r.pending[1:]
		r.dropped++
	}
	r.pending = append(r.pending, ev)
}

// drainPending 取走全部待补传帧与溢出计数（调用方须持 writeMu 或专用于测试）。
func (r *runner) drainPending() ([]eventLine, int) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	evs, dropped := r.pending, r.dropped
	r.pending, r.dropped = nil, 0
	return evs, dropped
}

// flushPending 重连后补传（连接建立后、进入读循环前调用一次）。
func (r *runner) flushPending() {
	events, dropped := r.drainPending()
	if len(events) == 0 && dropped == 0 {
		return
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.conn == nil {
		// 取帧与写帧之间连接又断了：放回缓冲等下一次重连，不能让这批帧凭空消失
		for _, ev := range events {
			r.enqueuePendingLocked(ev)
		}
		r.dropped += dropped
		return
	}
	if dropped > 0 {
		_ = r.conn.WriteJSON(eventLine{Type: "runner", Payload: mustJSON(map[string]any{
			"note": "断线期间缓冲溢出，丢弃最老事件", "dropped": dropped})})
	}
	for _, ev := range events {
		_ = r.conn.WriteJSON(ev)
	}
}

// ---------- 执行（校验 → 组装 → exec → 透传） ----------

func (r *runner) runTask(spec taskSpec) {
	fmt.Fprintf(os.Stderr, "[ego-runner] exec=%s origin=%s entry=%s round=%d\n",
		spec.ExecutionID, spec.Origin, spec.Entry, spec.Round)
	r.currentMu.Lock()
	if r.current != nil {
		r.currentMu.Unlock()
		r.send(eventLine{Type: "runner", Execution: spec.ExecutionID,
			Payload: mustJSON(map[string]string{"error": "runner_busy：本机已有在途执行，服务端并发闸应先拦"})})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ec := &execContext{spec: spec, cancel: cancel}
	r.current = ec
	r.currentMu.Unlock()
	defer func() {
		r.currentMu.Lock()
		r.current = nil
		r.currentMu.Unlock()
		cancel()
	}()

	// 硬超时看门狗（R6；服务端另有状态机看门狗，双保险）
	if spec.TimeoutSec > 0 {
		tctx, tcancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSec)*time.Second)
		defer tcancel()
		ctx = tctx
	} else {
		tctx, tcancel := context.WithTimeout(ctx, execHardTimeout)
		defer tcancel()
		ctx = tctx
	}

	script, cleanup, err := r.assembleScript(spec)
	if err != nil {
		r.send(eventLine{Type: "error", Execution: spec.ExecutionID, Stderr: "组装失败: " + err.Error()})
		return
	}
	defer cleanup()
	// 裁决文件与脚本同目录（assembleScript 的执行级临时目录）：路径可由脚本路径确定性派生，
	// 不改 assembleScript 签名——目录每执行独立创建，天然无跨执行残留/伪造面。
	ec.decisionPath = confirmDecisionPath(script)

	code, errOut := r.execEgo(ctx, cancel, spec, script)
	r.send(eventLine{Type: "done", Execution: spec.ExecutionID, ExitCode: code, Stderr: truncate(errOut, 2000)})
}

// assembleScript 组装 round 脚本。骨架版规则：
//   - origin=draft：写 conf 的 DraftScript 到临时文件（正式扫描门/模板包装在 T13 落地；
//     本骨架先按调研 §6.1④ 限制：仅本 user 的 draft 会到达这里——服务端职责）
//   - origin=market：从 workspace 读资产 entry 文件
//
// 脚本模板（R4 全量）M2 落地；骨架版透传 entry 内容并以最小模板包装。
func (r *runner) assembleScript(spec taskSpec) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "ego-exec-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	var entrySrc string
	switch {
	case spec.Mode == "ops":
		entrySrc = buildOpsScript(spec, dir)
	case spec.Origin == "draft":
		entrySrc = spec.DraftScript
	case spec.Origin == "market":
		assetDir := filepath.Join(r.conf.WorkspaceDir, "learnings", spec.AssetID)
		entryPath := filepath.Join(assetDir, spec.Entry)
		manifestRaw, err := os.ReadFile(filepath.Join(assetDir, "manifest.json"))
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("读 manifest 失败（资产未安装？workspace=%s）: %w", assetDir, err)
		}
		b, err := os.ReadFile(entryPath)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("读资产 entry 失败（workspace=%s）: %w", assetDir, err)
		}
		// 两级校验，顺序即语义：
		//   ① 完整性（永远查）：entry 的 sha256 必须等于 manifest 里的 script_hash——
		//      拦"装完之后本机被改"；
		//   ② 来源认证（manifest 带签名才查）：ed25519 验签只服务将来跨实例官方包通道。
		// 为什么不是无条件验签：本轮定案把签名链移出主链路（资产由本实例管理端发布，
		// 信任源就是本实例），无条件验签会让"资产中心点运行"在正常部署下必然失败
		// ——那不是安全，是把一条腿做成死的。
		if err := verifyAssetAt(assetDir, manifestRaw, b); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("资产校验失败，拒绝执行: %w", err)
		}
		// 服务端批准快照交叉核对（specs/017①）：spec 带批准 hash 时，本地 manifest
		// 必须同源——完整性校验管"装完之后本机被改"，这里管"装进来的就是旧版本"。
		if err := verifySpecConsistency(manifestRaw, spec); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("资产与服务端批准版本不一致，拒绝执行: %w", err)
		}
		entrySrc = string(b)
	default:
		cleanup()
		return "", nil, fmt.Errorf("未知 origin=%q", spec.Origin)
	}
	// 运行时收敛预检（specs/015 C2 第一层；扫描门 EGO-R001..R014 的 runner 侧镜像）：
	// 服务端扫描门管"发布前"，这里管"执行前"——拦截工作区被篡改/历史脏数据漏网的
	// 已知恶意模式。与服务端同一份规则语义，runner 不 import server internal 包
	// （纪律），漂移由 scan_test 样例集钉死。
	if violated := firstEntryBlocker(entrySrc); violated != "" {
		cleanup()
		return "", nil, fmt.Errorf("运行时收敛预检拒绝执行：命中 %s", violated)
	}

	// round 脚本前导：把执行上下文写成**脚本常量**。
	//
	// 唯一可行的注入方式就是"写进文本"——F13 实测：ego-browser 内嵌 Node 不继承
	// shell env、cwd 恒为 /、argv 是 Helper 可执行文件本身。此前 params / space_name
	// 只到 spec 就丢了，表现为"UI 填的参数脚本收不到"，静默降级成默认值。
	//
	// 契约（资产脚本可用）：
	//   __EXEC__            本次执行键（ego-<row>-<ms>，脚本打点可回填）
	//   __PARAMS__          调用方参数（JSON 对象；未传为 {}）
	//   __SPACE_NAME__      space 名（空串=不指定，脚本自行起名）；同名的 space 会续跑，
	//                       这是多轮任务的唯一衔接点（每轮新进程，变量不持久）
	//   __CONFIRM_FILE__    事前阻塞确认闸的裁决文件（每执行独立临时目录，无残留面）
	//   __CONFIRM_TTL_SEC__ 确认等待上限（秒）；到期按拒绝，与服务端看门狗同口径
	//   __awaitConfirm__    写提交点前调用：false=用户拒绝或确认超时，脚本必须立即收口
	paramsJSON := "{}"
	if len(spec.Params) > 0 {
		var probe any
		if json.Unmarshal(spec.Params, &probe) == nil && probe != nil {
			paramsJSON = string(spec.Params)
		}
	}
	ttlSec := spec.ConfirmTTL
	if ttlSec <= 0 {
		ttlSec = egoConfirmDefTTLSec
	}
	p := filepath.Join(dir, "flow-round01.mjs")
	decisionPath := confirmDecisionPath(p)
	// __DOMAINS__ 空数组 = 零域（fail-close：守卫 helper 全拒；绕过 helper 的裸
	// goto/fetch 由事件域审计兜底拦截）
	domainsJSON := "[]"
	if len(spec.Domains) > 0 {
		if b, err := json.Marshal(spec.Domains); err == nil {
			domainsJSON = string(b)
		}
	}
	preamble := fmt.Sprintf(
		"// ego-runner assembled @%s execution=%s origin=%s round=%d\n"+
			"const __EXEC__ = %q;\n"+
			"const __PARAMS__ = %s;\n"+
			"const __SPACE_NAME__ = %q;\n"+
			"const __SPACE_ID__ = %q;\n"+
			"const __CONFIRM_FILE__ = %q;\n"+
			"const __CONFIRM_TTL_SEC__ = %d;\n"+
			"const __CONFIRM_POLICY__ = %q;\n"+
			"const __ARTIFACT_DIR__ = %q;\n"+
			"const __DOMAINS__ = %s;\n"+
			confirmHelperJS+domainHelperJS,
		time.Now().UTC().Format(time.RFC3339), spec.ExecutionID, spec.Origin, spec.Round,
		spec.ExecutionID, paramsJSON, spec.SpaceName, spec.SpaceID, decisionPath, ttlSec,
		spec.ConfirmPolicy, dir, domainsJSON)
	script := preamble + entrySrc

	if err := os.WriteFile(p, []byte(script), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return p, cleanup, nil
}

const egoConfirmDefTTLSec = 300

// egoColdStartHintAfter 冷启动静默哨阈值（见 execEgo 内注释）
const egoColdStartHintAfter = 60 * time.Second

const confirmDecisionFile = "confirm-decision.json"

// confirmDecisionPath 裁决文件路径（与脚本同目录，路径由脚本路径确定性派生）
func confirmDecisionPath(scriptPath string) string {
	return filepath.Join(filepath.Dir(scriptPath), confirmDecisionFile)
}

// confirmHelperJS 注入 round 脚本的事前阻塞确认闸 helper。
// 只认 decision==="allow" 为放行（fail-close）：文件损坏/半写/未知值一律不放行；
// TTL 到期返回 false，由脚本自行收口（进程被 runner kill 是兜底，不是依赖）。
const confirmHelperJS = `// —— 事前阻塞写确认闸（HiveMTK runner 注入；契约见 specs/014-egolite-blocking-confirm-gate）——
// 写提交点前调用：if (!(await __awaitConfirm__("动作名"))) { 跳过写动作，打印 ok:false 审计行 }
// 注意：false 时**不要 process.exit**（扫描门 EGO-R013 禁止，且会伪造 done 终态）——
// 跳过写、正常结束即可；终态由服务端裁决（deny/TTL 已判 stopped）。
// 未调用本函数的脚本保持既有行为（乐观执行 + 服务端可杀死）。
async function __awaitConfirm__(label) {
  console.log(JSON.stringify({ type: "confirm", action: String(label ?? ""), execution: __EXEC__, ts: Date.now() }));
  const { readFile } = await import("node:fs/promises");
  const deadline = Date.now() + Math.max(1, __CONFIRM_TTL_SEC__) * 1000;
  while (Date.now() < deadline) {
    try {
      const d = JSON.parse(await readFile(__CONFIRM_FILE__, "utf8"));
      if (d.decision === "allow") return true;
      if (d.decision === "deny") return false;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  return false;
}
`

// domainHelperJS 注入 round 脚本的域名守卫（specs/015 C2 协作层）：
// 裸 page.goto/page.fetch 无法从 runner 侧拦截（ego 注入的原语），但守卫 helper
// 给脚本一条"被审计的通路"；绕过 helper 的导航由 runner 事件域审计兜底（auditEventURL）。
// 语义与 Go 侧 domainAllowed 同源：整站 * 放行；精确域或子域命中；www. 归一。
const domainHelperJS = `// —— 域名守卫（HiveMTK runner 注入；specs/015）——
function __domainAllowed__(url) {
  try {
    const host = new URL(url).hostname.toLowerCase();
    if (__DOMAINS__.includes("*")) return true;
    return __DOMAINS__.some((d) => {
      const dom = String(d).toLowerCase().trim().replace(/^www\./, "");
      return dom !== "" && (host === dom || host.endsWith("." + dom));
    });
  } catch { return false; }
}
async function __gotoChecked__(page, url) {
  if (!__domainAllowed__(url)) throw new Error("domain_violation: " + url + " 不在声明 domains 内（用 __gotoChecked__ 或扩 domains）");
  return page.goto(url);
}
async function __fetchChecked__(page, url, opts) {
  if (!__domainAllowed__(url)) throw new Error("domain_violation: " + url + " 不在声明 domains 内（用 __fetchChecked__ 或扩 domains）");
  return page.fetch(url, opts);
}
`

// eventTypeFromPayload 从脚本打印的 JSON 行里取事件类型。
//
// 为什么必须透传而不是一律 "action"：服务端的写操作闸靠事件类型推进状态机——
// 只有 type=confirm 才会进 waiting_confirm（再由 /confirm 裁决、TTL 到期按拒绝收口）。
// 此前所有行都被标成 action，等于把"写动作逐步确认"做成了装饰：状态永远到不了
// waiting_confirm，deny 也永远不会下发。
//
// 白名单以外的类型一律归 action：脚本作者可能顺手写 {"type":"log"}，
// 不能让它凭空造出服务端不认识的状态迁移。
func eventTypeFromPayload(raw json.RawMessage) string {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "action"
	}
	switch probe.Type {
	case "action", "snapshot", "artifact", "handoff", "extract", "confirm", "round_result":
		return probe.Type
	default:
		return "action"
	}
}

// execEgo 执行 ego-browser nodejs（stdin 喂脚本——T2 实测：CLI 不接受文件参数，只认
// `ego-browser nodejs < script` 或 `-e`）；stdout 逐行扫描透传，stderr 聚合。
// T2 实测勘误（F6 降级）：EGO_BROWSER_AGENT_WORKSPACE 环境变量**不穿透**到内嵌 Node
// （探针 pid 稳定复现 unset）——资产分发改为 SyncPull 直接解包到物理目录
// ~/.local/share/ego/ego-skills/learnings/{site}/，脚本内以物理路径访问（实测可读）。
func (r *runner) execEgo(ctx context.Context, cancel context.CancelFunc, spec taskSpec, scriptPath string) (int, string) {
	cmd := exec.CommandContext(ctx, r.conf.EgoBin, egoNodeArgs(r.conf)...)
	script, err := os.Open(scriptPath)
	if err != nil {
		r.send(eventLine{Type: "error", Execution: spec.ExecutionID, Stderr: "打开脚本失败: " + err.Error()})
		return -1, err.Error()
	}
	defer script.Close()
	cmd.Stdin = script
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		r.send(eventLine{Type: "error", Execution: spec.ExecutionID,
			Stderr: "启动 ego-browser 失败（ego lite 是否已安装并启动？）: " + err.Error()})
		return -1, err.Error()
	}

	// T2 实测勘误（A3 协议实现事实）：ego-browser CLI 把内嵌 Node 的脚本 stdout 转发到
	// **CLI 自身的 stderr**（CLI stdout 分离实测为空）——事件行扫描必须在 stderr 流。
	// 两流都扫：stderr 扫事件行 + 兼做错误缓冲；stdout 扫描保留（未来版本可能纠正流分离）。
	var wg sync.WaitGroup
	seq := 0
	var seqMu sync.Mutex
	errBuf := &strings.Builder{}
	// 冷启动静默哨（任务一 e2e 实测发现形态）：CLI 子进程可能停在 ego lite 的
	// 客户端授权门/未响应上——进程活着但**零输出**直到超时，用户侧只看到"一直下发中"。
	// 60s 无任何脚本输出即上报一次 runner 提示事件（只提示一次，不干预执行）。
	firstEvent := make(chan struct{})
	closeOnce := sync.Once{}
	go func() {
		t := time.NewTimer(egoColdStartHintAfter)
		defer t.Stop()
		select {
		case <-firstEvent:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			r.send(eventLine{Type: "runner", Execution: spec.ExecutionID, Payload: mustJSON(map[string]string{
				"note": "ego-browser 已启动但 60s 无脚本输出：请检查 ego lite 是否弹出授权确认（需在 GUI 点一次允许）、是否未登录或未响应",
			})})
		}
	}()
	// 统一事件出口：补 seq → 透传 → 运行时域审计（specs/015 C2）。
	// 事件 payload 带 url 且越域 ⇒ fail-close：error 事件 + cancel 终止执行
	// （脚本侧守卫 helper 是协作层，这里是强制层）。
	handleEvent := func(raw json.RawMessage) {
		closeOnce.Do(func() { close(firstEvent) })
		seqMu.Lock()
		seq++
		n := seq
		seqMu.Unlock()
		r.send(eventLine{Seq: n, Type: eventTypeFromPayload(raw), Execution: spec.ExecutionID, Payload: raw})
		if badURL, ok := auditEventURL(spec.Domains, raw); !ok {
			r.send(eventLine{Type: "error", Execution: spec.ExecutionID,
				Stderr: "domain_violation: 事件 URL 越域（" + badURL + "），按 fail-close 终止执行"})
			cancel()
		}
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		// 先判事件行；非事件行进错误缓冲
		// （逐行双职：事件行不进 errBuf，避免 done.stderr 里混入已结构化的内容）
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" && strings.HasPrefix(line, "{") {
				var raw json.RawMessage
				if json.Unmarshal([]byte(line), &raw) == nil {
					handleEvent(raw)
					continue
				}
			}
			if errBuf.Len() < 2000 {
				errBuf.WriteString(line + "\n")
			}
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			var raw json.RawMessage
			if json.Unmarshal([]byte(line), &raw) != nil {
				continue
			}
			handleEvent(raw)
		}
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		r.send(eventLine{Type: "error", Execution: spec.ExecutionID, Stderr: "执行超时被 kill（wall-clock 看门狗）"})
		return -1, "timeout"
	}
	wg.Wait()

	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return code, errBuf.String()
}

// ---------- 主循环：连接 → register → 读下发帧 ----------

func (r *runner) connect(ctx context.Context) error {
	wsURL, err := url.Parse(r.conf.ServerURL)
	if err != nil {
		return fmt.Errorf("server_url 非法: %w", err)
	}
	wsURL.Path = "/api/egolite/runner-ws"
	q := wsURL.Query()
	q.Set("token", r.conf.Token)
	wsURL.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, _, err := dialer.DialContext(ctx, wsURL.String(), nil)
	if err != nil {
		return err
	}
	r.conn = conn
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))

	// register 帧（调研 §6.5：device_id + hostname + 版本）
	egoVer := probeEgoVersion(r.conf.EgoBin)
	_ = conn.WriteJSON(map[string]any{
		"type":        "register",
		"version":     "0.1.0",
		"device_id":   r.conf.DeviceID,
		"hostname":    mustHostname(),
		"ego_version": egoVer,
		"workspace":   r.conf.WorkspaceDir,
	})
	return nil
}

// egoNodeArgs 组装 ego-browser 的 argv：--ego-server-name 必须前置在 nodejs 子命令之前
// （官方 CLI 唯一跨实例选择手段，调用机制调研 E3；多实例并存时防连错实例）。
func egoNodeArgs(c *runnerConf) []string {
	args := make([]string, 0, 2)
	if c.EgoServerName != "" {
		args = append(args, "--ego-server-name="+c.EgoServerName)
	}
	return append(args, "nodejs")
}

// probeEgoVersion 版本探测。F11 实测：ego-browser --version 把版本打到 **stderr**
// （stdout 为空）——Output() 只收 stdout 会恒得空串，register 帧的 ego_version 就
// 恒为 unknown，A5 版本协商随之失真。改用 CombinedOutput 取首行，剥掉 "ego-browser"
// 前缀只留版本号；探不到如实报 unknown。
func probeEgoVersion(egoBin string) string {
	out, err := exec.Command(egoBin, "--version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return strings.TrimSpace(strings.TrimPrefix(line, "ego-browser"))
}

func mustHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func main() {
	confFile := flag.String("conf", "", "配置文件路径（默认 ~/.hivemtk/ego_runner.conf）")
	once := flag.Bool("once", false, "连接一次并保持直到 Ctrl+C（PoC 模式）")
	check := flag.Bool("check", false, "只核对配置与 ego-browser 可达性，不连接（对齐 install.sh --check 模式）")
	flag.Parse()

	if *confFile != "" {
		os.Setenv("EGO_RUNNER_CONF", *confFile)
	}
	c, err := loadConf()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
	if *check {
		fmt.Printf("server_url=%s\nworkspace=%s\ndevice_id=%s\nloopback_only=%v\n",
			c.ServerURL, c.WorkspaceDir, c.DeviceID, c.LoopbackOnly)
		if c.EgoServerName != "" {
			fmt.Printf("ego_server_name=%s（多实例并存时按名连接）\n", c.EgoServerName)
		}
		if c.RepoDir != "" {
			fmt.Printf("repo_dir=%s（升级时追加 runner 自更新）\n", c.RepoDir)
		}
		fmt.Printf("ego-browser: %s (%s)\n", c.EgoBin, probeEgoVersion(c.EgoBin))
		if _, err := os.Stat(c.WorkspaceDir); err != nil {
			fmt.Printf("⚠️ workspace 不存在（安装资产后自动创建）: %s\n", c.WorkspaceDir)
		}
		return
	}

	fmt.Printf("ego-runner 0.1.0 | server=%s | device=%s | ego=%s\n",
		c.ServerURL, c.DeviceID, probeEgoVersion(c.EgoBin))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := &runner{conf: c}
	// 资产同步（T3）：启动即拉一次，之后每 10 分钟一轮（幂等，只写变化项）。
	// 独立于 WS 生命周期——WS 断了资产目录仍应是最新的。
	r.pullAssets()
	go func() {
		ticker := time.NewTicker(assetPullInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.pullAssets()
			}
		}
	}()
	backoff := reconnectBase
	for {
		if ctx.Err() != nil {
			return
		}
		err := r.connect(ctx)
		if err == nil {
			fmt.Println("✅ 已连接，等待 task spec…")
			backoff = reconnectBase
			r.flushPending() // 断线期间缓冲的事件行先补传（DF-4⑤），再进读循环
			readLoop(r, ctx, *once)
		}
		if ctx.Err() != nil {
			return
		}
		fmt.Printf("⚠️ 连接断开（%v），%s 后重连\n", err, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff *= 2
		if backoff > reconnectCap {
			backoff = reconnectCap
		}
	}
}

// readLoop 读服务端下发帧；骨架版只认 task spec 与 ping。
func readLoop(r *runner, ctx context.Context, once bool) {
	defer func() {
		r.writeMu.Lock()
		if r.conn != nil {
			_ = r.conn.Close()
			r.conn = nil
		}
		r.writeMu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		_ = r.conn.SetReadDeadline(time.Now().Add(pongWait))
		// 分型解帧（specs/014 D5）：此前把 spec 直接解成 taskSpec，confirm 帧的
		// decision 字段不是 taskSpec 成员，解帧即丢——allow/deny 永远到不了裁决文件。
		var frame struct {
			Type string          `json:"type"`
			Spec json.RawMessage `json:"spec"`
		}
		if err := r.conn.ReadJSON(&frame); err != nil {
			return
		}
		switch frame.Type {
		case "task_spec":
			var spec taskSpec
			if err := json.Unmarshal(frame.Spec, &spec); err == nil {
				go r.runTask(spec)
			}
		case "round_spec":
			// 通用执行能力：一轮 ops（服务端每次只下发一轮，脚本固定模板）
			var spec taskSpec
			if err := json.Unmarshal(frame.Spec, &spec); err == nil {
				if spec.Mode == "" {
					spec.Mode = "ops"
				}
				go r.runTask(spec)
			}
		case "finish_spec":
			var fs finishSpec
			if err := json.Unmarshal(frame.Spec, &fs); err == nil {
				go r.runFinish(fs)
			}
		case "ping":
			r.send(eventLine{Type: "pong"})
		case "abort":
			var a struct {
				ExecutionID string `json:"execution_id"`
			}
			_ = json.Unmarshal(frame.Spec, &a)
			r.currentMu.Lock()
			if r.current != nil && execKeyMatches(r.current.spec.ExecutionID, a.ExecutionID) {
				r.current.cancel()
			}
			r.currentMu.Unlock()
		case "confirm":
			var cf struct {
				ExecutionID string `json:"execution_id"`
				Decision    string `json:"decision"`
			}
			_ = json.Unmarshal(frame.Spec, &cf)
			r.handleConfirmFrame(cf.ExecutionID, cf.Decision)
		case "upgrade":
			// ego-browser 升级指令（specs/015 C10）：能力升级归 ego 官方，runner 只执行
			// 官方 upgrade 子命令并回传结果事件。异步执行不阻塞读循环。
			go r.runUpgrade()
		}
		if once && frame.Type == "task_spec" {
			// PoC 模式：跑完一个任务即退出（便于脚本化验收）
			time.Sleep(2 * time.Second)
			return
		}
	}
}

// execKeyMatches 服务端下行键（EgoExecKey，ego-<row>）与 dispatch 键（ego-<row>-<ms>）的匹配。
// 此前精确相等必假——abort/confirm 帧在 runner 侧被静默丢弃，进程照跑（specs/014 R2-1）。
// 前缀必须含 "-" 分隔：ego-91 不得命中 ego-9-123。
func execKeyMatches(dispatched, frameID string) bool {
	if dispatched == "" || frameID == "" {
		return false
	}
	return dispatched == frameID || strings.HasPrefix(dispatched, frameID+"-")
}

// handleConfirmFrame 服务端裁决落地（specs/014 D2/D3）：
//
//	allow → 写裁决文件，脚本轮询到后自行放行（事前阻塞的放行通道）；
//	deny  → 写裁决文件（让脚本有机会打出 ok:false 审计行）+ cancel（fail-close，既有语义）。
//
// 其余 decision 值不动作（fail-close：不认识的裁决不能当放行用）。
func (r *runner) handleConfirmFrame(execID, decision string) {
	r.currentMu.Lock()
	defer r.currentMu.Unlock()
	if r.current == nil || !execKeyMatches(r.current.spec.ExecutionID, execID) {
		return
	}
	switch decision {
	case "allow":
		if r.current.decisionPath != "" {
			_ = os.WriteFile(r.current.decisionPath, []byte(`{"decision":"allow"}`), 0o600)
		}
	case "deny":
		if r.current.decisionPath != "" {
			_ = os.WriteFile(r.current.decisionPath, []byte(`{"decision":"deny"}`), 0o600)
		}
		r.current.cancel()
	}
}

// verifySpecConsistency 服务端批准快照 ↔ 本地 manifest 交叉核对（specs/017①）。
// spec.script_hash / asset_version / entry 任一非空即比对本轮下发值与安装时记录值；
// 不一致 ⇒ 本地工作区滞留的不是服务端当前批准的版本，拒绝组装（fail-close）。
// 三项都为空（旧服务端/重放旧 spec）时跳过——不因缺字段把在途旧执行整体打死。
func verifySpecConsistency(manifestRaw []byte, spec taskSpec) error {
	if spec.ScriptHash == "" && spec.AssetVersion == "" && spec.Entry == "" {
		return nil
	}
	var m struct {
		Hivemtk struct {
			Entry      string `json:"entry"`
			AssetVer   string `json:"asset_ver"`
			ScriptHash string `json:"script_hash"`
		} `json:"hivemtk"`
	}
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		return fmt.Errorf("manifest 解析失败: %w", err)
	}
	if spec.ScriptHash != "" && m.Hivemtk.ScriptHash != spec.ScriptHash {
		return fmt.Errorf("批准 hash 不一致：服务端=%s 本地 manifest=%s（工作区资产已过期，请重新拉取资产）",
			spec.ScriptHash, m.Hivemtk.ScriptHash)
	}
	if spec.AssetVersion != "" && m.Hivemtk.AssetVer != spec.AssetVersion {
		return fmt.Errorf("资产版本不一致：服务端=v%s 本地 manifest=v%s（工作区资产已过期，请重新拉取资产）",
			spec.AssetVersion, m.Hivemtk.AssetVer)
	}
	if spec.Entry != "" && m.Hivemtk.Entry != spec.Entry {
		return fmt.Errorf("入口不一致：服务端=%s 本地 manifest=%s", spec.Entry, m.Hivemtk.Entry)
	}
	return nil
}

// verifyAssetAt 资产执行前的两级校验（顺序即语义）：
//
//	① 完整性（永远查）：entry 文件字节的 sha256 必须等于 manifest 里安装时记下的
//	  script_hash —— 拦"装完之后本机被改"；
//	② 来源认证（manifest 带 signature 才查）：ed25519 验签，只服务跨实例官方包通道。
//
// 为什么不再无条件验签：本实例发布的资产全程没有第三方签名环节（发布即信任 +
// 静态扫描门 + 下发时服务端再过一次裁决），无条件验签会让"资产中心点运行"在正常
// 部署下必然失败——把一条腿做成死的并不是安全。
func verifyAssetAt(assetDir string, raw []byte, script []byte) error {
	var m struct {
		Hivemtk struct {
			ScriptHash string `json:"script_hash"`
			SigAlg     string `json:"sig_alg"`
			Signature  string `json:"signature"`
		} `json:"hivemtk"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("manifest 解析失败: %w", err)
	}
	if m.Hivemtk.ScriptHash == "" {
		return errors.New("manifest 缺 script_hash——无法做完整性校验（fail-close）")
	}
	sum := sha256.Sum256(script)
	if got := hex.EncodeToString(sum[:]); got != m.Hivemtk.ScriptHash {
		return fmt.Errorf("脚本 hash 与 manifest 不一致（本机文件可能被改动）：manifest=%s 实得=%s",
			m.Hivemtk.ScriptHash, got)
	}
	if m.Hivemtk.Signature == "" {
		// 未签名（本实例发布的用户流程/脚本包）：完整性已过，来源信任由服务端裁决
		return nil
	}
	return verifyAssetManifestAt(assetDir, raw)
}

// verifyAssetManifestAt runner 侧 manifest 验签（只在上面的 ② 分支被调用：
// manifest 明确带签名时才要求信任公钥，此时缺公钥即 fail-close）。
// 与 server 端同一验签算法——算法双端同源靠同一份规范
// （SHA-256 over raw-bytes-with-blank-signature）+ 本地重复实现最小化
// （runner 不 import server internal 包，规范漂移由 asset_sign_test 的向量测试钉死）。
func verifyAssetManifestAt(assetDir string, raw []byte) error {
	trustPaths := []string{
		filepath.Join(assetDir, "trust.pem"),
		filepath.Join(mustHome(), ".hivemtk", "ego_trust.pem"),
	}
	var pubKeys []ed25519.PublicKey
	for _, tp := range trustPaths {
		b, err := os.ReadFile(tp)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if k, err := base64.StdEncoding.DecodeString(line); err == nil && len(k) == ed25519.PublicKeySize {
				var pk ed25519.PublicKey = ed25519.PublicKey(k)
				pubKeys = append(pubKeys, pk)
			}
		}
		if len(pubKeys) > 0 {
			break
		}
	}
	if len(pubKeys) == 0 {
		return errors.New("无信任公钥（trust.pem 缺失或为空）——fail-close")
	}
	var lastErr error
	for _, pk := range pubKeys {
		if err := verifyAssetSignatureLocal(raw, pk); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// verifyAssetSignatureLocal 与 internal/egolite.VerifyAssetSignature 同规范的最小实现
// （SHA-256 over raw bytes with signature field blanked → ed25519.Verify）
func verifyAssetSignatureLocal(raw []byte, pub ed25519.PublicKey) error {
	var m struct {
		Hivemtk struct {
			SigAlg    string `json:"sig_alg"`
			Signature string `json:"signature"`
		} `json:"hivemtk"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("manifest 解析失败: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(m.Hivemtk.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("signature 缺失或长度非法")
	}
	canonical, err := blankSignatureLocal(raw, m.Hivemtk.Signature)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if !ed25519.Verify(pub, sum[:], sig) {
		return errors.New("签名验证失败")
	}
	return nil
}

func blankSignatureLocal(raw []byte, sigValue string) ([]byte, error) {
	if sigValue == "" {
		return raw, nil
	}
	needle := []byte("\"" + sigValue + "\"")
	repl := []byte("\"\"")
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if i+len(needle) <= len(raw) && string(raw[i:i+len(needle)]) == string(needle) {
			out = append(out, repl...)
			i += len(needle)
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return out, nil
}

// ---------- 运行时收敛（specs/015 C2）与升级指令（C10） ----------

// runnerEntryBlockers 执行前二次静扫（服务端扫描门 EGO-R001..R014 的 runner 侧镜像）。
// 扫描门管"发布前"，这里管"执行前"——拦工作区被篡改/历史脏数据漏网的已知恶意模式。
// 与服务端同一份语义；runner 不 import server internal 包（纪律），样例集由测试钉死。
var runnerEntryBlockers = []struct {
	name string
	rx   *regexp.Regexp
}{
	{"EGO-R001 require(", regexp.MustCompile(`(?m)\brequire\s*\(`)},
	{"EGO-R002 node: 模块", regexp.MustCompile(`(?m)(?:import\s+[^;]*from\s*|import\s*\(?|require\s*\(\s*)["']node:`)},
	{"EGO-R003 child_process", regexp.MustCompile(`(?i)child[_\s]*process|spawnSync|execSync|fork\s*\(`)},
	{"EGO-R004 import() 动态导入", regexp.MustCompile(`(?m)\bimport\s*\(\s*[^)]`)},
	{"EGO-R010 eval(", regexp.MustCompile(`(?:^|[^$\w])eval\s*\(`)},
	{"EGO-R011 Function 构造器", regexp.MustCompile(`(?m)new\s+Function\s*\(|Function\s*\(\s*["']`)},
	{"EGO-R013 process 进程面", regexp.MustCompile(`(?m)\bprocess\s*\.\s*(?:binding|dlopen|exit|kill|env\s*\[)`)},
}

// firstEntryBlocker 返回首个命中的规则名（无命中返回空串）。
// 注意：只扫 entry 原文——preamble 是 runner 自产的受信代码（confirm 契约用
// import("node:fs/promises")，不适用本门）。
func firstEntryBlocker(entrySrc string) string {
	for _, b := range runnerEntryBlockers {
		if b.rx.MatchString(entrySrc) {
			return b.name
		}
	}
	return ""
}

// domainAllowed 域名白名单判定（Go 侧；与注入 JS 的 __domainAllowed__ 同语义）：
// 整站 * 放行；精确域或子域命中；www. 前缀归一；空域单 = 全拒（fail-close）。
func domainAllowed(rawURL string, domains []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range domains {
		dom := strings.ToLower(strings.TrimSpace(d))
		dom = strings.TrimPrefix(dom, "www.")
		if dom == "" {
			continue
		}
		if dom == "*" || host == dom || strings.HasSuffix(host, "."+dom) {
			return true
		}
	}
	return false
}

// auditEventURL 运行时域审计：事件 payload 带 url 字段时校验白名单。
// 零域单不审计（draft/market 的 domains 服务端必填，缺省视为未启用该层，避免误杀
// 老资产——口径与「保存时 domains 必填」互补而非重复）。
func auditEventURL(domains []string, raw json.RawMessage) (string, bool) {
	if len(domains) == 0 {
		return "", true
	}
	var probe struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.URL == "" {
		return "", true
	}
	if domainAllowed(probe.URL, domains) {
		return "", true
	}
	return probe.URL, false
}

// runUpgrade 执行官方 `ego-browser upgrade`（specs/015 C10）。ego lite 的升级策略
// 归官方（不 fork、不代管版本），runner 只做"服务端触发 → 官方命令 → 结果回传"。
func (r *runner) runUpgrade() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, r.conf.EgoBin, egoUpgradeArgs(r.conf)...).CombinedOutput()
	payload := map[string]any{"action": "ego-browser upgrade", "ok": err == nil}
	if err != nil {
		payload["error"] = err.Error()
	}
	payload["output"] = truncate(string(out), 1500)
	// runner 自更新（specs/016 C10 余量）：仅在显式配置 repo_dir 时启用——
	// 官方 ego-browser 升级之后重建 runner 二进制（重启生效，运行中进程不被替换）
	if r.conf.RepoDir != "" {
		so, se := selfUpdate(r.conf.RepoDir)
		payload["self_update"] = map[string]any{
			"ok": se == nil, "output": truncate(so, 800), "note": "重启 ego-runner 后生效",
		}
		if se != nil {
			payload["self_update"].(map[string]any)["error"] = se.Error()
		}
	}
	r.send(eventLine{Type: "runner", Payload: mustJSON(payload)})
}

// selfUpdate 拉源码 + 重建当前二进制（前提：repo_dir 指向 user-server 源码目录，
// 当前进程由 go build/install.sh 产物运行）。任一步失败都如实回传，不做半更新回滚——
// 旧二进制仍在磁盘上跑着，下次升级重试即可。
func selfUpdate(repoDir string) (string, error) {
	var buf strings.Builder
	pull := exec.Command("git", "-C", repoDir, "pull", "--ff-only")
	pullOut, err := pull.CombinedOutput()
	buf.Write(pullOut)
	if err != nil {
		return buf.String(), fmt.Errorf("git pull 失败（--ff-only 防止覆盖本机改动）: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return buf.String(), fmt.Errorf("定位当前二进制失败: %w", err)
	}
	build := exec.Command("go", "build", "-o", exe, "./cmd/ego-runner")
	build.Dir = repoDir
	buildOut, err := build.CombinedOutput()
	buf.Write(buildOut)
	if err != nil {
		return buf.String(), fmt.Errorf("go build 失败: %w", err)
	}
	return buf.String(), nil
}

// egoUpgradeArgs upgrade 子命令的 argv（--ego-server-name 同样前置）
func egoUpgradeArgs(c *runnerConf) []string {
	args := make([]string, 0, 2)
	if c.EgoServerName != "" {
		args = append(args, "--ego-server-name="+c.EgoServerName)
	}
	return append(args, "upgrade")
}
