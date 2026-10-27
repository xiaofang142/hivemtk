// Package platform — 浏览器自动化多平台基座 L3 平台适配层。
//
// 设计来源：user-web/docs/platform-base/PLATFORM_BASE_DESIGN.md（八原则 P1-P8）。
// 先例：Postiz social.abstract.ts（三段式验证协议，契约默认失败）+
//
//	Mixpost SocialProviderManager（注册表）+ Apify SessionPool（三态机）。
//
// 新平台接入 = platform/<name>/ 目录四件套（Locators/Recipe/Verifier/Signature）
// + init() 注册，基座/Brain/UI 零改动。
package platform

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Capability 平台能力（可选，基座探测而非强求全实现——Postiz optional 方法语义）
type Capability string

const (
	CapSearch      Capability = "search"       // 关键词搜索
	CapOpenDetail  Capability = "open_detail"  // 打开详情页（含 token 透传）
	CapPostComment Capability = "post_comment" // 发评论
	CapReadComment Capability = "read_comment" // 读评论
	CapPost        Capability = "post"         // 发帖/发布作品
)

// AllCapabilities 全部能力值域——**唯一真源**。
//
// 存在的理由不是方便遍历，而是让「新增一个 Capability 却忘了接执行面」这件事编译得过、
// 但测试过不去：capabilityCoverage 断言每个值要么被 RequiredCapability 消费（执行面拦），
// 要么在 AdvisoryCapabilities 里写明为什么只服务 Brain。没有这一层，新增能力默认是
// 「声明了、没人读」——正是 C21 要修的那个洞，只是换个能力名再犯一次。
func AllCapabilities() []Capability {
	return []Capability{CapSearch, CapOpenDetail, CapPostComment, CapReadComment, CapPost}
}

// ErrType 错误四分类协议（Postiz handleErrors 语义，写进 RPC 协议）
type ErrType string

const (
	ErrRefreshToken ErrType = "refresh_token" // 登录态过期，可刷新
	ErrBadBody      ErrType = "bad_body"      // 参数/内容不合规，不重试
	ErrRetry        ErrType = "retry"         // 瞬态失败（网络/元素未就绪），可重试
	ErrDisconnect   ErrType = "disconnect"    // 账号被平台拒绝，必须停止并人工介入
	// ErrUnknown 判据未命中——A3 fail-closed：契约默认失败，不盲目重试。
	// 依据：AWS SDK Go v2 retry 三值判定「全 Unknown 即不重试」；盲目重试把瞬态抖动
	// 放大成风控信号，且掩盖归因缺口——未分类错误应由上层打点、显式扩表，而非默认放行。
	ErrUnknown ErrType = "unknown"
)

// Platform 平台适配器接口（每个平台一个实现，四件套收口在此）
type Platform interface {
	// Identifier 平台标识（xiaohongshu / douyin / xianyu）
	Identifier() string
	// Capabilities 平台能力声明
	Capabilities() []Capability
	// MaxConcurrentJobs 单账号并发上限（Postiz 默认 1——写操作串行是风控要求）
	MaxConcurrentJobs() int

	// Locators 元素定位表（选择器+语义描述；平台 DOM 知识只存这里；
	// A1：拦截文案不混入定位表，见 BlockMarkers）
	Locators() map[string]string
	// DetectBlock 拦截页识别（A2 结构化判据：URL 层 + 快照结构层，不扫正文全文）。
	// pageURL 来自 snapshot 原语回包（起扩展快照携带 location.href）。
	DetectBlock(pageURL, pageSnapshot string) bool
	// BlockMarkers 风控文案判据（只与快照结构行匹配；同时供 Brain 平台知识 prompt）
	BlockMarkers() []string
	// BlockURLPatterns 拦截 URL 重定向/路径判据（实测形态见各适配器注释出处）
	BlockURLPatterns() []string
	// ClassifyError 平台错误归因（refresh-token/bad-body/retry/disconnect）
	ClassifyError(errText string) ErrType
}

// CommentPoster 发评论能力（可选实现；每个写操作强制配对 Verify——P4 fails-loudly）
type CommentPoster interface {
	// CommentLocators 发评论所需的平台专属选择器（输入框/发送按钮/评论区容器）
	CommentLocators() CommentLocators
}

// CommentLocators 平台发评论选择器四元组（适配器声明，基座统一下发扩展）
type CommentLocators struct {
	InputSelector    string `json:"input_selector"`    // 评论输入框（CSS）
	SendButtonText   string `json:"send_button_text"`  // 发送按钮文本（发/发布/评论）
	CommentContainer string `json:"comment_container"` // 评论区容器（验证渲染用）
	CommentItemText  string `json:"comment_item_text"` // 单条评论文本节点（**必填**：新旧判据的载体，见 Missing）
}

// Registry 平台注册表（基座唯一入口；新平台 init() 注册）
type Registry struct {
	mu        sync.RWMutex
	platforms map[string]Platform
}

var global = &Registry{platforms: map[string]Platform{}}

// Register 注册平台适配器（各平台包 init() 调用；重复注册 panic 防呆）
func Register(p Platform) {
	global.mu.Lock()
	defer global.mu.Unlock()
	id := p.Identifier()
	if _, dup := global.platforms[id]; dup {
		panic(fmt.Sprintf("platform %s already registered", id))
	}
	global.platforms[id] = p
}

// Deregister 注销平台适配器（返回是否真的存在）。
//
// 用途只有两类：适配器热重载/下线，以及**测试隔离**——注册表是进程级全局状态，
// 一个用例塞进去的假适配器会漏给同包后续用例（尤其 TestCapabilitiesWithinVocabulary
// 那种遍历 List() 的白名单断言），有了对称的注销点才谈得上「用例之间零共享」。
func Deregister(id string) bool {
	global.mu.Lock()
	defer global.mu.Unlock()
	if _, ok := global.platforms[id]; !ok {
		return false
	}
	delete(global.platforms, id)
	return true
}

// Get 取平台适配器；未注册返回错误（而非 nil，防下游空指针）
func Get(id string) (Platform, error) {
	global.mu.RLock()
	defer global.mu.RUnlock()
	p, ok := global.platforms[id]
	if !ok {
		return nil, fmt.Errorf("平台 %s 未注册（可用: %v）", id, global.ids())
	}
	return p, nil
}

// List 列出已注册平台
func List() []Platform {
	global.mu.RLock()
	defer global.mu.RUnlock()
	out := make([]Platform, 0, len(global.platforms))
	for _, p := range global.platforms {
		out = append(out, p)
	}
	return out
}

func (r *Registry) ids() []string {
	ids := make([]string, 0, len(r.platforms))
	for id := range r.platforms {
		ids = append(ids, id)
	}
	return ids
}

// HasCapability 探测平台能力
func HasCapability(p Platform, c Capability) bool {
	for _, pc := range p.Capabilities() {
		if pc == c {
			return true
		}
	}
	return false
}

// RequiredCapability 原语 → 能力映射（基座执行面的唯一判定表，C21）。
//
// 只映射「语义上确实需要某平台能力」的原语：post_comment 需要平台声明 post_comment
// 并实现 CommentPoster。表外的原语都是**通用动作**（open_tab/click/type/query/…），
// 它们与平台无关，任何已注册平台都放行。
//
// 为什么要有这张表：能力声明（Capabilities()）此前只进 Brain prompt 与 /platforms 接口，
// 执行面一处都不读——「声明了能力」与「执行面是否按能力拦」之间没有连接，
// 等于把 P4（未声明能力即报错）做成了纯文档。表 + capabilityCoverage 测试把这条接上：
// 每个能力要么被这张表消费，要么必须在 AdvisoryCapabilities 里写明「只服务 Brain 决策」。
func RequiredCapability(action string) (Capability, bool) {
	c, ok := requiredCapabilityByAction[action]
	return c, ok
}

// requiredCapabilityByAction 上表的**数据形态**（同一份真源，不另抄一遍）。
// 测试靠它反查「哪些能力已被执行面消费」，从而判定剩下的是不是都在免责清单里。
var requiredCapabilityByAction = map[string]Capability{
	"post_comment": CapPostComment,
}

// RequiredCapabilities 已被执行面消费的能力集合（去重，顺序稳定）。
func RequiredCapabilities() []Capability {
	out := make([]Capability, 0, len(requiredCapabilityByAction))
	seen := map[Capability]bool{}
	for _, a := range sortedActions() {
		c := requiredCapabilityByAction[a]
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// sortedActions 动作名升序（map 遍历无序，错误文案与测试断言都要稳定）
func sortedActions() []string {
	out := make([]string, 0, len(requiredCapabilityByAction))
	for a := range requiredCapabilityByAction {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// AdvisoryCapabilities 不被执行面拦截的能力，以及为什么。
//
// 「声明了却不拦」不是遗漏，是边界：这些能力描述的是「平台能做什么」，供 Brain 决策与
// 前端展示；执行面没有对应的强制点——例如 search/open_detail 只是个 URL 导航，
// open_tab 原语对任何平台都放行（拦它需要先定义「open_detail 与 open_tab 的语义差」，
// 那会把通用原语拆出平台分支，与「基座零平台知识」相反）。
// post 同理：发帖没有对应原语。read_comment 也不需要拦：读永远比写安全，
// 拦读只会在页面结构变化时把「读不到」误报成「平台不支持」。
func AdvisoryCapabilities() map[Capability]string {
	return map[Capability]string{
		CapSearch:      "URL 导航语义，open_tab 对所有平台放行；供 Brain 决定先搜索",
		CapOpenDetail:  "同上：详情页就是一个 URL，没有可拦的执行点",
		CapPost:        "发帖没有对应原语（post_comment 才是），声明仅供 Brain/UI",
		CapReadComment: "读永远比写安全：拦读会把「结构变了读不到」误报成「平台不支持」",
	}
}

// CommentLocatorsFor 取平台发评论选择器（契约默认失败：声明了 post_comment 能力
// 却不实现 CommentPoster → 首次调用即报错，防静默假成功）
func CommentLocatorsFor(ctx context.Context, id string) (CommentLocators, error) {
	p, err := Get(id)
	if err != nil {
		return CommentLocators{}, err
	}
	if !HasCapability(p, CapPostComment) {
		return CommentLocators{}, fmt.Errorf("平台 %s 未声明 post_comment 能力", id)
	}
	cp, ok := p.(CommentPoster)
	if !ok {
		return CommentLocators{}, fmt.Errorf("平台 %s 声明了 post_comment 但未实现 CommentPoster（fails-loudly）", id)
	}
	locs := cp.CommentLocators()
	// E21：四元组必须全声明，尤其是 comment_item_text。少了它不是「少一个可选字段」，
	// 而是回查判据退化成「页面上有没有这句话」——同账号重复文本评论会被判成
	// 「这次发布成功」（session644 真机假绿）。缺哪个就报哪个，别让执行面自己找补。
	if missing := locs.Missing(); len(missing) > 0 {
		return CommentLocators{}, fmt.Errorf("平台 %s 声明了 post_comment 但选择器未声明全：%s（缺 comment_item_text 会让新旧判据无法建立）",
			id, strings.Join(missing, ", "))
	}
	return locs, nil
}

// Missing 未声明的选择器字段名（顺序固定，便于错误文案稳定）
func (l CommentLocators) Missing() []string {
	var out []string
	if strings.TrimSpace(l.InputSelector) == "" {
		out = append(out, "input_selector")
	}
	if strings.TrimSpace(l.SendButtonText) == "" {
		out = append(out, "send_button_text")
	}
	if strings.TrimSpace(l.CommentContainer) == "" {
		out = append(out, "comment_container")
	}
	if strings.TrimSpace(l.CommentItemText) == "" {
		out = append(out, "comment_item_text")
	}
	return out
}
