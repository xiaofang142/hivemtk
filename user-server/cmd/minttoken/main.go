package main

import (
	"fmt"
	"hivemtk-user/internal/pkg/utils"
)

func main() {
	j := utils.NewJWTUtils(utils.DefaultJWTConfig)
	tok, err := j.GenerateToken(1, "admin", "admin")
	if err != nil {
		panic(err)
	}
	fmt.Println(tok)
}
