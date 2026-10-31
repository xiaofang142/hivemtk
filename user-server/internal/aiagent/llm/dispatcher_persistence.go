package llm

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/secrets"

	"gorm.io/gorm"
)

func providerDB() *gorm.DB {
	if g := getAuditDB(); g != nil {
		return g
	}
	return db.GetDB()
}

// systemScopeCtx 返回平台级操作的 ctx。
//
// 启动期/后台加载 provider 注册表与路由表没有 HTTP 请求上下文可绑，
// TENANT_MODE=on 时隔离 callback 会 fail-closed 拒绝（"需要租户上下文但 ctx 未绑定"）。
// 这类「装配层加载」与 migration/备份/健康检查同类，走 tenant.WithSystemScope
// 这条唯一豁免通道；不放宽任何带请求 ctx 的 HTTP 路径的隔离。
func systemScopeCtx() context.Context {
	return tenant.WithSystemScope(context.Background())
}

func (d *Dispatcher) LoadProvidersFromDB() error {
	g := providerDB()
	if g == nil {
		logger.Warnf("[LLM] LoadProvidersFromDB: db 未就绪，跳过从库加载 provider")
		return nil
	}
	// 启动期读路径：无租户 ctx，system scope 豁免（详见 systemScopeCtx 注释）
	sysCtx := systemScopeCtx()
	var rows []model.LLMProvider
	// 只装默认租户（tenant_id = 1）：dispatcher 全局单键 map（AddProvider/路由按 Name 索引），
	// 多租户需按租户分 map，本过滤是防跨租户 Key 泄漏的下限；
	// TENANT_MODE=off 时数据本就全部归属默认租户，行为不变。
	if err := g.WithContext(sysCtx).Where("tenant_id = ?", tenant.DefaultTenantID).
		Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return err
	}

	migrated := 0
	if secrets.Ready() {
		for i := range rows {
			if rows[i].APIKey != "" && !secrets.IsCiphertextFormat(rows[i].APIKey) {
				rows[i].APIKey = encryptAPIKeyForStorage(rows[i].APIKey)
				// 存量加密迁移同样是启动期后台写（不来自 HTTP 请求），同走 system scope
				if err := g.WithContext(sysCtx).Model(&model.LLMProvider{}).Where("id = ?", rows[i].ID).
					Update("api_key", rows[i].APIKey).Error; err == nil {
					migrated++
				} else {
					logger.Warnf("[LLM] api key 迁移加密失败 provider=%s: %v", rows[i].Name, err)
				}
			}
		}
		if migrated > 0 {
			logger.Infof("[LLM] api key 存量迁移：%d 个 provider 的明文密钥已加密", migrated)
		}
	}
	invalidKeys := 0
	for i := range rows {
		cfg := rowToProviderConfig(&rows[i])
		plain, invalid := decryptAPIKeyForUseNamed(cfg.Name, cfg.APIKey)
		cfg.APIKey = plain
		cfg.KeyInvalid = invalid
		if invalid {
			invalidKeys++
		}
		d.AddProvider(cfg)
	}
	logger.Infof("[LLM] LoadProvidersFromDB: 从 llm_providers 加载 %d 个 provider", len(rows))
	if invalidKeys > 0 {
		logger.Errorf("[LLM] 有 %d 个 provider 的 API Key 无法解密（MASTER_KEY 轮换或实例间不一致），需在「系统设置 → 模型提供商」重新保存密钥后恢复", invalidKeys)
	}
	return nil
}

// UpsertProviderToDB 写入/更新单个 provider 到 llm_providers（Name 为唯一键）。
// 直接镜像内存态 pc（含 APIKey）：与 dispatcher 内存保持一致。
// 清空密钥由调用方通过 APIKeyClearSentinel 解析后传 "" 实现。
//
// 写库必须走 system scope + 显式 tenant_id（两个都缺一不可）：
//  1. **system scope**：llm_providers 有 tenant_id 列，TENANT_MODE=on 时隔离 callback
//     对「ctx 未绑租户」的写是 fail-closed 的。本函数是平台级装配写库（provider 注册表
//     是全局单键 map，按 Name 索引，本就不分租户），与读路径同一口径；
//     隔离本身不放宽，带请求 ctx 的 HTTP 路径仍然 fail-closed。
//  2. **显式 tenant_id**：Save 一个 TenantID 零值的结构体时 GORM 仍会写 tenant_id 列，
//     值 0 会把行移出本租户；显式写 DefaultTenantID 才既不串租户、也不依赖列默认值。
func (d *Dispatcher) UpsertProviderToDB(pc ProviderConfig) error {
	g := providerDB()
	if g == nil {
		return nil
	}
	g = g.WithContext(systemScopeCtx())
	row := providerConfigToRow(pc)
	row.TenantID = tenant.DefaultTenantID
	row.UpdatedAt = time.Now()

	var exist model.LLMProvider
	err := g.Where("name = ?", pc.Name).First(&exist).Error
	if err == gorm.ErrRecordNotFound {
		if err := g.Create(&row).Error; err != nil {
			logger.Errorf("[LLM] UpsertProviderToDB 新建 %s 失败: %v", pc.Name, err)
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	row.ID = exist.ID
	row.CreatedAt = exist.CreatedAt
	if err := g.Save(&row).Error; err != nil {
		logger.Errorf("[LLM] UpsertProviderToDB 更新 %s 失败: %v", pc.Name, err)
		return err
	}
	// 重新保存即视为密钥已修复：清掉启动期打的「解不开」标记，
	// 否则页面会一直挂着"密钥失效"，直到下次重启才消失。
	clearKeyInvalid(pc.Name)
	return nil
}

// LoadRoutesFromDB 启动时从 llm_routing_rules 表加载路由，覆盖内存种子。
//
// 与 LoadProvidersFromDB 同理：DB 中的路由定义作为「源真相」覆盖代码 seed，
// 保证运营在后台配置的路由(主 provider / 兜底 / 灰度 / canary)重启不丢、多实例一致。
// 若表为空(全新部署)，内存保持代码种子，行为不变。
func (d *Dispatcher) LoadRoutesFromDB() error {
	g := providerDB()
	if g == nil {
		logger.Warnf("[LLM] LoadRoutesFromDB: db 未就绪，跳过路由加载")
		return nil
	}
	var rows []model.LLMRoutingRule
	// 启动期读路径：与 LoadProvidersFromDB 同理，system scope 豁免。
	// 同样只装默认租户：dispatcher.routes 是全局单键 map（按 scenario 索引），
	// 仅装默认租户；多租户需按租户分 map，本过滤是防跨租户路由/Key 泄漏的下限。
	if err := g.WithContext(systemScopeCtx()).Where("tenant_id = ?", tenant.DefaultTenantID).
		Find(&rows).Error; err != nil {
		logger.Errorf("[LLM] LoadRoutesFromDB 查询失败: %v", err)
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range rows {
		var r ScenarioRoute
		if err := json.Unmarshal([]byte(rows[i].RouteJSON), &r); err != nil {
			logger.Errorf("[LLM] LoadRoutesFromDB: 解析 scenario=%s 失败: %v", rows[i].Scenario, err)
			continue
		}
		d.routes[r.Scenario] = &r
	}
	logger.Infof("[LLM] LoadRoutesFromDB: 从 llm_routing_rules 加载 %d 条路由覆盖种子", len(rows))
	return nil
}

// UpsertRouteToDB 将路由规则 upsert 到 llm_routing_rules 表。
// db 未就绪时静默跳过(内存已生效)，仅记录日志，不阻断主流程。
//
// 写库走 system scope + 显式 tenant_id：llm_routing_rules 是租户表，
// TENANT_MODE=on 时无 ctx 的写会被隔离 fail-closed 拒绝
// （"[tenant] 表 llm_routing_rules 需要租户上下文但 ctx 未绑定（query）"），
// 后果是**后台配的路由只进内存、容器一重启全丢**。
// 口径与 UpsertProviderToDB、LoadRoutesFromDB 同一套。
func (d *Dispatcher) UpsertRouteToDB(r ScenarioRoute) error {
	g := providerDB()
	if g == nil {
		return nil
	}
	g = g.WithContext(systemScopeCtx())
	buf, err := json.Marshal(r)
	if err != nil {
		logger.Errorf("[LLM] UpsertRouteToDB 序列化 scenario=%s 失败: %v", r.Scenario, err)
		return err
	}
	row := model.LLMRoutingRule{
		TenantID:  tenant.DefaultTenantID,
		Scenario:  string(r.Scenario),
		RouteJSON: string(buf),
		Version:   r.Version,
		UpdatedAt: time.Now(),
	}
	var exist model.LLMRoutingRule
	q := g.Where("scenario = ?", string(r.Scenario)).First(&exist)
	if q.Error == gorm.ErrRecordNotFound {
		row.CreatedAt = time.Now()
		if err := g.Create(&row).Error; err != nil {
			logger.Errorf("[LLM] UpsertRouteToDB 新建 scenario=%s 失败: %v", r.Scenario, err)
			return err
		}
		return nil
	}
	if q.Error != nil {
		logger.Errorf("[LLM] UpsertRouteToDB 查询 scenario=%s 失败: %v", r.Scenario, q.Error)
		return q.Error
	}
	row.ID = exist.ID
	row.CreatedAt = exist.CreatedAt
	if err := g.Save(&row).Error; err != nil {
		logger.Errorf("[LLM] UpsertRouteToDB 更新 scenario=%s 失败: %v", r.Scenario, err)
		return err
	}
	return nil
}

// DeleteRouteFromDB 从数据库删除某场景路由定义。
// 同 UpsertRouteToDB：走 system scope，否则删除被隔离 fail-closed 拒绝
// （后台「删除路由」会删不掉，且内存已删、库还在 ⇒ 重启后又回来了）。
func (d *Dispatcher) DeleteRouteFromDB(scenario DispatchScenario) error {
	g := providerDB()
	if g == nil {
		return nil
	}
	g = g.WithContext(systemScopeCtx())
	if err := g.Where("scenario = ?", string(scenario)).Delete(&model.LLMRoutingRule{}).Error; err != nil {
		logger.Errorf("[LLM] DeleteRouteFromDB scenario=%s 失败: %v", scenario, err)
		return err
	}
	return nil
}

// DeleteProviderFromDB 从 llm_providers 删除指定 provider。
// 同 UpsertProviderToDB：走 system scope，否则删除被隔离 fail-closed 拒绝。
func (d *Dispatcher) DeleteProviderFromDB(name string) error {
	g := providerDB()
	if g == nil {
		return nil
	}
	g = g.WithContext(systemScopeCtx())
	if err := g.Where("name = ?", name).Delete(&model.LLMProvider{}).Error; err != nil {
		logger.Errorf("[LLM] DeleteProviderFromDB %s 失败: %v", name, err)
		return err
	}
	return nil
}

func providerConfigToRow(pc ProviderConfig) model.LLMProvider {
	return model.LLMProvider{
		Name:            pc.Name,
		DisplayName:     pc.DisplayName,
		BaseURL:         pc.BaseURL,
		Model:           pc.Model,
		APIKey:          encryptAPIKeyForStorage(pc.APIKey),
		APIType:         pc.APIType,
		Enabled:         pc.Enabled,
		QualityScore:    pc.QualityScore,
		MaxRPM:          pc.MaxRPM,
		MaxTPM:          pc.MaxTPM,
		CostPer1k:       pc.CostPer1k,
		CostPer1kInput:  pc.CostPer1kInput,
		CostPer1kOutput: pc.CostPer1kOutput,
		Source:          pc.ProviderSource(),
		PlanCode:        pc.PlanCode,
		AvgLatencyMs:    pc.AvgLatencyMs,
		NoFC:            pc.NoFC,
		Vendor:          pc.Vendor,
		Tags:            tagsToText(pc.Tags),
	}
}

func rowToProviderConfig(row *model.LLMProvider) ProviderConfig {
	return ProviderConfig{
		Name:            row.Name,
		DisplayName:     row.DisplayName,
		APIKey:          row.APIKey,
		BaseURL:         row.BaseURL,
		Model:           row.Model,
		APIType:         row.APIType,
		Enabled:         row.Enabled,
		QualityScore:    row.QualityScore,
		MaxRPM:          row.MaxRPM,
		MaxTPM:          row.MaxTPM,
		CostPer1k:       row.CostPer1k,
		CostPer1kInput:  row.CostPer1kInput,
		CostPer1kOutput: row.CostPer1kOutput,
		Source:          row.Source,
		PlanCode:        row.PlanCode,
		AvgLatencyMs:    row.AvgLatencyMs,
		NoFC:            row.NoFC,
		Vendor:          row.Vendor,
		Tags:            tagsFromText(row.Tags),
	}
}

func tagsToText(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

func tagsFromText(s string) []string {
	if s == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err == nil {
		return tags
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
