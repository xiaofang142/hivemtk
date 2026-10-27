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
	"crypto/rand"
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
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ---------- 配置（~/.hivemtk/ego_runner.conf，key=value 行，同 nm_host.conf 形状） ----------

type runnerConf struct {
	ServerURL    string // ws(s)://host:8204
	Token        string // eg_<userID>_<rand>
	WorkspaceDir string // learnings workspace 根（EGO_BROWSER_AGENT_WORKSPACE 指向）
	DeviceID     string // 首次安装生成，持久化
	EgoBin       string // ego-browser 可执行文件路径
	LoopbackOnly bool   // 形态甲标记（调研 §6.5；runner 侧仅记录与自检提示）
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
		ServerURL:    "ws://127.0.0.1:8204",
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
	ConfirmPolicy string          `json:"confirm_policy"` // none / require（M2 全量实现，本骨架透传）
	ConfirmTTL    int             `json:"confirm_ttl_sec"`
	TimeoutSec    int             `json:"timeout_sec"`
	Round         int             `json:"round"` // resume 时 >1
	ResumeSpaceID string          `json:"resume_space_id,omitempty"`
	Origin        string          `json:"origin"` // market / draft
	DraftScript   string          `json:"draft_script,omitempty"`
}

// ---------- 事件行（A3 协议；runner 只透传+补 seq，不自产结论） ----------

type eventLine struct {
	Seq      int             `json:"seq"`
	Type     string          `json:"type"` // action/snapshot/artifact/handoff/extract/confirm/done/error/runner
	Asset    string          `json:"asset,omitempty"`
	Execution string         `json:"execution_id,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Stderr   string          `json:"stderr,omitempty"` // runner 级错误（非资产自述）
	ExitCode int             `json:"exit_code,omitempty"`
}

// ---------- WS 客户端（照抄 host_registry client 侧纪律：outbound、退避、心跳） ----------

const (
	pingInterval    = 30 * time.Second
	pongWait        = 90 * time.Second // 服务端读超时参数沿用（调研 §6.5）
	dialTimeout     = 10 * time.Second
	reconnectBase   = 2 * time.Second
	reconnectCap    = 60 * time.Second
	execHardTimeout = 30 * time.Minute // R6：单次执行硬超时（wall-clock 看门狗）
)

type runner struct {
	conf      *runnerConf
	conn      *websocket.Conn
	writeMu   sync.Mutex
	current   *execContext // 同一时刻至多一个执行（服务端并发闸是真正限制，这里防本地踩踏）
	currentMu sync.Mutex
}

type execContext struct {
	spec     taskSpec
	cancel   context.CancelFunc
	exitCode int
}

func (r *runner) send(ev eventLine) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.conn == nil {
		return
	}
	_ = r.conn.WriteJSON(ev)
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

	code, errOut := r.execEgo(ctx, spec, script)
	r.send(eventLine{Type: "done", Execution: spec.ExecutionID, ExitCode: code, Stderr: truncate(errOut, 2000)})
}

// assembleScript 组装 round 脚本。骨架版规则：
//   - origin=draft：写 conf 的 DraftScript 到临时文件（正式扫描门/模板包装在 T13 落地；
//     本骨架先按调研 §6.1④ 限制：仅本 user 的 draft 会到达这里——服务端职责）
//   - origin=market：从 workspace 读资产 entry 文件
// 脚本模板（R4 全量）M2 落地；骨架版透传 entry 内容并以最小模板包装。
func (r *runner) assembleScript(spec taskSpec) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "ego-exec-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	var entrySrc string
	switch spec.Origin {
	case "draft":
		entrySrc = spec.DraftScript
	case "market":
		assetDir := filepath.Join(r.conf.WorkspaceDir, "learnings", spec.AssetID)
		entryPath := filepath.Join(assetDir, spec.Entry)
		// 决策 8：market 资产执行前验签（fail-close）。公钥集合由安装器落盘
		//（trust.pem，每行一个 base64 公钥——SyncPull 安装时写入；本文件只验）。
		manifestRaw, err := os.ReadFile(filepath.Join(assetDir, "manifest.json"))
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("读 manifest 失败（资产未安装？workspace=%s）: %w", assetDir, err)
		}
		if err := verifyAssetManifestAt(assetDir, manifestRaw); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("资产验签失败，拒绝执行: %w", err)
		}
		b, err := os.ReadFile(entryPath)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("读资产 entry 失败（workspace=%s）: %w", assetDir, err)
		}
		entrySrc = string(b)
	default:
		cleanup()
		return "", nil, fmt.Errorf("未知 origin=%q", spec.Origin)
	}

	// 最小包装：注入 execution_id，供脚本内 E() 打点关联（R4 全量在 M2/T13）
	script := fmt.Sprintf(`// ego-runner assembled @%s execution=%s origin=%s
const __EXEC__ = %q;
%s
`, time.Now().UTC().Format(time.RFC3339), spec.ExecutionID, spec.Origin, spec.ExecutionID, entrySrc)

	p := filepath.Join(dir, "flow-round01.mjs")
	if err := os.WriteFile(p, []byte(script), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return p, cleanup, nil
}

// execEgo 执行 ego-browser nodejs（stdin 喂脚本——T2 实测：CLI 不接受文件参数，只认
// `ego-browser nodejs < script` 或 `-e`）；stdout 逐行扫描透传，stderr 聚合。
// T2 实测勘误（F6 降级）：EGO_BROWSER_AGENT_WORKSPACE 环境变量**不穿透**到内嵌 Node
// （探针 pid 稳定复现 unset）——资产分发改为 SyncPull 直接解包到物理目录
// ~/.local/share/ego/ego-skills/learnings/{site}/，脚本内以物理路径访问（实测可读）。
func (r *runner) execEgo(ctx context.Context, spec taskSpec, scriptPath string) (int, string) {
	cmd := exec.CommandContext(ctx, r.conf.EgoBin, "nodejs")
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
	scanEvents := func(sc *bufio.Scanner) {
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			var raw json.RawMessage
			if json.Unmarshal([]byte(line), &raw) != nil {
				continue
			}
			seqMu.Lock()
			seq++
			n := seq
			seqMu.Unlock()
			r.send(eventLine{Seq: n, Type: "action", Execution: spec.ExecutionID, Payload: raw})
		}
	}
	errBuf := &strings.Builder{}
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
					seqMu.Lock()
					seq++
					n := seq
					seqMu.Unlock()
					r.send(eventLine{Seq: n, Type: "action", Execution: spec.ExecutionID, Payload: raw})
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
		scanEvents(sc)
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
		"type":       "register",
		"version":    "0.1.0",
		"device_id":  r.conf.DeviceID,
		"hostname":   mustHostname(),
		"ego_version": egoVer,
		"workspace":  r.conf.WorkspaceDir,
	})
	return nil
}

func probeEgoVersion(egoBin string) string {
	out, err := exec.Command(egoBin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
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
		var frame struct {
			Type string    `json:"type"`
			Spec taskSpec  `json:"spec"`
		}
		if err := r.conn.ReadJSON(&frame); err != nil {
			return
		}
		switch frame.Type {
		case "task_spec":
			go r.runTask(frame.Spec)
		case "ping":
			r.send(eventLine{Type: "pong"})
		case "abort":
			r.currentMu.Lock()
			if r.current != nil && r.current.spec.ExecutionID == frame.Spec.ExecutionID {
				r.current.cancel()
			}
			r.currentMu.Unlock()
		case "confirm":
			// 服务端裁决（allow/deny）转发给在途执行：deny=取消执行进程
			//（fail-close：拒绝即终止本轮）；allow=继续（脚本在 confirm 事件行后自行推进）。
			r.currentMu.Lock()
			if r.current != nil && r.current.spec.ExecutionID == frame.Spec.ExecutionID {
				var d struct {
					Decision string `json:"decision"`
				}
				_ = json.Unmarshal(frame.Spec.Params, &d)
				if d.Decision == "deny" {
					r.current.cancel() // fail-close：拒绝即终止本轮
				}
			}
			r.currentMu.Unlock()
		}
		if once && frame.Type == "task_spec" {
			// PoC 模式：跑完一个任务即退出（便于脚本化验收）
			time.Sleep(2 * time.Second)
			return
		}
	}
}

// verifyAssetManifestAt runner 侧 manifest 验签（决策 8：与 server 端同一验签算法——
// 算法双端同源靠同一份规范（SHA-256 over raw-bytes-with-blank-signature）+ 本地重复实现
// 最小化（runner 不 import server internal 包，规范漂移由 asset_sign_test 的向量测试钉死）。
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
//（SHA-256 over raw bytes with signature field blanked → ed25519.Verify）
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

// 编译期引用检查（rand/hex 在 token 自检时用；避免未使用报错的正式位置在 M2 轮换逻辑）
var _ = func() error {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	_ = hex.EncodeToString(b)
	_ = runtime.GOOS
	return err
}
