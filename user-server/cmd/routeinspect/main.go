package main

import (
	"encoding/json"
	"fmt"
	"os"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/router"

	"github.com/gin-gonic/gin"
)

var _ = config.GetAppConfig
var _ = platform.InitSync

func main() {
	_ = config.GetAppConfig

	db.InitDB()

	middleware.InitInstallStatus()

	gin.SetMode(gin.DebugMode)
	r := gin.New()
	router.Setup(r, db.GetDB())

	routes := r.Routes()
	type rp struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	out := []rp{}
	for _, route := range routes {
		if len(route.Path) >= 4 && route.Path[:4] == "/api" {
			out = append(out, rp{Method: route.Method, Path: route.Path})
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	fmt.Fprintf(os.Stderr, "API 路由总数: %d\n", len(out))
}
