//go:build windows

package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// serveHTTP Windows 平台实现：endless 依赖 Unix 信号（SIGUSR2 零停机重启），
// 在 Windows 上无法编译，退化为标准 http 服务。
// 关闭路径返回 http.ErrServerClosed，由 isGracefulShutdownErr 归一处理，语义与 Unix 一致。
func serveHTTP(addr string, r *gin.Engine) error {
	return http.ListenAndServe(addr, r)
}
