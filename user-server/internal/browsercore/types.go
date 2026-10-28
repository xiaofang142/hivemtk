// Package browsercore 定义「编排层 ↔ 执行层」的通用浏览器执行契约。
//
// 定位（internal-docs/ego-browser-通用执行能力-技术方案与开发清单-2026-10-28.md）：
//   - HiveMtk 只做编排（任务分解 / 轮次决策 / 策略 / 审计 / 配额）；
//   - ego-browser（或 NM 桥腿）只做执行（动作 / 观测）；
//   - 两侧唯一协议 = 本包的 DTO。包内**不含任何业务依赖**，两条腿各自实现 Executor。
//
// 纪律：本包不 import egolite / browser_automation / model，避免把执行腿绑死；
// 需要持久化的形状由各腿在自己的 model 层映射。
package browsercore

import (
	"context"
	"errors"
	"time"
)

// 引擎标识
const (
	EngineAuto = "auto" // 由 Router 按能力选择
	EngineEgo  = "ego"  // ego lite 腿（ego-browser / TaskSpace）
	EngineNM   = "nm"   // NM 桥腿（Chrome 扩展 + browser_tasks）
)

// 执行模式
const (
	ModeOps    = "ops"    // 通用：受限动作集，逐轮生成（默认）
	ModeScript = "script" // 预置脚本（flow / asset），审核过的完整计划
)

// 会话状态（与 egolite_executions 状态机同构，不另造事实源）
const (
	SessionCreated        = "created"
	SessionActive         = "active"
	SessionWaitingConfirm = "waiting_confirm"
	SessionHandedOff      = "handed_off"
	SessionFinished       = "finished"
	SessionFailed         = "failed"
	SessionStopped        = "stopped"
	SessionTimeout        = "timeout"
)

// TerminalStates 终态集合
var TerminalStates = map[string]bool{
	SessionFinished: true,
	SessionFailed:   true,
	SessionStopped:  true,
	SessionTimeout:  true,
}

// ErrNotFound 会话不存在 / 不属于该用户（两者同响应，防枚举）
var ErrNotFound = errors.New("浏览器会话不存在")

// ErrStateConflict 当前状态不允许该操作
var ErrStateConflict = errors.New("浏览器会话状态不允许该操作")

// ErrEngineUnavailable 指定引擎不可用（未安装 / 离线 / 版本不满足）
var ErrEngineUnavailable = errors.New("浏览器执行引擎不可用")

// ErrPolicyViolation 请求违反策略（域名 / 写动作 / 预算 / 动作数）
var ErrPolicyViolation = errors.New("请求违反浏览器执行策略")

// ErrGate 每用户并发闸
var ErrGate = errors.New("浏览器会话/执行已达并发上限")

// Policy 会话级策略。空 Domains 表示拒绝一切导航（fail-close），"*" 显式放开。
type Policy struct {
	Domains        []string `json:"domains"`
	WriteConfirm   string   `json:"write_confirm"` // none / require
	ConfirmTTLSec  int      `json:"confirm_ttl_sec"`
	TimeoutSec     int      `json:"timeout_sec"` // 单轮硬超时
	MaxRounds      int      `json:"max_rounds"`
	MaxActions     int      `json:"max_actions"` // 单轮动作上限
	RedactPatterns []string `json:"redact_patterns,omitempty"`
}

// OpenRequest 创建会话
type OpenRequest struct {
	UserID      uint   `json:"user_id"`
	Engine      string `json:"engine"` // auto/ego/nm
	Mode        string `json:"mode"`   // ops/script
	Goal        string `json:"goal"`
	SpaceName   string `json:"space_name"` // 幂等键；同名 = 续跑
	CreatedFrom string `json:"created_from"`
	Policy      Policy `json:"policy"`
}

// SessionInfo 会话对外视图（持久化形状由各腿映射）
type SessionInfo struct {
	ID          uint      `json:"id"`
	UserID      uint      `json:"user_id"`
	Goal        string    `json:"goal"`
	Engine      string    `json:"engine"`
	Mode        string    `json:"mode"`
	SpaceName   string    `json:"space_name"`
	SpaceID     string    `json:"space_id,omitempty"`
	ExecutionID uint      `json:"execution_id,omitempty"`
	State       string    `json:"state"`
	Round       int       `json:"round"`
	Policy      Policy    `json:"policy"`
	CreatedFrom string    `json:"created_from,omitempty"`
	FailReason  string    `json:"fail_reason,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Target 可交互/有语义的页面节点。Ref 只对生成它的那次快照有效（ego 官方语义），
// 跨轮必须用 Loc 重新解析。
type Target struct {
	Ref  string `json:"ref"`
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
	Loc  string `json:"loc,omitempty"`
}

// Artifact 产物（截图/导出文件）。Path 是执行本机绝对路径，服务端只记账。
type Artifact struct {
	Kind   string `json:"kind"` // screenshot / extract / upload
	Path   string `json:"path,omitempty"`
	Handle string `json:"handle,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
}

// Observation 单轮观测（已裁剪/脱敏）
type Observation struct {
	URL            string     `json:"url"`
	Title          string     `json:"title"`
	Viewport       Viewport   `json:"viewport"`
	Targets        []Target   `json:"targets"`
	Text           string     `json:"text"`
	TextTruncated  bool       `json:"text_truncated"`
	SnapshotHandle string     `json:"snapshot_handle,omitempty"`
	Extract        any        `json:"extract,omitempty"`
	Artifacts      []Artifact `json:"artifacts,omitempty"`
}

// Viewport 视口尺寸
type Viewport struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Usage 轮次成本（配额/遥测数据源）
type Usage struct {
	MS      int64 `json:"ms"`
	Actions int   `json:"actions"`
}

// ExecError 机器可读错误
type ExecError struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Step int    `json:"step,omitempty"`
}

// RoundRequest 提交一轮
type RoundRequest struct {
	UserID    uint        `json:"user_id"`
	SessionID uint        `json:"session_id"`
	Ops       []Op        `json:"ops"`
	Observe   ObserveSpec `json:"observe"`
}

// ObserveSpec 观测选项
type ObserveSpec struct {
	Snapshot   bool   `json:"snapshot"`
	Scope      string `json:"scope"` // viewport / full_page / subtree
	MaxTargets int    `json:"max_targets"`
	MaxText    int    `json:"max_text"`
	Screenshot string `json:"screenshot"` // "" / on_error / always
}

// RoundInfo 轮次对外视图
type RoundInfo struct {
	Round       int        `json:"round"`
	ExecutionID uint       `json:"execution_id"`
	OK          *bool      `json:"ok"`
	URL         string     `json:"url,omitempty"`
	Title       string     `json:"title,omitempty"`
	Targets     []Target   `json:"targets,omitempty"`
	Text        string     `json:"text,omitempty"`
	Extract     any        `json:"extract,omitempty"`
	Artifacts   []Artifact `json:"artifacts,omitempty"`
	Error       *ExecError `json:"error,omitempty"`
	Usage       Usage      `json:"usage"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ActResult 一轮提交结果（异步：返回已派发的轮次；观测用 Observe 或查询轮次账本）
type ActResult struct {
	Session SessionInfo `json:"session"`
	Round   RoundInfo   `json:"round"`
}

// ObserveRequest 取观测
type ObserveRequest struct {
	UserID    uint `json:"user_id"`
	SessionID uint `json:"session_id"`
	Round     int  `json:"round"` // 0 = 最新一轮
}

// Executor 编排层看到的唯一执行面。实现方负责把 DTO 映射到自己的持久化/传输。
type Executor interface {
	Engine() string
	Open(ctx context.Context, req OpenRequest) (*SessionInfo, error)
	Act(ctx context.Context, req RoundRequest) (*ActResult, error)
	Observe(ctx context.Context, req ObserveRequest) (*RoundInfo, error)
	Confirm(ctx context.Context, userID, sessionID uint, allow bool) (*SessionInfo, error)
	Abort(ctx context.Context, userID, sessionID uint, reason string) (*SessionInfo, error)
	Finish(ctx context.Context, userID, sessionID uint, keep []string) (*SessionInfo, error)
	Get(ctx context.Context, userID, sessionID uint) (*SessionInfo, error)
	List(ctx context.Context, userID uint, page, pageSize int) ([]*SessionInfo, int64, error)
	Rounds(ctx context.Context, userID, sessionID uint, limit int) ([]*RoundInfo, error)
}
