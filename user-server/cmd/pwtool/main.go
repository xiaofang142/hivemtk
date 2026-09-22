// pwtool 生成 system_users.password 用的 bcrypt 哈希。
//
// 无参时仍打印开源仓库公开演示口令（cmd/seed 的 seedPasswordDefault）的哈希，
// 保持既有文档口径；要为新自定口令生成哈希：pwtool '<口令>' 或 SEED_PASSWORD=<口令> pwtool。
package main

import (
	"fmt"
	"os"
	"strings"

	"hivemtk-user/internal/pkg/utils/bcrypt"
)

// 与 cmd/seed/seed_users.go 的 seedPasswordDefault 同值；两处一改动步到位，
// 本工具无参运行只是给公开默认口令出文档哈希，不参与鉴权链路。
const publicDemoPassword = "Seed@123456"

func resolvePassword(argv []string, getenv func(string) string) string {
	if len(argv) > 0 {
		return argv[0]
	}
	// 环境变量分支按 TrimSpace 处理，与 cmd/seed 的 resolveSeedPassword 同口径，
	// 否则同一个 SEED_PASSWORD 在两边算出的哈希不一致（首尾空白差异）。
	for _, key := range []string{"SEED_PASSWORD", "ADMIN_PASSWORD"} {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
	}
	return publicDemoPassword
}

func main() {
	pw := resolvePassword(os.Args[1:], os.Getenv)
	if pw == publicDemoPassword {
		fmt.Fprintln(os.Stderr, "pwtool: 使用仓库公开的演示口令生成哈希；要覆盖请传口令参数或设置 SEED_PASSWORD")
	}
	h, err := bcrypt.HashPassword(pw)
	if err != nil {
		panic(err)
	}
	fmt.Println(h)
}
