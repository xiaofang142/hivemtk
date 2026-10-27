// genjwt 一次性开发工具：本地签 admin JWT（PoC 验收用；不进生产，M2 完成后删除）
package main

import (
	"fmt"
	"os"

	"hivemtk-user/internal/pkg/utils"
)

func main() {
	u := utils.NewJWTUtils(utils.DefaultJWTConfig)
	tok, err := u.GenerateToken(1, "admin", "admin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(tok)
}
