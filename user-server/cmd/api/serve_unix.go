//go:build unix

package main

import (
	"time"

	"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"
)

func serveHTTP(addr string, r *gin.Engine) error {
	// F2（批5e 真机实测更正）：本路径的优雅停机一直是 endless 在管（SIGTERM/SIGINT →
	// shutdown() 关监听 + 等在途请求收敛），不是「没有 os/signal」。真机踩到的是它的
	// 另一半语义：被 Hijack 的长连接（浏览器 Host WS、SSE）永不 WaitGroup.Done，
	// 于是旧进程要活满 DefaultHammerTime（库默认 60s）才被强制收掉——期间监听端口已释放，
	// 新实例已可绑定，旧实例的 cron/outbox 轮询还在跑（双 worker 并存窗口）。
	// 压到 15s：普通 API 请求仍有充分收敛时间，双跑窗口从 60s 降到 15s。
	// 运维即时停：`kill -USR2 <pid>` 走 endless.hammerTime(0)，立刻强制退出（不等收敛）。
	// 另注意 `kill -HUP` 不是重启单机进程而是 endless.fork()（会并存第二个 server）。
	endless.DefaultHammerTime = 15 * time.Second

	// 只设 ReadHeaderTimeout/IdleTimeout:不设 ReadTimeout(知识库大文件上传)与
	// WriteTimeout(SSE 长连接会被强制断开);endless.Server 内嵌 http.Server,
	// 热重启能力与超时配置并存
	srv := endless.NewServer(addr, r)
	srv.ReadHeaderTimeout = 10 * time.Second
	srv.IdleTimeout = 120 * time.Second
	// ListenAndServe 会创建 EndlessListener 后调 Serve;
	// 直接调 Serve 时 EndlessListener 为 nil → Accept 空指针 panic
	return srv.ListenAndServe()
}
