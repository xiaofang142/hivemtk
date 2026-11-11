// minttoken 本地开发辅助工具：铸一枚 user-server admin JWT 打到 stdout。
//
// 消费方：本机联调与 e2e 脚本（scripts/e2e_browser_real.py 经 `go run ./cmd/minttoken`
// 铸本地 admin token，不落盘、不打印）。安全面闭合：密钥链 fail-closed
// （internal/pkg/utils loadJWTSecret——非 test 进程缺/短 USER_JWT_SECRET 直接 panic），
// 工具只能在已持有真实密钥的机器上出票，无独立提权面；生产禁用。
//
// O-2（2026-11-12）：原版本硬编码 uid=26/uit_admin/admin 且零注释；本轮补语义说明
// 并参数化。默认值与原硬编码一致（无参运行行为等价，兼容既有 e2e 调用）。
// 同批：cmd/genjwt 已删除（其注释自述「PoC 验收用，M2 完成后删除」，且全仓零引用）。
package main

import (
	"flag"
	"fmt"

	"hivemtk-user/internal/pkg/utils"
)

func main() {
	userID := flag.Uint("uid", 26, "目标 userID")
	username := flag.String("username", "uit_admin", "用户名")
	role := flag.String("role", "admin", "角色（admin/customer_service/viewer）")
	flag.Parse()

	j := utils.NewJWTUtils(utils.DefaultJWTConfig)
	tok, err := j.GenerateToken(uint(*userID), *username, *role)
	if err != nil {
		panic(err)
	}
	fmt.Println(tok)
}
