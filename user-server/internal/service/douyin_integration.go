package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/tenant"
)

const (
	dyDMOutreachCooldown = 60 * time.Minute
	dyDMOutreachMinScore = 60
)

// dmOutreachAllowed 私信触达冷却闸：同一 key 在 dyDMOutreachCooldown 内只放行一次。
func dmOutreachAllowed(ctx context.Context, key string) bool {
	return dmOutreachAllowedWithCache(ctx, key, cache.GetGlobalCache())
}

// dmOutreachAllowedWithCache 在原共享缓存上确认许可；失败不换缓存、不释放已占预算。
func dmOutreachAllowedWithCache(ctx context.Context, key string, c cache.Cache) bool {
	if ctx == nil || c == nil || strings.TrimSpace(key) == "" || ctx.Err() != nil {
		return false
	}
	if tenant.IsSystemScope(ctx) {
		return false
	}
	if _, ok := tenant.FromContext(ctx); tenant.IsMultiTenant() && !ok {
		return false
	}
	set, err := c.SetNX(ctx, key, "1", dyDMOutreachCooldown)
	return err == nil && set && ctx.Err() == nil
}

func bridgeDMOutreachIdentityValid(channel, accountID, fromID string) bool {
	if strings.TrimSpace(accountID) == "" || accountID == "default" || strings.TrimSpace(fromID) == "" {
		return false
	}
	switch channel {
	case string(ChannelDouyin), string(ChannelTiktok), "xiaohongshu", "kuaishou", "xianyu":
		return true
	default:
		return false
	}
}

func BuildDouyinDMWelcome(groupTitle, originalText string) string {
	groupLabel := strings.TrimSpace(groupTitle)
	if groupLabel == "" {
		groupLabel = "直播间"
	}
	templates := []string{
		fmt.Sprintf("你好！看到你在「%s」的发言，我是做这方面产品的，想了解一下你的具体需求。方便聊聊吗？", groupLabel),
		fmt.Sprintf("您好！关注到你对「%s」的内容感兴趣，我们提供相关产品/服务，是否方便进一步沟通？", groupLabel),
		fmt.Sprintf("哈喽！在「%s」看到你的留言，我这边正好有相关资源，可以聊聊你的需求吗？", groupLabel),
	}
	idx := 0
	if len(originalText) > 0 {
		for _, r := range originalText {
			if r >= 0x4E00 && r <= 0x9FFF {
				idx = 0
				break
			}
		}
		if idx == 0 {
			hasASCII := false
			for _, r := range originalText {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					hasASCII = true
					break
				}
			}
			if hasASCII {
				idx = 1
			}
		}
	}
	return templates[idx]
}
