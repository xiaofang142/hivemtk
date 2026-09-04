//go:build unix

package main

import (
	"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"
)

// serveHTTP Unix 平台实现：endless 支持 SIGUSR2 零停机热重启与优雅关闭。
// Windows 不可用（依赖 SIGUSR1/2/SIGTSTP），对应实现见 serve_windows.go。
func serveHTTP(addr string, r *gin.Engine) error {
	return endless.ListenAndServe(addr, r)
}
