package main

import (
	"context"
	"fmt"
	"os"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service/trace_learning"
)

func main() {
	fmt.Println("[eval-trace] Starting...")
	db.InitDB()
	appCfg := config.GetAppConfig()
	dispatcher := llm.NewDispatcherFromConfig(appCfg)
	llm.InitGlobalDispatcherWithDB(dispatcher, db.GetDB())
	if err := llm.GetGlobalDispatcher().LoadProvidersFromDB(); err != nil {
		fmt.Printf("[eval-trace] WARN: 从数据库加载 provider 失败：%v\n", err)
	}
	if err := llm.GetGlobalDispatcher().LoadRoutesFromDB(); err != nil {
		fmt.Printf("[eval-trace] WARN: 从数据库加载场景路由规则失败：%v\n", err)
	}

	cfg := trace_learning.DefaultConfig()
	cfg.Industry = "general"
	svc := trace_learning.New(db.GetDB(), llm.GetGlobalDispatcher(), cfg)

	fmt.Println("[eval-trace] RunBatch(since=72h, batch=20)...")
	result, err := svc.RunBatch(context.Background(), 72, 20, false)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("processed=%d previews=%d\n", result.Processed, len(result.Previews))
	for i, p := range result.Previews {
		if i >= 5 {
			break
		}
		fmt.Printf("  trace=%s score=%d bad=%v reason=%s\n", p.TraceID, p.Score, p.Bad, p.Reason[:min(60, len(p.Reason))])
	}
	var count int
	db.GetDB().Raw("SELECT count(*) FROM learning_insights").Scan(&count)
	fmt.Printf("learning_insights now: %d\n", count)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
