# R39 任务清单（Step3 论证吸收 + Step4 头脑风暴二次论证）

> 采纳标准（用户钦定）: **站在用户角度, 功能是否需要, 是否有必要 — 其他不考虑**
> 依据: FEATURE_INVENTORY_R39.md(Step1) + 3代理网络调研(findings.md Step2)

## Step3 论证表（缺口→裁决）

| # | 任务 | 用户场景 | 竞品佐证 | 裁决 | 明细 |
|---|------|---------|---------|------|------|
| K1 | 话术版本+AB曝光 | 运营者想知道"哪版话术转化高, 旧版本停用" | Langfuse整数版本+label; Gong曝光→成交归因 | **吸收** | ScriptLibrary加version/status/expires_at; script_exposure_log表; FNV分桶; 48h归因窗可配; 转化率API |
| K2 | feature-flags | 管理员按环境灰度开功能 | Unleash/GrowthBook全有 | **吸收** | feature_flags表+CRUD+enable/disable+rollout evaluate+audit+eval_log+stale |
| K3 | security面板 | 管理员看"谁在攻击我" | authentik事件审计/Vaultwarden限速 | **吸收** | overview聚合+abnormal-logins(login_events已有)+password-weak(熵检测)+scan+report+unauthorized-access(401日志聚合) |
| K4 | backup管理补齐 | 管理员备份恢复 | 管理必备 | **吸收** | stats/strategy/preview/restore/create补齐(已有/backups) |
| K5 | AB高级统计 | 运营者判断实验可信 | GrowthBook标配 | **吸收** | bayesian胜率+SRM diagnostics+CUPED+sequential+results-with-reach |
| K6 | bots管理 | 管理员管理渠道机器人 | 渠道管理标配 | **吸收** | bots CRUD+register+mount-agent+sync-crm+webhook(映射channel_accounts+ai_agents) |
| K7 | kb-templates | 新用户一键建KB | Dify模板 | **吸收** | kb_templates表已有(A3债)+CRUD/apply/rate/subscribe |
| K8 | dashboard-templates | 大屏复用 | 报表标配 | **吸收** | dashboard_templates表已有+CRUD/clone/publish/rate |
| K9 | rag/eval面板 | 知识运营者回归检索质量 | RAGAS/Langfuse标配 | **吸收** | golden set表+CSV上传+run异步+runs列表+latest+diff |
| K10 | links接口 | 短链页调/api/links | 前端已开发 | **吸收** | 别名适配到现有shortlink服务 |
| K11 | quick-reply | 客服快捷回复分组 | 客服标配 | **吸收** | quick_reply_folders表+CRUD+reorder |
| K12 | analytics cohort/path | 留存/路径分析 | 分析标配 | **吸收** | cohort(注册周留存矩阵)+path(会话跳转桑基) |
| K13 | customer-service面板 | 客服主管看状态板 | 坐席标配 | **吸收** | agent-status-board(agent_statuses聚合)+ai-suggestions(sales_ai_suggestion) |
| K14 | email送达分析 | 邮件运营者看送达率 | Listmonk/BillionMail | **吸收** | deliverability聚合+bounces分桶+domain-reputation(SPF/DKIM检查)+test-send |
| K15 | whatsapp bulk-send | WA批量触达 | 渠道标配 | **吸收** | 复用reach_pipeline+进度job表 |
| K16 | 零散18接口 | 各页面点按钮报错 | — | **吸收** | 逐个最小实现(见K16明细) |

### K16 明细（18个零散）
1. mentions/:x/read POST — 通知已读
2. mentions/mine GET — 已存在(401), 跳过
3. cards/cross-publish — 名片跨平台复制发布(已下线: 未接任何平台发布 API，实际只建本地卡片+短链，名不副实；建卡能力保留在各平台卡片管理页)
4. customer-events/batch POST — 批量事件写入
5. marketing-flows/:x/sync-ab-results — 流程AB结果回写
6. admin/tuning/escalation-config GET/PUT — 转人工配置
7. customer-sessions/:x/csat GET + csat/trigger POST
8. customer-sessions/:x/edit-lock POST — 会话编辑锁
9. customer-sessions/:x/internal-notes GET/POST — 内部备注
10. customer-sessions/:x/apply-tag-rule POST — 标签规则应用
11. clues/:x/merge POST — 线索合并
12. clues/import/apply-suggestions POST — 导入建议应用
13. message-hub/dlq/:x GET + retry POST + batch-retry POST — 死信队列
14. knowledge/document-types GET — 文档类型枚举
15. knowledge/playground/presets GET — 检索预设
16. system/create-default-admin POST — 初始化管理员(init-admin已有, 加别名)
17. whatsapp/jobs/:x/progress GET — 批量进度
18. email/sent/:x GET — 发送详情

## Step4 头脑风暴二次论证（红队）

| 风险质疑 | 裁决 |
|---------|------|
| K1 semver还是整数版本? | 调研推翻semver: 用递增整数+status(active/archived/expired)。T-6原"三字段已入库"不实→按模型实况加字段 |
| K1 曝光日志会不会拖慢发送? | fire-and-forget异步缓冲(先例InitComplianceAuditLogger), 不阻断 |
| K2 rollout哈希分桶和T-7重复? | 共用同一FNV-1a工具函数, 两处消费 |
| K3 HIBP外网依赖? | 弱密码检测用本地熵+常见密码Top100列表, 不引外部API(部署环境可能离线) |
| K5 CUPED需要实验前数据? | 用客户注册前7天事件计数作协变量, 无数据退化为普通t检验 |
| K6 bots与channel_accounts重复? | bots=渠道账号视图+挂载agent, 组合查询不建新表 |
| K9 golden set与现有eval包重复? | eval包是指标计算器, 本任务补持久化+面板API, 复用RAGASEvaluator |
| K12 cohort数据量大? | 单租户小微规模, SQL实时聚合+30天窗口, 不物化 |
| K14 SPF/DKIM检查? | DNS TXT查询(net包), 无外网时返回unknown不报错 |
| 架构铁律 | 全部五层: router→controller→service→repository→model; 禁止内联handler; DI在router装配(现状事实) |
| 迁移纪律 | 新表注册migrate.go AutoMigrate(先例充分), 不改router/*.go之外的映射模式 |

## 实施批次（Step5 输入）

- B1: K1 话术版本+AB曝光（含迁移+API+前端对接检查）
- B2: K2 feature-flags 全套
- B3: K3 security + K4 backup
- B4: K5 AB高级统计 + K8 dashboard-templates
- B5: K6 bots + K7 kb-templates + K11 quick-reply
- B6: K9 rag/eval + K14 knowledge工作台补齐
- B7: K10 links + K12 analytics + K13 customer-service
- B8: K14 email + K15 whatsapp
- B9: K16 零散18接口
- B10: Step6 全量测试(api_verify+UI遍历+数据日志核对+修复)
