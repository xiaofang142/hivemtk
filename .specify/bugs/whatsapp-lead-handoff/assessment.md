# Bug Assessment: WhatsApp 线索选择无法交接群发

- Slug: whatsapp-lead-handoff
- Created: 2026-11-20
- Source: 唯一未完成清单 NAV-04
- Verdict: valid
- Severity: medium

## Report

线索选择页写入 `localStorage.selectedLeads` 后跳到群发页，但群发页没有消费者。关闭判据要求真实预选、收件人预览、可取消以及无旧选择残留；允许可复现本地数据，不代表第三方送达。

## Reproduction / Root Cause

`LeadGroupSelection.vue` 的 proceedToMessaging 只写全局持久缓存；`BulkMessaging.vue` 初始化空 leadIds，不读缓存。另查明当前 Go 列表 DTO 返回小写 `id`，两个页面却仅取 `ID`，会丢失实际线索身份。置信度高：仓库搜索没有 selectedLeads 缓存消费者，且 DTO 定义可直接核验。

## Proposed Remediation

首选同一 SPA 内存中的一次性交接，以随机路由标识关联已选 ID，绑定当前用户与租户，五分钟失效。接收方立即消费，从当前租户接口重新解析联系人；缺失 ID 或接口失败时不部分选中。只传 ID，不持久化个人资料，不读取旧 selectedLeads。刷新和直接打开保持空选择。Layout 以 fullPath 为组件 key，因此消费后保留无个人资料的失效标识，避免清理 query 导致立即重挂载丢失选择；标识不可重放。

群发页显示选中姓名和号码；点击发送先重读联系人、展示明确确认框，取消不调用发送接口。确认使用冻结副本并校验当前表单/身份，重置和离开取消交接。保留已有沙箱 HTML 模板预览。

确认按钮再次读取名单，若姓名或号码变化则更新确认框并要求再次确认，缺失或接口失败不发送。浏览器夹具使用原始 SFC 与 fullPath key，接口全部本地模拟并记录外发计数。

未选方案：继续 localStorage 会保留跨会话残留；仅路由明文 ID 缺少一次性与用户作用域；持久 Pinia/新增后端草稿会扩大变更范围。

## Files likely to change

- `user-web/src/views/whatsappBot/LeadGroupSelection.vue`
- `user-web/src/views/whatsappBot/BulkMessaging.vue`
- `user-web/src/utils/bulkLeadSelection.js`（一次性交接与列表解析）
- `user-web/tests/unit/bulk_lead_handoff.test.js`
- `user-web/tests/unit/bulk_lead_selection.test.js`
- `user-web/tests/fixtures/bulk-lead-handoff/{index.html,main.js,api.js,server.mjs}`
- `user-web/src/i18n/locales/*.json`（新确认流程文案覆盖全部现有语言；构建缺键门要求）
- `user-web/src/config/onboarding/pageTours.ts`（仅必要时调整现有导览目标）

## Validation

先复现发送方真实小写 ID 选择未进入接收页；测试一次性消费、过期/错用户/错租户、号码更新、缺失记录、加载失败、超过首屏分页、确认前零 POST、取消和重置、直接重访无残留。保留 bulk_preview_isolation 回归，执行全量 SPA Vitest、eslint、构建；ego-browser 逐元素核验本地合成数据，并保存快照及零外发证据；按链 D 发布并核对线上资产摘要。

## Risks / Boundaries

不保证外部第三方送达；不在生产发送测试消息。删除的线索必须整批拒绝。浏览器内作用域只隔离 UI 交接，服务端权限仍由现有接口负责。分页读取仅为当前选择解析，不新增接口；若数据变化，确认前须重新审阅。扩展和后端无改动。
