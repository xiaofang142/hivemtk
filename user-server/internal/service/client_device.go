package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/platform"
)

// ClientDeviceService 商户电脑设备快照上报服务（任务三）。
//
// SaaS 形态下心跳的"设备"必须回答"商户此刻在用哪台电脑"，而不是云端服务器。
// user-web 登录后发一帧设备信标到 /api/client-device/report，本服务把
// 浏览器自报信息（屏幕/时区/语言）与请求侧采集信息（出口 IP、User-Agent）
// 合并成快照交给 platform 包缓存；心跳协程取最近一帧随帧上报平台。
//
// 本地部署形态（TENANT_MODE=off）不收快照：user-server 机即部署机，
// 心跳的 host_info 已经是商户机器信息，两套口径不并存。
type ClientDeviceService struct{}

// NewClientDeviceService 构造
func NewClientDeviceService() *ClientDeviceService { return &ClientDeviceService{} }

// ClientDeviceReport 一帧设备信标（controller 采集请求侧字段后组装）
type ClientDeviceReport struct {
	Platform   string // 浏览器自报平台（navigator.platform / userAgentData.platform）
	Screen     string // 屏幕分辨率，如 1920x1080
	Timezone   string // IANA 时区
	Language   string // 首选语言
	UA         string // User-Agent（controller 从请求头取，浏览器不自报）
	IP         string // 出口 IP（controller 经 ctx.ClientIP() 采集）
	TenantCode string `json:"tenant_code"`
}

// Report 记录最近一帧商户电脑设备快照。
// 返回 recorded=false 表示当前形态不收快照（离线单机部署），属正常静默口径。
// clientDeviceTenantWhitelist 允许上报设备信标的租户码白名单
// （env CLIENT_DEVICE_TENANT_CODES，逗号分隔）。TA-06 归属口径（2026-11-19）：
// 快照是进程级单份、随「部署商户」的 merchant_key 心跳上报——SaaS 共享实例上
// 不做白名单时，任意租户管理员的浏览器信标会覆盖别人的设备快照
// （共享实例最后一帧覆盖此前其他租户的数据）。空白名单 = 拒收全部
// （fail-closed：部署方没配归属口径就不收，宁缺毋错）。
func clientDeviceTenantWhitelist() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("CLIENT_DEVICE_TENANT_CODES"))
	if raw == "" {
		return nil
	}
	set := map[string]bool{}
	for _, code := range strings.Split(raw, ",") {
		if code = strings.TrimSpace(code); code != "" {
			set[code] = true
		}
	}
	return set
}

func (s *ClientDeviceService) Report(ctx context.Context, rep *ClientDeviceReport) (recorded bool, err error) {
	if rep == nil {
		return false, fmt.Errorf("设备信标内容为空")
	}
	if !tenant.IsMultiTenant() {
		// 离线单机部署：心跳设备口径就是部署机（host_info），不收浏览器快照。
		return false, nil
	}
	wl := clientDeviceTenantWhitelist()
	// 归属以**登录租户**（JWT ctx）为准：body 自报 tenant_code 仅作透传展示，
	// 不可信客户端自报归属（AGENTS.md 多租户纪律）。
	code := platform.TenantCodeFromContext(ctx)
	if code == "" || wl == nil || !wl[code] {
		// 未配置白名单或来源租户不在白名单：拒收（不落快照）。
		// 不返回 error——信标是低价值遥测，静默拒收避免管理端噪音。
		return false, nil
	}
	platform.SetClientDeviceSnapshot(&platform.ClientDeviceSnapshot{
		IP:         strings.TrimSpace(rep.IP),
		UA:         strings.TrimSpace(rep.UA),
		Platform:   strings.TrimSpace(rep.Platform),
		Screen:     strings.TrimSpace(rep.Screen),
		Timezone:   strings.TrimSpace(rep.Timezone),
		Language:   strings.TrimSpace(rep.Language),
		TenantCode: code,
		CapturedAt: time.Now(),
	})
	return true, nil
}
