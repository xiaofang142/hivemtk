// Package platform — 浏览器自动化多平台基座 L3 平台适配层。
//
// 设计来源：user-web/docs/platform-base/PLATFORM_BASE_DESIGN.md（八原则 P1-P8）。
// 先例：Postiz social.abstract.ts（三段式验证协议，契约默认失败）+
//
//	Mixpost SocialProviderManager（注册表）+ Apify SessionPool（三态机）。
//
// 新平台接入 = platform/<name>/ 目录四件套（Locators/Recipe/Verifier/Signature）
// + init() 注册，基座/Brain/UI 零改动（P5）。
package platform

import (
	"context"
	"fmt"
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

// ErrType 错误四分类协议（Postiz handleErrors 语义，写进 RPC 协议）
type ErrType string

const (
	ErrRefreshToken ErrType = "refresh_token" // 登录态过期，可刷新
	ErrBadBody      ErrType = "bad_body"      // 参数/内容不合规，不重试
	ErrRetry        ErrType = "retry"         // 瞬态失败（网络/元素未就绪），可重试
	ErrDisconnect   ErrType = "disconnect"    // 账号被平台拒绝，必须停止并人工介入
)

// Platform 平台适配器接口（每个平台一个实现，四件套收口在此）
type Platform interface {
	// Identifier 平台标识（xiaohongshu / douyin / xianyu）
	Identifier() string
	// Capabilities 平台能力声明
	Capabilities() []Capability
	// MaxConcurrentJobs 单账号并发上限（Postiz 默认 1——写操作串行是风控要求）
	MaxConcurrentJobs() int

	// Locators 元素定位表（选择器+语义描述；平台 DOM 知识只存这里）
	Locators() map[string]string
	// DetectBlock 拦截页识别（平台私有判据：跳转 error 页/验证码/风控文案）
	// 返回 true 表示当前页面是平台拦截页
	DetectBlock(pageSnapshot string) bool
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
	CommentItemText  string `json:"comment_item_text"` // 单条评论文本节点（可选）
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
	return cp.CommentLocators(), nil
}
