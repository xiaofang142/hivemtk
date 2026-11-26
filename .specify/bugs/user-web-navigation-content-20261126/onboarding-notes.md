# 导航引导接线说明

本轮子任务只修改 useOnboarding、pageTours、navigationGuide 双语资源及必要测试。保留主助手已改的 BrandConfig `.preview-notice`，不使用仅成功时存在的 `.code-block`。

## Layout 接线

- 顶栏 `el-menu-item` 添加 `:data-module="menu.key"`；Element Plus 当前项的 `.is-active` 用于确认侧栏与内容区属于当前模块。
- `useOnboarding()` 解构 `startModuleTour`、`getAvailableModules`。用户菜单增加 `command="module-tour"`，显示 `t('navigationGuide.moduleTour')`；仅当 `getAvailableModules().includes(activeTopMenu.value)` 时显示/启用。
- 手动处理分支调用 `startModuleTour(activeTopMenu.value, true)`。返回 boolean，失败可提示 `t('navigationGuide.moduleUnavailable')`。
- 三步为当前 tab → `.app-shell .app-aside` → `.app-shell .app-main`。只接受后端已加载且实际呈现的八组；未登录、菜单失败/未加载、未知 key、隐藏/未呈现/非当前 tab、缺任一目标均拒绝。管理员同样检查实际下发菜单。
- 不增加模块自动弹出入口。`startModuleTour(key)` 默认作为手动调用，忽略看过标记，因此现有 Layout 单参数调用也能重看；若未来接入自动调用，须显式传 `false`，按模块抑制重复启动。`restart('module', key)` 也可重看。切页时可沿用现有销毁入口，避免旧模块引导留在新页面。

## catalog 接线

在 `src/i18n/catalog.js` 静态导入 `navigationGuide.zh.json`、`navigationGuide.en.json`，分别挂到 `messages.zh.navigationGuide` 和 `messages.en.navigationGuide`。资源顶层没有重复的 namespace 包装。其余七语言沿用正式 `fallbackLocale: 'zh'`。

本轮回归时已观察到主助手完成 catalog 挂载及 Layout 的 `data-module`/手动菜单接线，实际 catalog 的双语解析与九语言回落测试通过。Layout 单参数调用可正常手动重看。

新增页面导览使用静态 `navigationGuide.pages.*`，原 `setup.onboarding.page.*` 调用保持不变。现有九语言完整性断言仍逐语言验证全部旧 key；新增 key 单独检查实际 catalog 的双语与九语言运行时 fallback，不绕开资源接线。CRM/线索只锚定原有 header/form/table，不依赖受权限控制的 WorkflowNavigation 容器。

## 主助手同步旧资源文案

以下保持旧静态 key，请主助手同时修改全局 zh/en 的对应值（本子任务不编辑全局资源）：

回归时已观察到主助手更新下列旧中文条目；表格作为语义核对参考，最终以主助手核验后的实际双语内容为准。

| key（均在 setup.onboarding.page 下） | 中文建议 | English suggestion |
|---|---|---|
| geoCrawlerStats.summary | 这里展示系统携带 AI User-Agent 发起的主动探测记录，用于检查站点响应，不代表外部 AI 爬虫真实访问。 | These records show active probes made by this system with AI User-Agent headers to check site responses; they do not prove external AI crawler visits. |
| geoCrawlerStats.step2.description | 核对探测目标、UA、时间、响应状态和来源；仅在有独立外部访问证据时判断真实抓取。 | Check the target, UA, time, response status and source. Assess real crawler visits only with independent external access evidence. |
| geoConfigOptimizer.summary | 维护品牌名称、官网域名、品牌描述与核心优势等基础资料，供 GEO 内容和站点工作使用。 | Maintain foundational brand and website data, including the brand name, domain, description and strengths, for GEO content and site workflows. |
| geoConfigOptimizer.step2.description | 检查品牌、站点、竞品与关键词资料后保存配置；此页不提供模型开关。 | Review brand, site, competitor and keyword details before saving. This page does not configure model switches. |
| geoDecisionReport.summary | 查看信源缺口补位任务与待处理、已完成概况，明确需要补充的内容或信源。 | Review source gap tasks and pending/completed totals to identify missing content or sources. |
| geoDecisionReport.step2.description | 按关键词、意图、缺口类型与详情逐项处理，核对补位结果后将任务标记完成。 | Work through each keyword, intent, gap type and detail, then mark the task complete after checking the result. |
| geoBrandConfig.step2.description | 预览会显示加载、失败或成功状态；仅成功生成后可核对并复制内容，失败时重试，预览不等于已部署。 | The preview reports loading, failure or success. Check and copy content only after successful generation; retry failures. A preview does not mean deployment is complete. |
| emailSmtp.summary | 配置实际发送邮件的发件账号、SMTP 连接与授权信息。 | Configure the sender accounts, SMTP connections and authorization used to send email. |
| emailSmtp.step2.description | 核对发件身份与连接参数，完成测试后再用于邮件任务；服务商参数可查阅邮箱服务商参考页。 | Verify the sender identity and connection settings, and test them before using the account in email tasks. Consult the email provider reference for provider parameters. |
| emailInfo.summary | 查阅邮箱服务商的连接端口与认证参考，辅助配置发件账号。 | Consult provider connection ports and authentication guidance when configuring sender accounts. |
| emailInfo.step2.description | 按服务商核对端口、安全连接与授权要求；实际账号凭据在发件账号页维护。 | Check ports, secure connection and authorization requirements for your provider. Maintain actual credentials on the sender accounts page. |

## 验证

- 先红：`npx vitest run tests/unit/onboarding_moduleTour.test.js`，初始 13 项失败，确认空实现与返回 route 的问题；补充切换模块记录回归时再次复现 1 项失败，再修复。
- 最终：`npx vitest run tests/unit/onboarding_moduleTour.test.js tests/unit/onboarding_pageTours.test.js tests/unit/onboarding_driverHooks.test.js tests/unit/workflow_navigation.test.js tests/unit/brandConfig_preview_state.test.js tests/unit/menu_navigation_layout.test.js`：6 files / 42 tests passed。
- `npx eslint tests/unit/onboarding_moduleTour.test.js tests/unit/onboarding_pageTours.test.js --max-warnings 0`：通过，无输出。
- `npx eslint src --max-warnings 80`：通过，0 errors / 12 warnings；警告均为主助手 Layout 接线处的模板缩进，请主助手在自己文件面修正。当前 ESLint 配置未覆盖独立 `.ts` 文件，Vitest 已实际加载两个引导 TS 模块。
- 对限定改动执行 `git diff --check`：通过。
- 未执行真实浏览器页面点验，不宣称 UI 已测。全量测试、构建、graphify 重建和发布由主助手统一完成；本子任务未 commit/push，未编辑 Layout/catalog/全局 zh/en。
