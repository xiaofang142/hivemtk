// Package main 提供 syncscripts 命令行工具
//
// 职责:
//   - 读取管理端话术模板库（script_templates）
//   - 幂等 upsert 到运行时引擎话术库（script_library），按 (category, title) 去重
//
// 多租户模式下按租户逐个同步：一条 (category, title) 属于一个租户，
// 用无租户绑定的 ctx 跑等于把各租户的同名话术合并进同一行、
// 并把新建行的归属落到列默认值 1（默认租户）。
//
// 使用:
//
//	cd hivemtk/user-server
//	go run ./cmd/syncscripts
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"hivemtk-user/internal/content/model"
	contentrepo "hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

func main() {
	flag.Parse()

	db.InitDB()
	gormDB := db.GetDB()

	// AV-04c：ScriptTemplateSyncDeps 直构（原 service.NewScriptTemplateSyncDeps
	// 工厂已收口到装配处）。
	svc := service.NewScriptTemplateSyncService(service.ScriptTemplateSyncDeps{
		TemplateRepo: contentrepo.NewScriptTemplateRepository(gormDB),
	})
	ctxs, err := syncContexts(gormDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] 读取话术归属租户失败: %v\n", err)
		os.Exit(1)
	}

	total := service.SyncToLibraryStats{}
	for _, ctx := range ctxs {
		stats, err := svc.SyncToLibrary(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] 同步失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s: scanned=%d created=%d updated=%d skipped=%d\n",
			tenantLabel(ctx), stats.Scanned, stats.Created, stats.Updated, stats.Skipped)
		total.Scanned += stats.Scanned
		total.Created += stats.Created
		total.Updated += stats.Updated
		total.Skipped += stats.Skipped
	}
	fmt.Printf("✅ template → library 同步完成: scanned=%d created=%d updated=%d skipped=%d\n",
		total.Scanned, total.Created, total.Updated, total.Skipped)
}

// syncContexts 给出本次要跑的租户 ctx 列表。
//
// 离线模式保持单趟（与改前逐字节等价）：那时隔离 callback 不注入谓词、
// 也不填 tenant_id，按租户拆趟只会把同一批行重复同步 N 次。
func syncContexts(gormDB *gorm.DB) ([]context.Context, error) {
	if !tenant.IsMultiTenant() {
		return []context.Context{context.Background()}, nil
	}
	var ids []uint
	err := gormDB.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&model.ScriptTemplate{}).Distinct().Pluck("tenant_id", &ids).Error
	if err != nil {
		return nil, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ctxs := make([]context.Context, 0, len(ids))
	for _, id := range ids {
		ctxs = append(ctxs, tenant.OnlyTenant(context.Background(), id))
	}
	return ctxs, nil
}

func tenantLabel(ctx context.Context) string {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return "全量（离线模式，单趟）"
	}
	return fmt.Sprintf("租户 %d", id)
}
