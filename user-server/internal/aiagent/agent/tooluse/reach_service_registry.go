package tooluse

import (
	"context"
	"errors"
	"sync"
)

// WechatServiceLike 微信公众号发送。
//
// 这一层的工具与适配器都不能 import service（service 侧已经反向依赖本包，成环即编不过），
// 所以公众号发送由装配点 internal/app.RegisterWeixinSender 注册进来、由适配器读取。
// 其余渠道（短信/邮件/企微/飞书/telegram/whatsapp/钉钉）的发送服务由
// NewIntegrationReachAdapterFromDB 按 db 直接构造，不走这里。
type WechatServiceLike interface {
	SendCustomMessage(ctx context.Context, accountID uint, openID, msgType, content string) (string, error)
}

// ServiceRegistry 全局渠道 service 注册中心
type ServiceRegistry struct {
	mu sync.RWMutex

	wechat WechatServiceLike
}

var globalReachServiceRegistry = &ServiceRegistry{}

// GlobalServiceRegistry 获取全局注册中心
func GlobalServiceRegistry() *ServiceRegistry { return globalReachServiceRegistry }

// RegisterWechat 注册微信公众号 service
func (r *ServiceRegistry) RegisterWechat(s WechatServiceLike) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wechat = s
}

// Wechat 取微信公众号；未注册即报错，不返回可用的零值
func (r *ServiceRegistry) Wechat() (WechatServiceLike, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.wechat == nil {
		return nil, errors.New("wechat service not registered")
	}
	return r.wechat, nil
}
