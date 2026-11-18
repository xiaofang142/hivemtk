package service

import (
	"fmt"
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
	Platform string // 浏览器自报平台（navigator.platform / userAgentData.platform）
	Screen   string // 屏幕分辨率，如 1920x1080
	Timezone string // IANA 时区
	Language string // 首选语言
	UA       string // User-Agent（controller 从请求头取，浏览器不自报）
	IP       string // 出口 IP（controller 经 ctx.ClientIP() 采集）
}

// Report 记录最近一帧商户电脑设备快照。
// 返回 recorded=false 表示当前形态不收快照（离线单机部署），属正常静默口径。
func (s *ClientDeviceService) Report(rep *ClientDeviceReport) (recorded bool, err error) {
	if rep == nil {
		return false, fmt.Errorf("设备信标内容为空")
	}
	if !tenant.IsMultiTenant() {
		// 离线单机部署：心跳设备口径就是部署机（host_info），不收浏览器快照。
		return false, nil
	}
	platform.SetClientDeviceSnapshot(&platform.ClientDeviceSnapshot{
		IP:         strings.TrimSpace(rep.IP),
		UA:         strings.TrimSpace(rep.UA),
		Platform:   strings.TrimSpace(rep.Platform),
		Screen:     strings.TrimSpace(rep.Screen),
		Timezone:   strings.TrimSpace(rep.Timezone),
		Language:   strings.TrimSpace(rep.Language),
		CapturedAt: time.Now(),
	})
	return true, nil
}
